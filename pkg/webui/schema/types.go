// Package schema provides OpenAPI schema parsing and HTML form rendering.
package schema

// FormSchema represents a parsed form definition from an OpenAPI request schema.
type FormSchema struct {
	// Title is the schema name (e.g., "ServerCreateRequest").
	Title string

	// Description is the schema description.
	Description string

	// Fields is the ordered list of form fields.
	Fields []FormField
}

// FormField represents a single field in a form.
type FormField struct {
	// Name is the JSON property name (e.g., "updateStrategy").
	Name string

	// Type is the OpenAPI type: "string", "integer", "boolean", "object", "array".
	Type string

	// Format is the OpenAPI format: "uri", "date-time", "ipv4", etc.
	Format string

	// Description is the field description from the spec.
	Description string

	// Required indicates whether the field is required.
	Required bool

	// Enum lists allowed values for select fields.
	Enum []string

	// Default is the default value from the spec.
	Default string

	// Pattern is a regex pattern for validation.
	Pattern string

	// MinLength is the minimum string length.
	MinLength *int

	// MaxLength is the maximum string length.
	MaxLength *int

	// Minimum is the minimum integer value.
	Minimum *int

	// Maximum is the maximum integer value.
	Maximum *int

	// Example is an example value from the spec.
	Example string

	// Properties holds sub-fields for nested objects.
	Properties []FormField

	// Items describes the schema of array elements.
	Items *FormField

	// AdditionalProperties indicates this is a string map (labels, annotations).
	AdditionalProperties bool

	// Condition controls when this field is visible.
	Condition *Condition

	// ReadOnlyOnEdit marks identity fields that cannot be changed during update.
	ReadOnlyOnEdit bool
}

// Condition represents conditional visibility for a form field.
type Condition struct {
	// DependsOn is the field name this condition depends on.
	DependsOn string

	// Values lists the values of DependsOn for which this field is visible.
	Values []string
}
