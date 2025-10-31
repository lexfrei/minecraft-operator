// Package plugins provides clients for plugin repository APIs.
package plugins

// Client defines the interface for plugin repository clients.
type Client interface {
	// FetchVersions retrieves available versions for a plugin.
	FetchVersions(project string) ([]string, error)
}
