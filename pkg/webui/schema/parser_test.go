package schema

import (
	"testing"

	openapi "github.com/lexfrei/minecraft-operator/api/openapi"
)

func TestParseSchema_ServerCreateRequest(t *testing.T) {
	p, err := NewParser(openapi.Spec)
	if err != nil {
		t.Fatalf("NewParser failed: %v", err)
	}

	schema, err := p.ParseSchema("ServerCreateRequest")
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	if schema.Title != "ServerCreateRequest" {
		t.Errorf("expected title ServerCreateRequest, got %s", schema.Title)
	}

	// Must have fields
	if len(schema.Fields) == 0 {
		t.Fatal("expected fields, got none")
	}

	// Check required fields exist
	requiredNames := map[string]bool{"name": false, "namespace": false, fieldUpdateStrategy: false}
	for _, f := range schema.Fields {
		if _, ok := requiredNames[f.Name]; ok {
			requiredNames[f.Name] = true
			if !f.Required {
				t.Errorf("field %s should be required", f.Name)
			}
		}
	}
	for name, found := range requiredNames {
		if !found {
			t.Errorf("required field %s not found in schema", name)
		}
	}
}

func TestParseSchema_FieldTypes(t *testing.T) {
	p, err := NewParser(openapi.Spec)
	if err != nil {
		t.Fatalf("NewParser failed: %v", err)
	}

	schema, err := p.ParseSchema("ServerCreateRequest")
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	fieldByName := make(map[string]FormField)
	for _, f := range schema.Fields {
		fieldByName[f.Name] = f
	}

	// updateStrategy should be an enum
	if us, ok := fieldByName["updateStrategy"]; ok {
		if len(us.Enum) == 0 {
			t.Error("updateStrategy should have enum values")
		}
	} else {
		t.Error("updateStrategy field not found")
	}

	// build should be integer
	if b, ok := fieldByName["build"]; ok {
		if b.Type != typeInteger {
			t.Errorf("build should be integer, got %s", b.Type)
		}
		if b.Minimum == nil || *b.Minimum != 1 {
			t.Error("build should have minimum 1")
		}
	} else {
		t.Error("build field not found")
	}

	// name should have pattern
	if n, ok := fieldByName["name"]; ok {
		if n.Pattern == "" {
			t.Error("name should have a pattern")
		}
	}

	// labels should be additionalProperties map
	if l, ok := fieldByName["labels"]; ok {
		if !l.AdditionalProperties {
			t.Error("labels should have additionalProperties=true")
		}
	} else {
		t.Error("labels field not found")
	}
}

func TestParseSchema_NestedObjects(t *testing.T) {
	p, err := NewParser(openapi.Spec)
	if err != nil {
		t.Fatalf("NewParser failed: %v", err)
	}

	schema, err := p.ParseSchema("ServerCreateRequest")
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	fieldByName := make(map[string]FormField)
	for _, f := range schema.Fields {
		fieldByName[f.Name] = f
	}

	// rcon should be a nested object with properties
	if rcon, ok := fieldByName["rcon"]; ok {
		if rcon.Type != typeObject {
			t.Errorf("rcon should be object, got %s", rcon.Type)
		}
		if len(rcon.Properties) == 0 {
			t.Error("rcon should have nested properties")
		}
		// Check rcon has enabled field
		found := false
		for _, p := range rcon.Properties {
			if p.Name == fieldEnabled {
				found = true
				if p.Type != typeBoolean {
					t.Errorf("rcon.enabled should be boolean, got %s", p.Type)
				}
			}
		}
		if !found {
			t.Error("rcon should have 'enabled' property")
		}
	} else {
		t.Error("rcon field not found")
	}

	// maintenanceWindow should be a nested object
	if mw, ok := fieldByName["maintenanceWindow"]; ok {
		if mw.Type != typeObject {
			t.Errorf("maintenanceWindow should be object, got %s", mw.Type)
		}
		if len(mw.Properties) == 0 {
			t.Error("maintenanceWindow should have nested properties")
		}
	} else {
		t.Error("maintenanceWindow field not found")
	}
}

func TestParseSchema_PluginCreateRequest(t *testing.T) {
	p, err := NewParser(openapi.Spec)
	if err != nil {
		t.Fatalf("NewParser failed: %v", err)
	}

	schema, err := p.ParseSchema("PluginCreateRequest")
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	fieldByName := make(map[string]FormField)
	for _, f := range schema.Fields {
		fieldByName[f.Name] = f
	}

	// source should be a nested object with type, project, url, checksum
	if src, ok := fieldByName["source"]; ok {
		if src.Type != typeObject {
			t.Errorf("source should be object, got %s", src.Type)
		}
		propNames := make(map[string]bool)
		for _, p := range src.Properties {
			propNames[p.Name] = true
		}
		for _, expected := range []string{"type", "project", "url", "checksum"} {
			if !propNames[expected] {
				t.Errorf("source should have property %s", expected)
			}
		}
	} else {
		t.Error("source field not found")
	}

	// instanceSelector should exist
	if _, ok := fieldByName["instanceSelector"]; !ok {
		t.Error("instanceSelector field not found")
	}

	// endpoints should be an array
	if ep, ok := fieldByName["endpoints"]; ok {
		if ep.Type != "array" {
			t.Errorf("endpoints should be array, got %s", ep.Type)
		}
		if ep.Items == nil {
			t.Error("endpoints should have items schema")
		}
	} else {
		t.Error("endpoints field not found")
	}
}

func TestParseSchema_ConditionalVisibility(t *testing.T) {
	p, err := NewParser(openapi.Spec)
	if err != nil {
		t.Fatalf("NewParser failed: %v", err)
	}

	schema, err := p.ParseSchema("ServerCreateRequest")
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	fieldByName := make(map[string]FormField)
	for _, f := range schema.Fields {
		fieldByName[f.Name] = f
	}

	// version should be conditional on updateStrategy=pin,build-pin
	if v, ok := fieldByName[fieldVersion]; ok {
		if v.Condition == nil {
			t.Error("version should have a conditional visibility rule")
		} else {
			if v.Condition.DependsOn != "updateStrategy" {
				t.Errorf("version condition should depend on updateStrategy, got %s", v.Condition.DependsOn)
			}
		}
	}

	// build should be conditional on updateStrategy=build-pin
	if b, ok := fieldByName["build"]; ok {
		if b.Condition == nil {
			t.Error("build should have a conditional visibility rule")
		} else {
			if b.Condition.DependsOn != "updateStrategy" {
				t.Errorf("build condition should depend on updateStrategy, got %s", b.Condition.DependsOn)
			}
		}
	}
}

func TestParseSchema_UpdateRequests(t *testing.T) {
	p, err := NewParser(openapi.Spec)
	if err != nil {
		t.Fatalf("NewParser failed: %v", err)
	}

	// ServerUpdateRequest should have no required fields
	schema, err := p.ParseSchema("ServerUpdateRequest")
	if err != nil {
		t.Fatalf("ParseSchema(ServerUpdateRequest) failed: %v", err)
	}

	for _, f := range schema.Fields {
		if f.Required {
			t.Errorf("ServerUpdateRequest field %s should not be required", f.Name)
		}
	}

	// PluginUpdateRequest should parse
	schema, err = p.ParseSchema("PluginUpdateRequest")
	if err != nil {
		t.Fatalf("ParseSchema(PluginUpdateRequest) failed: %v", err)
	}

	if len(schema.Fields) == 0 {
		t.Error("PluginUpdateRequest should have fields")
	}
}

func TestParseSchema_NonExistent(t *testing.T) {
	p, err := NewParser(openapi.Spec)
	if err != nil {
		t.Fatalf("NewParser failed: %v", err)
	}

	_, err = p.ParseSchema("NonExistentSchema")
	if err == nil {
		t.Error("expected error for non-existent schema")
	}
}

func TestNewParser_InvalidYAML(t *testing.T) {
	_, err := NewParser([]byte("not: valid: yaml: ["))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}
