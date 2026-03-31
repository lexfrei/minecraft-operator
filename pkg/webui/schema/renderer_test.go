package schema

import (
	"strings"
	"testing"
)

func TestRenderForm_BasicFields(t *testing.T) {
	schema := &FormSchema{
		Title: "TestCreate",
		Fields: []FormField{
			{Name: fieldName, Type: typeString, Required: true, ReadOnlyOnEdit: true},
			{Name: "strategy", Type: typeString, Enum: []string{"latest", "pin"}, Required: true},
			{Name: "count", Type: "integer", Minimum: intPtr(1), Maximum: intPtr(100)},
			{Name: "enabled", Type: "boolean", Default: "true"},
		},
	}

	html := RenderForm(schema, nil, RenderOptions{Mode: ModeCreate, SubmitURL: "/api/v1/servers"})

	// Should contain form tag with hx-post
	if !strings.Contains(html, `hx-post="/api/v1/servers"`) {
		t.Error("expected hx-post attribute in form")
	}

	// Name field should be text input
	if !strings.Contains(html, `name="name"`) {
		t.Error("expected name field")
	}
	// Name should NOT be readonly in create mode
	if strings.Contains(html, `name="name"`) && strings.Contains(html, `readonly`) {
		// Check it's specifically the name field that's readonly
		idx := strings.Index(html, `name="name"`)
		surrounding := html[max(0, idx-200):min(len(html), idx+200)]
		if strings.Contains(surrounding, "readonly") {
			t.Error("name field should not be readonly in create mode")
		}
	}

	// Strategy should be a select
	if !strings.Contains(html, "<select") {
		t.Error("expected select element for enum field")
	}
	if !strings.Contains(html, `<option value="latest"`) {
		t.Error("expected 'latest' option")
	}

	// Count should be number input
	if !strings.Contains(html, `type="number"`) {
		t.Error("expected number input for integer field")
	}
	if !strings.Contains(html, `min="1"`) {
		t.Error("expected min attribute")
	}

	// Enabled should be checkbox
	if !strings.Contains(html, `type="checkbox"`) {
		t.Error("expected checkbox for boolean field")
	}
}

func TestRenderForm_EditMode(t *testing.T) {
	schema := &FormSchema{
		Title: "TestUpdate",
		Fields: []FormField{
			{Name: fieldName, Type: typeString, Required: true, ReadOnlyOnEdit: true},
			{Name: "version", Type: typeString},
		},
	}

	values := map[string]any{
		fieldName: "my-server",
		"version": "1.21.1",
	}

	html := RenderForm(schema, values, RenderOptions{
		Mode:      ModeEdit,
		SubmitURL: "/api/v1/servers/default/my-server",
	})

	// Should use hx-put for edit
	if !strings.Contains(html, `hx-put=`) {
		t.Error("expected hx-put for edit mode")
	}

	// Name should be readonly
	nameIdx := strings.Index(html, `name="name"`)
	if nameIdx == -1 {
		t.Fatal("name field not found")
	}
	surrounding := html[max(0, nameIdx-300):min(len(html), nameIdx+100)]
	if !strings.Contains(surrounding, "readonly") {
		t.Error("name should be readonly in edit mode")
	}

	// Version should have value prefilled
	if !strings.Contains(html, `value="1.21.1"`) {
		t.Error("expected version value to be prefilled")
	}
}

func TestRenderForm_NestedObject(t *testing.T) {
	schema := &FormSchema{
		Title: "TestNested",
		Fields: []FormField{
			{
				Name: "rcon",
				Type: typeObject,
				Properties: []FormField{
					{Name: "enabled", Type: "boolean"},
					{Name: "port", Type: "integer", Default: "25575"},
				},
			},
		},
	}

	html := RenderForm(schema, nil, RenderOptions{Mode: ModeCreate, SubmitURL: "/api/v1/servers"})

	// Should use dot-notation for nested fields
	if !strings.Contains(html, `name="rcon.enabled"`) {
		t.Error("expected dot-notation name for nested field")
	}
	if !strings.Contains(html, `name="rcon.port"`) {
		t.Error("expected rcon.port field")
	}
}

func TestRenderForm_ConditionalVisibility(t *testing.T) {
	schema := &FormSchema{
		Title: "TestConditional",
		Fields: []FormField{
			{Name: "strategy", Type: typeString, Enum: []string{"latest", "pin"}},
			{
				Name: "version",
				Type: typeString,
				Condition: &Condition{
					DependsOn: "strategy",
					Values:    []string{"pin"},
				},
			},
		},
	}

	html := RenderForm(schema, nil, RenderOptions{Mode: ModeCreate, SubmitURL: "/api/v1/servers"})

	if !strings.Contains(html, `data-depends-on="strategy"`) {
		t.Error("expected data-depends-on attribute")
	}
	if !strings.Contains(html, `data-show-when="pin"`) {
		t.Error("expected data-show-when attribute")
	}
}

func TestRenderForm_AdditionalProperties(t *testing.T) {
	schema := &FormSchema{
		Title: "TestMap",
		Fields: []FormField{
			{Name: "labels", Type: typeObject, AdditionalProperties: true},
		},
	}

	html := RenderForm(schema, nil, RenderOptions{Mode: ModeCreate, SubmitURL: "/api/v1/servers"})

	// Should have add button for key-value pairs
	if !strings.Contains(html, "data-map-field") {
		t.Error("expected data-map-field attribute for key-value editor")
	}
}

func TestRenderForm_ArrayField(t *testing.T) {
	schema := &FormSchema{
		Title: "TestArray",
		Fields: []FormField{
			{
				Name: "endpoints",
				Type: typeArray,
				Items: &FormField{
					Name: "item",
					Type: typeObject,
					Properties: []FormField{
						{Name: fieldName, Type: typeString, Required: true},
						{Name: "port", Type: "integer", Required: true},
					},
				},
			},
		},
	}

	html := RenderForm(schema, nil, RenderOptions{Mode: ModeCreate, SubmitURL: "/api/v1/servers"})

	if !strings.Contains(html, "data-array-field") {
		t.Error("expected data-array-field attribute")
	}
	// Should have a template for cloning
	if !strings.Contains(html, "<template") {
		t.Error("expected template element for array items")
	}
}

func TestRenderForm_RequiredMarker(t *testing.T) {
	schema := &FormSchema{
		Title: "TestRequired",
		Fields: []FormField{
			{Name: fieldName, Type: typeString, Required: true},
			{Name: "optional", Type: typeString},
		},
	}

	html := RenderForm(schema, nil, RenderOptions{Mode: ModeCreate, SubmitURL: "/api/v1/test"})

	// Required field label should have asterisk
	nameLabel := extractBetween(html, `<label for="name"`, "</label>")
	if !strings.Contains(nameLabel, "*") {
		t.Error("required field should have asterisk in label")
	}

	// Optional field should not
	optLabel := extractBetween(html, `<label for="optional"`, "</label>")
	if strings.Contains(optLabel, "*") {
		t.Error("optional field should not have asterisk in label")
	}
}

func TestRenderForm_SubmitButton(t *testing.T) {
	schema := &FormSchema{Title: "Test", Fields: []FormField{}}

	html := RenderForm(schema, nil, RenderOptions{Mode: ModeCreate, SubmitURL: "/api/v1/test"})

	if !strings.Contains(html, `type="submit"`) {
		t.Error("expected submit button")
	}
	if !strings.Contains(html, "Create") {
		t.Error("expected 'Create' button text for create mode")
	}

	html = RenderForm(schema, nil, RenderOptions{Mode: ModeEdit, SubmitURL: "/api/v1/test"})

	if !strings.Contains(html, "Update") {
		t.Error("expected 'Update' button text for edit mode")
	}
}

// extractBetween returns the substring between start and end markers.
func extractBetween(s, start, end string) string {
	si := strings.Index(s, start)
	if si == -1 {
		return ""
	}
	ei := strings.Index(s[si:], end)
	if ei == -1 {
		return ""
	}
	return s[si : si+ei+len(end)]
}

func TestRenderForm_ConfigMapRefField(t *testing.T) {
	s := &FormSchema{
		Title: "TestConfigMapRef",
		Fields: []FormField{
			{
				Name: "configMapRef",
				Type: typeObject,
				Properties: []FormField{
					{Name: fieldName, Type: typeString, Required: true},
					{Name: "key", Type: typeString, Required: true},
				},
			},
		},
	}

	h := RenderForm(s, nil, RenderOptions{Mode: ModeCreate, SubmitURL: "/api/v1/test"})

	if !strings.Contains(h, "data-configmap-ref") {
		t.Error("expected data-configmap-ref attribute for ConfigMap picker")
	}
	if !strings.Contains(h, "data-configmap-name") {
		t.Error("expected data-configmap-name select")
	}
	if !strings.Contains(h, "data-configmap-key") {
		t.Error("expected data-configmap-key select")
	}
	if !strings.Contains(h, "data-configmap-create") {
		t.Error("expected create button")
	}
	if strings.Contains(h, "<fieldset") {
		t.Error("configMapRef should use custom picker, not generic fieldset")
	}
}

func TestRenderForm_ConfigMapRefPreFill(t *testing.T) {
	s := &FormSchema{
		Title: "TestConfigMapRefEdit",
		Fields: []FormField{
			{
				Name: "configMapRef",
				Type: typeObject,
				Properties: []FormField{
					{Name: fieldName, Type: typeString},
					{Name: "key", Type: typeString},
				},
			},
		},
	}

	values := map[string]any{
		"configMapRef.name": "my-config",
		"configMapRef.key":  "core.conf",
	}

	h := RenderForm(s, values, RenderOptions{Mode: ModeEdit, SubmitURL: "/api/v1/test"})

	if !strings.Contains(h, `value="my-config"`) {
		t.Error("expected ConfigMap name to be pre-filled")
	}
	if !strings.Contains(h, `value="core.conf"`) {
		t.Error("expected ConfigMap key to be pre-filled")
	}
}

func TestRenderForm_DisabledSelectPreservesValue(t *testing.T) {
	schema := &FormSchema{
		Title: "TestDisabled",
		Fields: []FormField{
			{
				Name:           "strategy",
				Type:           typeString,
				Enum:           []string{"latest", "pin"},
				ReadOnlyOnEdit: true,
			},
		},
	}

	values := map[string]any{"strategy": "pin"}

	html := RenderForm(schema, values, RenderOptions{Mode: ModeEdit, SubmitURL: "/api/v1/test"})

	// Select should be disabled
	if !strings.Contains(html, "disabled") {
		t.Error("expected disabled attribute on readonly select in edit mode")
	}
	// Hidden input should preserve the value
	if !strings.Contains(html, `<input type="hidden" name="strategy" value="pin"/>`) {
		t.Error("expected hidden input to preserve disabled select value")
	}
}

func intPtr(v int) *int {
	return &v
}
