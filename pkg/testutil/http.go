/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package testutil

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
)

// MockHTTPServer is a test HTTP server for simulating JAR downloads.
type MockHTTPServer struct {
	Server *httptest.Server

	mu sync.Mutex

	// Track download requests
	Downloads []string

	// Control behavior
	FailOnPath map[string]bool
	FileData   map[string][]byte
}

// NewMockHTTPServer creates a new mock HTTP server.
func NewMockHTTPServer() *MockHTTPServer {
	mock := &MockHTTPServer{
		Downloads:  make([]string, 0),
		FailOnPath: make(map[string]bool),
		FileData:   make(map[string][]byte),
	}

	mock.Server = httptest.NewServer(http.HandlerFunc(mock.handler))
	return mock
}

// handler handles HTTP requests for the mock server.
func (m *MockHTTPServer) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := r.URL.Path
	m.Downloads = append(m.Downloads, path)

	// Check if we should fail for this path
	if m.FailOnPath[path] {
		http.Error(w, "Simulated download failure", http.StatusInternalServerError)
		return
	}

	// Return file data if available
	if data, ok := m.FileData[path]; ok {
		w.Header().Set("Content-Type", "application/java-archive")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	// Default: return fake JAR data
	fakeData := []byte(fmt.Sprintf("fake-jar-data-for-%s", path))
	w.Header().Set("Content-Type", "application/java-archive")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(fakeData)
}

// AddFile adds a file to be served at the given path.
func (m *MockHTTPServer) AddFile(path string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.FileData[path] = data
}

// SetFailOnPath makes the server return an error for the given path.
func (m *MockHTTPServer) SetFailOnPath(path string, fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.FailOnPath[path] = fail
}

// GetDownloads returns a copy of recorded download paths.
func (m *MockHTTPServer) GetDownloads() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	downloads := make([]string, len(m.Downloads))
	copy(downloads, m.Downloads)
	return downloads
}

// Reset clears all tracking data.
func (m *MockHTTPServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Downloads = make([]string, 0)
	m.FailOnPath = make(map[string]bool)
	m.FileData = make(map[string][]byte)
}

// Close shuts down the mock server.
func (m *MockHTTPServer) Close() {
	m.Server.Close()
}

// URL returns the base URL of the mock server.
func (m *MockHTTPServer) URL() string {
	return m.Server.URL
}

// ComputeSHA256 computes the SHA256 hash of data.
func ComputeSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)
}
