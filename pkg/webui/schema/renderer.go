package schema

import (
	"fmt"
	"html"
	"strings"
)

// FormMode indicates whether the form is for creating or editing.
type FormMode string

const (
	// ModeCreate renders a form for creating a new resource.
	ModeCreate FormMode = "create"
	// ModeEdit renders a form for editing an existing resource.
	ModeEdit FormMode = "edit"

	boolTrue  = "true"
	boolFalse = "false"
)

// RenderOptions controls form rendering behavior.
type RenderOptions struct {
	// Mode is create or edit.
	Mode FormMode
	// SubmitURL is the API endpoint the form submits to.
	SubmitURL string
	// SuccessRedirect is where to redirect after success.
	SuccessRedirect string
}

// RenderForm renders a FormSchema into an HTML form string.
// SECURITY: This function's output is injected via templ.Raw(), bypassing autoescaping.
// All dynamic values MUST be passed through html.EscapeString before inclusion.
func RenderForm(schema *FormSchema, values map[string]any, opts RenderOptions) string {
	var b strings.Builder

	writeFormOpen(&b, opts)
	b.WriteString(`<div id="form-result"></div>`)
	b.WriteString(`<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:20px;">`)

	for _, field := range schema.Fields {
		writeField(&b, field, "", values, opts.Mode)
	}

	b.WriteString(`</div>`)
	writeSubmitButton(&b, opts.Mode)
	b.WriteString(`</form>`)

	return b.String()
}

func writeFormOpen(b *strings.Builder, opts RenderOptions) {
	method := "hx-post"
	if opts.Mode == ModeEdit {
		method = "hx-put"
	}

	redirect := opts.SuccessRedirect
	if redirect == "" {
		redirect = "/ui"
	}

	fmt.Fprintf(b,
		`<form id="resource-form" %s="%s" hx-ext="json-enc" `+
			`hx-target="#form-result" hx-swap="innerHTML" data-success-redirect="%s">`,
		method, html.EscapeString(opts.SubmitURL), html.EscapeString(redirect))
}

func writeField(b *strings.Builder, field FormField, prefix string, values map[string]any, mode FormMode) {
	fullName := field.Name
	if prefix != "" {
		fullName = prefix + "." + field.Name
	}

	// Wide fields (maps, arrays, objects) span full grid width
	isWide := field.AdditionalProperties ||
		(field.Type == typeArray && field.Items != nil) ||
		(field.Type == typeObject && len(field.Properties) > 0)

	// Conditional visibility wrapper
	wideStyle := ""
	if isWide {
		wideStyle = "grid-column:1/-1;"
	}
	if field.Condition != nil {
		fmt.Fprintf(b, `<div data-depends-on="%s" data-show-when="%s" style="display:none;%s">`,
			html.EscapeString(field.Condition.DependsOn),
			html.EscapeString(strings.Join(field.Condition.Values, ",")),
			wideStyle)
	} else if isWide {
		fmt.Fprintf(b, `<div style="%s">`, wideStyle)
	} else {
		b.WriteString(`<div>`)
	}

	switch {
	case field.AdditionalProperties:
		writeMapField(b, field, fullName, values)
	case field.Type == typeArray && field.Items != nil:
		writeArrayField(b, field, fullName, values)
	case field.Type == typeObject && len(field.Properties) > 0:
		writeObjectField(b, field, fullName, values, mode)
	default:
		writeScalarField(b, field, fullName, values, mode)
	}

	b.WriteString(`</div>`)
}

func writeScalarField(b *strings.Builder, field FormField, fullName string, values map[string]any, mode FormMode) {
	writeLabel(b, field, fullName)

	value := getStringValue(values, fullName)
	readonly := field.ReadOnlyOnEdit && mode == ModeEdit

	switch {
	case len(field.Enum) > 0:
		writeSelect(b, field, fullName, value, readonly)
	case field.Type == "boolean":
		writeCheckbox(b, fullName, value, field.Default, readonly)
	case field.Type == "integer":
		writeNumberInput(b, field, fullName, value, readonly)
	default:
		writeTextInput(b, field, fullName, value, readonly)
	}

	writeDescription(b, field)
}

func writeLabel(b *strings.Builder, field FormField, fullName string) {
	fmt.Fprintf(b, `<label for="%s" style="display:block;font-size:13px;font-weight:600;`+
		`color:var(--text-secondary);margin-bottom:6px;">`, html.EscapeString(fullName))
	b.WriteString(html.EscapeString(humanize(field.Name)))
	if field.Required {
		b.WriteString(` <span style="color:var(--error);">*</span>`)
	}
	b.WriteString(`</label>`)
}

func writeDescription(b *strings.Builder, field FormField) {
	if field.Description == "" {
		return
	}
	// Take only first sentence for brevity
	desc := field.Description
	if idx := strings.Index(desc, "\n"); idx > 0 {
		desc = desc[:idx]
	}
	desc = strings.TrimSpace(desc)
	if desc != "" {
		fmt.Fprintf(b, `<p style="font-size:11px;color:var(--text-muted);margin-top:4px;">%s</p>`,
			html.EscapeString(desc))
	}
	writeHelpLink(b, field)
}

// helpLinks maps field patterns to documentation URLs.
var helpLinks = map[string]struct{ label, url string }{
	"cron": {
		"Cron syntax reference",
		"https://crontab.guru/",
	},
	"duration": {
		"Go duration format reference",
		"https://pkg.go.dev/time#ParseDuration",
	},
}

func writeHelpLink(b *strings.Builder, field FormField) {
	lowerDesc := strings.ToLower(field.Description)
	if strings.Contains(field.Name, "Cron") || strings.Contains(field.Name, "cron") ||
		strings.Contains(lowerDesc, "cron") {
		h := helpLinks["cron"]
		fmt.Fprintf(b, `<a href="%s" target="_blank" rel="noopener" `+
			`style="font-size:11px;color:var(--accent);margin-top:2px;display:inline-block;">%s</a>`,
			h.url, html.EscapeString(h.label))
	}
	if strings.Contains(lowerDesc, "duration") || strings.Contains(lowerDesc, "go duration") {
		h := helpLinks["duration"]
		fmt.Fprintf(b, `<a href="%s" target="_blank" rel="noopener" `+
			`style="font-size:11px;color:var(--accent);margin-top:2px;display:inline-block;">%s</a>`,
			h.url, html.EscapeString(h.label))
	}
}

func writeTextInput(b *strings.Builder, field FormField, fullName, value string, readonly bool) {
	inputType := "text"
	if field.Format == "uri" {
		inputType = "url"
	}

	fmt.Fprintf(b, `<input type="%s" id="%s" name="%s"`,
		inputType, html.EscapeString(fullName), html.EscapeString(fullName))
	writeValueAttr(b, value)
	writeValidationAttrs(b, field)
	if readonly {
		b.WriteString(` readonly`)
	}
	writeInputStyle(b, readonly)
	b.WriteString(`/>`)
}

func writeNumberInput(b *strings.Builder, field FormField, fullName, value string, readonly bool) {
	fmt.Fprintf(b, `<input type="number" id="%s" name="%s"`,
		html.EscapeString(fullName), html.EscapeString(fullName))
	writeValueAttr(b, value)
	if field.Minimum != nil {
		fmt.Fprintf(b, ` min="%d"`, *field.Minimum)
	}
	if field.Maximum != nil {
		fmt.Fprintf(b, ` max="%d"`, *field.Maximum)
	}
	if field.Required {
		b.WriteString(` required`)
	}
	if readonly {
		b.WriteString(` readonly`)
	}
	writeInputStyle(b, readonly)
	b.WriteString(`/>`)
}

func writeSelect(b *strings.Builder, field FormField, fullName, value string, readonly bool) {
	fmt.Fprintf(b, `<select id="%s" name="%s"`,
		html.EscapeString(fullName), html.EscapeString(fullName))
	if field.Required {
		b.WriteString(` required`)
	}
	if readonly {
		b.WriteString(` disabled`)
	}
	writeInputStyle(b, readonly)
	b.WriteString(`>`)

	if !field.Required {
		b.WriteString(`<option value="">— Select —</option>`)
	}

	for _, opt := range field.Enum {
		selected := ""
		if opt == value || (value == "" && opt == field.Default) {
			selected = " selected"
		}
		fmt.Fprintf(b, `<option value="%s"%s>%s</option>`,
			html.EscapeString(opt), selected, html.EscapeString(opt))
	}

	b.WriteString(`</select>`)
	// Disabled selects are excluded from form submission; add hidden input to preserve value
	if readonly && value != "" {
		fmt.Fprintf(b, `<input type="hidden" name="%s" value="%s"/>`,
			html.EscapeString(fullName), html.EscapeString(value))
	}
}

func writeCheckbox(b *strings.Builder, fullName, value, defaultVal string, readonly bool) {
	checked := value == boolTrue || (value == "" && defaultVal == boolTrue)

	fmt.Fprintf(b, `<input type="checkbox" id="%s" name="%s"`,
		html.EscapeString(fullName), html.EscapeString(fullName))
	if checked {
		b.WriteString(` checked`)
	}
	if readonly {
		b.WriteString(` disabled`)
	}
	b.WriteString(` style="width:auto;margin-top:8px;"/>`)
	// Disabled checkboxes are excluded from form submission; add hidden input to preserve value
	if readonly {
		val := boolFalse
		if checked {
			val = boolTrue
		}
		fmt.Fprintf(b, `<input type="hidden" name="%s" value="%s"/>`,
			html.EscapeString(fullName), val)
	}
}

func writeObjectField(b *strings.Builder, field FormField, fullName string, values map[string]any, mode FormMode) {
	writeLabel(b, field, fullName)
	b.WriteString(`<fieldset style="border:1px solid var(--border);border-radius:6px;padding:15px;margin:0;">`)

	for _, sub := range field.Properties {
		writeField(b, sub, fullName, values, mode)
	}

	b.WriteString(`</fieldset>`)
}

func writeMapField(b *strings.Builder, field FormField, fullName string, values map[string]any) {
	writeLabel(b, field, fullName)
	fmt.Fprintf(b, `<div data-map-field="%s">`, html.EscapeString(fullName))
	b.WriteString(`<div data-map-entries>`)
	// Pre-fill existing key-value pairs
	if mapVal := getMapValue(values, fullName); mapVal != nil {
		for k, v := range mapVal {
			writeMapEntryRow(b, fullName, k, toString(v))
		}
	}
	b.WriteString(`</div>`)
	fmt.Fprintf(b,
		`<button type="button" data-map-add="%s" `+
			`style="margin-top:8px;padding:4px 12px;font-size:12px;background:var(--bg-tertiary);`+
			`color:var(--text-secondary);border:1px solid var(--border);border-radius:4px;cursor:pointer;">+ Add</button>`,
		html.EscapeString(fullName))
	b.WriteString(`</div>`)
}

// TODO: Pre-fill existing array items from values (endpoints, matchExpressions).
func writeArrayField(b *strings.Builder, field FormField, fullName string, _ map[string]any) {
	writeLabel(b, field, fullName)
	fmt.Fprintf(b, `<div data-array-field="%s">`, html.EscapeString(fullName))
	b.WriteString(`<div data-array-entries></div>`)

	// Template for cloning new items
	b.WriteString(`<template data-array-template>`)
	b.WriteString(`<div data-array-item style="border:1px solid var(--border);` +
		`border-radius:4px;padding:10px;margin-bottom:8px;">`)
	if field.Items.Type == typeObject {
		for _, prop := range field.Items.Properties {
			b.WriteString(`<div style="margin-bottom:8px;">`)
			writeLabel(b, prop, fullName+".*."+prop.Name)
			writeTextInput(b, prop, fullName+".*."+prop.Name, "", false)
			b.WriteString(`</div>`)
		}
	}
	b.WriteString(`<button type="button" data-array-remove ` +
		`style="font-size:11px;color:var(--error);background:none;border:none;cursor:pointer;">Remove</button>`)
	b.WriteString(`</div>`)
	b.WriteString(`</template>`)

	fmt.Fprintf(b,
		`<button type="button" data-array-add="%s" `+
			`style="margin-top:8px;padding:4px 12px;font-size:12px;background:var(--bg-tertiary);`+
			`color:var(--text-secondary);border:1px solid var(--border);border-radius:4px;cursor:pointer;">+ Add</button>`,
		html.EscapeString(fullName))
	b.WriteString(`</div>`)
}

func writeSubmitButton(b *strings.Builder, mode FormMode) {
	label := "Create"
	if mode == ModeEdit {
		label = "Update"
	}
	fmt.Fprintf(b,
		`<div style="margin-top:20px;"><button type="submit" `+
			`style="padding:10px 24px;background:var(--accent);color:white;border:none;`+
			`border-radius:6px;font-size:14px;font-weight:600;cursor:pointer;">%s</button></div>`, label)
}

func writeValueAttr(b *strings.Builder, value string) {
	if value != "" {
		fmt.Fprintf(b, ` value="%s"`, html.EscapeString(value))
	}
}

func writeValidationAttrs(b *strings.Builder, field FormField) {
	if field.Required {
		b.WriteString(` required`)
	}
	if field.Pattern != "" {
		fmt.Fprintf(b, ` pattern="%s"`, html.EscapeString(field.Pattern))
	}
	if field.MinLength != nil {
		fmt.Fprintf(b, ` minlength="%d"`, *field.MinLength)
	}
	if field.MaxLength != nil {
		fmt.Fprintf(b, ` maxlength="%d"`, *field.MaxLength)
	}
}

func writeInputStyle(b *strings.Builder, readonly bool) {
	base := `width:100%;padding:8px 10px;background:var(--bg-tertiary);color:var(--text-primary);` +
		`border:1px solid var(--border);border-radius:4px;font-size:14px;`
	if readonly {
		base += `opacity:0.6;cursor:not-allowed;`
	}
	fmt.Fprintf(b, ` style="%s"`, base)
}

// writeMapEntryRow writes a single key-value row for a map field.
func writeMapEntryRow(b *strings.Builder, fieldName, key, value string) {
	b.WriteString(`<div style="display:flex;gap:8px;margin-bottom:6px;align-items:center;">`)
	fmt.Fprintf(b,
		`<input type="text" data-map-key value="%s" placeholder="Key" `+
			`style="flex:1;padding:6px 8px;background:var(--bg-tertiary);color:var(--text-primary);`+
			`border:1px solid var(--border);border-radius:4px;font-size:13px;"/>`,
		html.EscapeString(key))
	fmt.Fprintf(b,
		`<input type="text" data-map-value data-map-name="%s" value="%s" placeholder="Value" `+
			`style="flex:1;padding:6px 8px;background:var(--bg-tertiary);color:var(--text-primary);`+
			`border:1px solid var(--border);border-radius:4px;font-size:13px;"/>`,
		html.EscapeString(fieldName), html.EscapeString(value))
	b.WriteString(`<button type="button" data-map-remove ` +
		`style="font-size:11px;color:var(--error);background:none;border:none;cursor:pointer;">` +
		`Remove</button>`)
	b.WriteString(`</div>`)
}

// getMapValue retrieves a map[string]any value by key from a values map.
func getMapValue(values map[string]any, key string) map[string]any {
	if values == nil {
		return nil
	}
	if v, ok := values[key]; ok {
		switch m := v.(type) {
		case map[string]any:
			return m
		case map[string]string:
			result := make(map[string]any, len(m))
			for k, val := range m {
				result[k] = val
			}
			return result
		}
	}
	return nil
}

// getStringValue retrieves a value by dot-notation key from a flat or nested map.
func getStringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}

	// Try flat key first
	if v, ok := values[key]; ok {
		return toString(v)
	}

	// Try nested navigation
	parts := strings.Split(key, ".")
	var current any = values
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[part]
	}

	return toString(current)
}

// humanize converts a camelCase or snake_case name to a human-readable label.
func humanize(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteRune(' ')
		}
		if i == 0 {
			b.WriteRune(toUpper(r))
		} else {
			b.WriteRune(r)
		}
	}
	result := b.String()
	result = strings.ReplaceAll(result, "_", " ")
	return result
}

func toUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 32
	}
	return r
}
