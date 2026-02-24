package plugins_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lexfrei/minecraft-operator/pkg/plugins"
	"github.com/lexfrei/minecraft-operator/pkg/testutil"
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

func TestValidateDownloadURL_BlockedHosts(t *testing.T) {
	blocked := []struct {
		name string
		url  string
	}{
		{"loopback IP", "https://127.0.0.1/plugin.jar"},
		{"private IP 10.x", "https://10.0.0.1/plugin.jar"},
		{"private IP 172.16.x", "https://172.16.0.1/plugin.jar"},
		{"private IP 192.168.x", "https://192.168.1.1/plugin.jar"},
		{"link-local IP", "https://169.254.169.254/latest/meta-data/"},
		{"localhost hostname", "https://localhost/plugin.jar"},
		{"kubernetes service DNS", "https://kubernetes.default.svc/plugin.jar"},
		{"kubernetes cluster-local DNS", "https://myservice.default.svc.cluster.local/plugin.jar"},
		{"GCP metadata endpoint", "https://metadata.google.internal/computeMetadata/v1/"},
		{"AWS metadata endpoint", "https://instance-data.ec2.internal/latest/meta-data/"},
		{"IPv6 loopback", "https://[::1]/plugin.jar"},
		{"IPv4-mapped loopback", "https://[::ffff:127.0.0.1]/plugin.jar"},
		{"IPv4-mapped link-local", "https://[::ffff:169.254.169.254]/plugin.jar"},
		{"IPv4-mapped private 10.x", "https://[::ffff:10.0.0.1]/plugin.jar"},
		{"IPv4-mapped private 192.168.x", "https://[::ffff:192.168.1.1]/plugin.jar"},
		{"IPv4-mapped private 172.16.x", "https://[::ffff:172.16.0.1]/plugin.jar"},
		{"decimal-encoded loopback", "https://2130706433/plugin.jar"},
		{"decimal-encoded private 10.x", "https://167772161/plugin.jar"},
		{"decimal-encoded link-local", "https://2852039166/plugin.jar"},
		{"unspecified address", "https://0.0.0.0/plugin.jar"},
		{"loopback IP with port", "https://127.0.0.1:8080/plugin.jar"},
		{"mDNS .local domain", "https://myhost.local/plugin.jar"},
		{"arbitrary .internal domain", "https://evil.internal/plugin.jar"},
	}
	for _, tc := range blocked {
		t.Run("blocks "+tc.name, func(t *testing.T) {
			err := plugins.ValidateDownloadURL(tc.url)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "blocked")
		})
	}

	allowed := []struct {
		name string
		url  string
	}{
		{"public IP", "https://1.2.3.4/plugin.jar"},
		{"public hostname", "https://github.com/user/repo/releases/download/v1.0/plugin.jar"},
	}
	for _, tc := range allowed {
		t.Run("allows "+tc.name, func(t *testing.T) {
			err := plugins.ValidateDownloadURL(tc.url)
			require.NoError(t, err)
		})
	}
}

func TestSafeHTTPClient(t *testing.T) {
	t.Run("has timeout configured", func(t *testing.T) {
		client := plugins.SafeHTTPClient()
		assert.NotZero(t, client.Timeout, "SafeHTTPClient should have a non-zero timeout")
	})

	t.Run("blocks redirect to private host", func(t *testing.T) {
		// Create a server on loopback that redirects to another loopback address.
		// Both are 127.0.0.1 (blocked host), so the redirect target is blocked.
		evilServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("evil"))
		}))
		defer evilServer.Close()

		redirectServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, evilServer.URL+"/evil", http.StatusFound)
		}))
		defer redirectServer.Close()

		// SafeHTTPClient won't work with test TLS servers out of the box,
		// so we create a client with the same CheckRedirect policy but the test TLS config.
		client := redirectServer.Client()
		safeClient := plugins.SafeHTTPClient()
		client.CheckRedirect = safeClient.CheckRedirect

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, redirectServer.URL+"/start", nil)
		require.NoError(t, err)

		_, err = client.Do(req) //nolint:bodyclose // Error expected, no body to close.
		require.Error(t, err)
		assert.Contains(t, err.Error(), "redirect to blocked host")
	})

	t.Run("allows redirect to public host", func(t *testing.T) {
		client := plugins.SafeHTTPClient()

		// Simulate a redirect to a public CDN host (e.g., GitHub releases → CDN).
		req, err := http.NewRequestWithContext(
			context.Background(), http.MethodGet,
			"https://objects.githubusercontent.com/release-asset.jar", nil,
		)
		require.NoError(t, err)

		// CheckRedirect should allow this since the target is a public host.
		via := []*http.Request{req}
		err = client.CheckRedirect(req, via)
		require.NoError(t, err)
	})
}

func TestDownloadJAR(t *testing.T) {
	t.Run("downloads JAR bytes successfully", func(t *testing.T) {
		jarBytes := testutil.BuildTestJAR("plugin.yml", "name: TestPlugin\n")
		server := serveJAR(t, jarBytes)

		body, err := plugins.DownloadJAR(context.Background(), server.URL+"/plugin.jar", server.Client())
		require.NoError(t, err)
		assert.Equal(t, jarBytes, body)
	})

	t.Run("returns error on HTTP failure", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		_, err := plugins.DownloadJAR(context.Background(), server.URL+"/missing.jar", server.Client())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP 404")
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("data"))
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := plugins.DownloadJAR(ctx, server.URL+"/plugin.jar", server.Client())
		require.Error(t, err)
	})

	t.Run("sends User-Agent header", func(t *testing.T) {
		var receivedUA string
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedUA = r.Header.Get("User-Agent")
			w.Header().Set("Content-Type", "application/java-archive")
			_, _ = w.Write(testutil.BuildTestJAR("plugin.yml", "name: Test\n"))
		}))
		defer server.Close()

		_, err := plugins.DownloadJAR(context.Background(), server.URL+"/plugin.jar", server.Client())
		require.NoError(t, err)
		assert.Equal(t, "minecraft-operator", receivedUA)
	})

	t.Run("uses SafeHTTPClient when nil client provided", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := plugins.DownloadJAR(ctx, "https://example.com/plugin.jar", nil)
		require.Error(t, err, "Should fail due to cancelled context, not panic from nil client")
	})
}

func TestDownloadJAR_SizeLimitAndErrors(t *testing.T) {
	t.Run("rejects JAR exceeding maximum size", func(t *testing.T) {
		// Serve a response larger than maxJARSize (100MB).
		// Uses streaming writes to avoid allocating 100MB in the test.
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/java-archive")
			chunk := make([]byte, 64*1024)
			total := 100*1024*1024 + 2
			for written := 0; written < total; written += len(chunk) {
				remaining := total - written
				if remaining < len(chunk) {
					chunk = chunk[:remaining]
				}
				_, _ = w.Write(chunk)
			}
		}))
		defer server.Close()

		_, err := plugins.DownloadJAR(context.Background(), server.URL+"/huge.jar", server.Client())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum size")
	})

	t.Run("returns error with HTTP status code on failure", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		_, err := plugins.DownloadJAR(context.Background(), server.URL+"/forbidden.jar", server.Client())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP 403")
	})
}

func TestParseJARMetadata(t *testing.T) {
	t.Run("extracts metadata from plugin.yml", func(t *testing.T) {
		jarBytes := testutil.BuildTestJAR("plugin.yml", "name: TestPlugin\nversion: \"2.5.0\"\napi-version: \"1.21\"\n")

		meta, err := plugins.ParseJARMetadata(jarBytes)
		require.NoError(t, err)

		assert.Equal(t, "TestPlugin", meta.Name)
		assert.Equal(t, "2.5.0", meta.Version)
		assert.Equal(t, "1.21", meta.APIVersion)
	})

	t.Run("returns error for invalid ZIP", func(t *testing.T) {
		_, err := plugins.ParseJARMetadata([]byte("not a zip file"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ZIP")
	})

	t.Run("returns error when no plugin.yml found", func(t *testing.T) {
		jarBytes := testutil.BuildTestJAR("README.txt", "not a plugin")

		_, err := plugins.ParseJARMetadata(jarBytes)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "plugin.yml")
	})

	t.Run("rejects oversized plugin.yml to prevent zip bomb", func(t *testing.T) {
		// Create a plugin.yml that decompresses to >10MB (maxPluginYMLSize).
		// Repeated content compresses extremely well in ZIP, keeping the test fast.
		oversized := strings.Repeat("key: value\n", 1024*1024+1) // ~11MB
		jarBytes := testutil.BuildTestJAR("plugin.yml", oversized)

		_, err := plugins.ParseJARMetadata(jarBytes)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum size")
	})
}

func TestParseJARMetadata_PaperPreference(t *testing.T) {
	t.Run("prefers paper-plugin.yml over plugin.yml when both exist", func(t *testing.T) {
		jarBytes := testutil.BuildTestJARMulti(map[string]string{
			"plugin.yml":       "name: BukkitPlugin\nversion: \"1.0.0\"\napi-version: \"1.20\"\n",
			"paper-plugin.yml": "name: PaperPlugin\nversion: \"2.0.0\"\napi-version: \"1.21\"\n",
		})

		meta, err := plugins.ParseJARMetadata(jarBytes)
		require.NoError(t, err)

		assert.Equal(t, "PaperPlugin", meta.Name, "Should prefer paper-plugin.yml")
		assert.Equal(t, "2.0.0", meta.Version, "Should use version from paper-plugin.yml")
		assert.Equal(t, "1.21", meta.APIVersion, "Should use api-version from paper-plugin.yml")
	})

	t.Run("extracts metadata from paper-plugin.yml alone", func(t *testing.T) {
		jarBytes := testutil.BuildTestJAR("paper-plugin.yml",
			"name: PaperTestPlugin\nversion: \"3.0.0\"\napi-version: \"1.20\"\n")

		meta, err := plugins.ParseJARMetadata(jarBytes)
		require.NoError(t, err)

		assert.Equal(t, "PaperTestPlugin", meta.Name)
		assert.Equal(t, "3.0.0", meta.Version)
		assert.Equal(t, "1.20", meta.APIVersion)
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
