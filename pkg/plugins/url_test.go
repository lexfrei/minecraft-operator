package plugins_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lexfrei/minecraft-operator/pkg/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDownloadURL(t *testing.T) {
	t.Run("accepts valid HTTPS URL", func(t *testing.T) {
		err := plugins.ValidateDownloadURL("https://example.com/plugins/my-plugin-1.0.0.jar")
		require.NoError(t, err)
	})

	t.Run("rejects HTTP URL", func(t *testing.T) {
		err := plugins.ValidateDownloadURL("http://example.com/plugins/my-plugin.jar")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTTPS")
	})

	t.Run("rejects empty URL", func(t *testing.T) {
		err := plugins.ValidateDownloadURL("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("rejects URL without host", func(t *testing.T) {
		err := plugins.ValidateDownloadURL("https:///no-host.jar")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "host")
	})

	t.Run("rejects malformed URL", func(t *testing.T) {
		err := plugins.ValidateDownloadURL("://not-a-url")
		require.Error(t, err)
	})
}

func TestBuildURLVersion(t *testing.T) {
	t.Run("builds version with all fields", func(t *testing.T) {
		pv := plugins.BuildURLVersion(
			"https://example.com/plugin.jar",
			"1.2.3",
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		)
		assert.Equal(t, "1.2.3", pv.Version)
		assert.Equal(t, "https://example.com/plugin.jar", pv.DownloadURL)
		assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", pv.Hash)
		assert.Empty(t, pv.MinecraftVersions)
		assert.Empty(t, pv.PaperVersions)
	})

	t.Run("defaults version to 0.0.0 when empty", func(t *testing.T) {
		pv := plugins.BuildURLVersion("https://example.com/plugin.jar", "", "")
		assert.Equal(t, "0.0.0", pv.Version)
	})

	t.Run("preserves empty checksum", func(t *testing.T) {
		pv := plugins.BuildURLVersion("https://example.com/plugin.jar", "1.0.0", "")
		assert.Empty(t, pv.Hash)
	})
}

func TestFetchJARMetadata(t *testing.T) {
	t.Run("extracts metadata from plugin.yml", func(t *testing.T) {
		jarBytes := buildTestJAR(t, "plugin.yml", `
name: TestPlugin
version: "2.5.0"
api-version: "1.21"
main: com.example.TestPlugin
`)
		server := serveJAR(t, jarBytes)
		meta, err := plugins.FetchJARMetadata(context.Background(), server.URL+"/plugin.jar", server.Client())
		require.NoError(t, err)

		assert.Equal(t, "TestPlugin", meta.Name)
		assert.Equal(t, "2.5.0", meta.Version)
		assert.Equal(t, "1.21", meta.APIVersion)
		assert.NotEmpty(t, meta.SHA256, "SHA256 should be computed")

		expectedHash := fmt.Sprintf("%x", sha256.Sum256(jarBytes))
		assert.Equal(t, expectedHash, meta.SHA256)
	})

	t.Run("extracts metadata from paper-plugin.yml", func(t *testing.T) {
		jarBytes := buildTestJAR(t, "paper-plugin.yml", `
name: PaperTestPlugin
version: "3.0.0"
api-version: "1.20"
main: com.example.PaperTestPlugin
`)
		server := serveJAR(t, jarBytes)
		meta, err := plugins.FetchJARMetadata(context.Background(), server.URL+"/plugin.jar", server.Client())
		require.NoError(t, err)

		assert.Equal(t, "PaperTestPlugin", meta.Name)
		assert.Equal(t, "3.0.0", meta.Version)
		assert.Equal(t, "1.20", meta.APIVersion)
	})

	t.Run("handles missing optional fields", func(t *testing.T) {
		jarBytes := buildTestJAR(t, "plugin.yml", `
name: MinimalPlugin
main: com.example.MinimalPlugin
`)
		server := serveJAR(t, jarBytes)
		meta, err := plugins.FetchJARMetadata(context.Background(), server.URL+"/plugin.jar", server.Client())
		require.NoError(t, err)

		assert.Equal(t, "MinimalPlugin", meta.Name)
		assert.Empty(t, meta.Version)
		assert.Empty(t, meta.APIVersion)
		assert.NotEmpty(t, meta.SHA256)
	})
}

func TestFetchJARMetadata_Errors(t *testing.T) {
	t.Run("returns error when no plugin.yml found", func(t *testing.T) {
		jarBytes := buildTestJAR(t, "README.txt", "not a plugin")
		server := serveJAR(t, jarBytes)

		_, err := plugins.FetchJARMetadata(context.Background(), server.URL+"/plugin.jar", server.Client())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "plugin.yml")
	})

	t.Run("returns error on HTTP failure", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		_, err := plugins.FetchJARMetadata(context.Background(), server.URL+"/missing.jar", server.Client())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "404")
	})

	t.Run("returns error on invalid ZIP", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not a zip file"))
		}))
		defer server.Close()

		_, err := plugins.FetchJARMetadata(context.Background(), server.URL+"/broken.jar", server.Client())
		require.Error(t, err)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("data"))
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := plugins.FetchJARMetadata(ctx, server.URL+"/plugin.jar", server.Client())
		require.Error(t, err)
	})
}

// serveJAR creates a TLS test server that serves the given JAR bytes.
func serveJAR(t *testing.T, jarBytes []byte) *httptest.Server {
	t.Helper()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/java-archive")
		_, _ = w.Write(jarBytes)
	}))
	t.Cleanup(server.Close)

	return server
}

// buildTestJAR creates an in-memory JAR (ZIP) file with a single entry.
func buildTestJAR(t *testing.T, filename, content string) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	f, err := w.Create(filename)
	require.NoError(t, err)

	_, err = f.Write([]byte(content))
	require.NoError(t, err)

	require.NoError(t, w.Close())

	return buf.Bytes()
}
