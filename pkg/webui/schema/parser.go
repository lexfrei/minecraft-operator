package schema

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	typeString = "string"
	typeObject = "object"
	typeArray  = "array"
	fieldName  = "name"
	fieldNS    = "namespace"
)

// conditionalRules defines known cross-field visibility dependencies.
var conditionalRules = map[string]*Condition{
	"version": {
		DependsOn: "updateStrategy",
		Values:    []string{"pin", "build-pin"},
	},
	"build": {
		DependsOn: "updateStrategy",
		Values:    []string{"build-pin"},
	},
}

// Parser parses OpenAPI specifications into FormSchema structs.
type Parser struct {
	root    map[string]any
	schemas map[string]any
}

// NewParser creates a Parser from raw OpenAPI YAML bytes.
func NewParser(specData []byte) (*Parser, error) {
	var root map[string]any
	if err := yaml.Unmarshal(specData, &root); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAPI spec: %w", err)
	}

	schemas, err := navigateMap(root, "components", "schemas")
	if err != nil {
		return nil, fmt.Errorf("failed to find components.schemas: %w", err)
	}

	return &Parser{
		root:    root,
		schemas: schemas,
	}, nil
}

// ParseSchema parses a named schema from the OpenAPI spec into a FormSchema.
func (p *Parser) ParseSchema(schemaName string) (*FormSchema, error) {
	schemaRaw, ok := p.schemas[schemaName]
	if !ok {
		return nil, fmt.Errorf("schema %q not found", schemaName)
	}

	schemaMap, ok := schemaRaw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema %q is not an object", schemaName)
	}

	description, _ := schemaMap["description"].(string)
	requiredSet := buildRequiredSet(schemaMap)
	properties, _ := schemaMap["properties"].(map[string]any)

	fields := make([]FormField, 0, len(properties))
	for propName, propRaw := range properties {
		field := p.parseField(propName, propRaw, requiredSet[propName])
		fields = append(fields, field)
	}

	sortFields(fields)

	return &FormSchema{
		Title:       schemaName,
		Description: description,
		Fields:      fields,
	}, nil
}

// parseField converts a single OpenAPI property into a FormField.
func (p *Parser) parseField(name string, raw any, required bool) FormField {
	field := FormField{
		Name:     name,
		Required: required,
	}

	propMap := p.resolvePropMap(raw)
	if propMap == nil {
		field.Type = typeString
		return field
	}

	p.extractBasicProps(&field, propMap)
	p.extractConstraints(&field, propMap)
	p.extractComplexTypes(&field, propMap)
	applyFieldMeta(&field)

	return field
}

// resolvePropMap extracts the property map, resolving $ref if needed.
func (p *Parser) resolvePropMap(raw any) map[string]any {
	propMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}

	if ref, hasRef := propMap["$ref"].(string); hasRef {
		return p.resolveRef(ref)
	}

	return propMap
}

// extractBasicProps fills basic scalar fields from the property map.
func (p *Parser) extractBasicProps(field *FormField, propMap map[string]any) {
	field.Type, _ = propMap["type"].(string)
	field.Format, _ = propMap["format"].(string)
	field.Description, _ = propMap["description"].(string)
	field.Pattern, _ = propMap["pattern"].(string)
	field.Example = toString(propMap["example"])
	field.Default = toString(propMap["default"])

	if enumRaw, ok := propMap["enum"]; ok {
		field.Enum = toStringSlice(enumRaw)
	}
}

// extractConstraints fills min/max length and value constraints.
func (p *Parser) extractConstraints(field *FormField, propMap map[string]any) {
	if v, ok := propMap["minLength"]; ok {
		n := toInt(v)
		field.MinLength = &n
	}
	if v, ok := propMap["maxLength"]; ok {
		n := toInt(v)
		field.MaxLength = &n
	}
	if v, ok := propMap["minimum"]; ok {
		n := toInt(v)
		field.Minimum = &n
	}
	if v, ok := propMap["maximum"]; ok {
		n := toInt(v)
		field.Maximum = &n
	}
}

// extractComplexTypes handles nested objects, arrays, and additionalProperties.
func (p *Parser) extractComplexTypes(field *FormField, propMap map[string]any) {
	if field.Type == typeObject {
		p.extractObjectProps(field, propMap)
	}

	if field.Type == typeArray {
		if items, hasItems := propMap["items"]; hasItems {
			itemField := p.parseField("item", items, false)
			field.Items = &itemField
		}
	}
}

// extractObjectProps handles nested object properties and additionalProperties.
func (p *Parser) extractObjectProps(field *FormField, propMap map[string]any) {
	if ap, hasAP := propMap["additionalProperties"]; hasAP {
		if apMap, ok := ap.(map[string]any); ok {
			if apType, _ := apMap["type"].(string); apType == typeString {
				field.AdditionalProperties = true
			}
		}
	}

	props, hasProps := propMap["properties"].(map[string]any)
	if !hasProps {
		return
	}

	requiredSet := buildRequiredSet(propMap)
	field.Properties = make([]FormField, 0, len(props))
	for subName, subRaw := range props {
		subField := p.parseField(subName, subRaw, requiredSet[subName])
		field.Properties = append(field.Properties, subField)
	}
	sortFields(field.Properties)
}

// applyFieldMeta attaches conditional visibility and identity markers.
func applyFieldMeta(field *FormField) {
	if cond, hasCond := conditionalRules[field.Name]; hasCond {
		field.Condition = cond
	}
	if field.Name == fieldName || field.Name == fieldNS {
		field.ReadOnlyOnEdit = true
	}
}

// buildRequiredSet extracts the "required" array from a schema map into a set.
func buildRequiredSet(schemaMap map[string]any) map[string]bool {
	required := toStringSlice(schemaMap["required"])
	set := make(map[string]bool, len(required))
	for _, r := range required {
		set[r] = true
	}
	return set
}

// resolveRef resolves a $ref pointer like "#/components/schemas/UpdateStrategy".
func (p *Parser) resolveRef(ref string) map[string]any {
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")

	var current any = p.root
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[part]
	}

	result, _ := current.(map[string]any)
	return result
}

// navigateMap walks nested maps by keys.
func navigateMap(m map[string]any, keys ...string) (map[string]any, error) {
	current := m
	for _, key := range keys {
		next, ok := current[key]
		if !ok {
			return nil, fmt.Errorf("key %q not found", key)
		}
		current, ok = next.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("key %q is not a map", key)
		}
	}
	return current, nil
}

// toStringSlice converts an any (expected []any) to []string.
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// toInt converts an any to int.
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}

// toString converts an any to string representation.
func toString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case int:
		return strconv.Itoa(val)
	case float64:
		if val == float64(int(val)) {
			return strconv.Itoa(int(val))
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// sortFields sorts fields: required first, then alphabetically.
func sortFields(fields []FormField) {
	for i := 1; i < len(fields); i++ {
		for j := i; j > 0; j-- {
			if fieldLess(fields[j], fields[j-1]) {
				fields[j], fields[j-1] = fields[j-1], fields[j]
			}
		}
	}
}

func fieldLess(a, b FormField) bool {
	aID := a.Name == fieldName || a.Name == fieldNS
	bID := b.Name == fieldName || b.Name == fieldNS
	if aID != bID {
		return aID
	}
	if aID && bID {
		return a.Name == fieldName
	}
	if a.Required != b.Required {
		return a.Required
	}
	return a.Name < b.Name
}
