// Package openapi provides the embedded OpenAPI specification.
package openapi

import _ "embed"

// Spec contains the raw OpenAPI specification YAML.
//
//go:embed openapi.yaml
var Spec []byte
