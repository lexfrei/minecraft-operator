// Package crdmanager handles embedding and applying CRDs at operator startup.
package crdmanager

import (
	"embed"
)

//go:embed crds/*.yaml
var crdFS embed.FS
