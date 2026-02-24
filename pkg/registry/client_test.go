/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type listTagsTestCase struct {
	name         string
	responseBody TagsResponse
	statusCode   int
	wantTags     []string
	wantErr      bool
}

func getListTagsTestCases() []listTagsTestCase {
	return []listTagsTestCase{
		{
			name: "successful single page",
			responseBody: TagsResponse{
				Count: 3,
				Results: []TagResult{
					{Name: "latest"},
					{Name: "1.21.10-91"},
					{Name: "1.21.9-88"},
				},
			},
			statusCode: http.StatusOK,
			wantTags:   []string{"latest", "1.21.10-91", "1.21.9-88"},
			wantErr:    false,
		},
		{
			name: "empty repository",
			responseBody: TagsResponse{
				Count:   0,
				Results: []TagResult{},
			},
			statusCode: http.StatusOK,
			wantTags:   []string{},
			wantErr:    false,
		},
		{
			name:         "non-200 status code",
			responseBody: TagsResponse{},
			statusCode:   http.StatusInternalServerError,
			wantTags:     nil,
			wantErr:      true,
		},
	}
}

func TestListTags(t *testing.T) {
	t.Parallel()

	tests := getListTagsTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := createTestServerForListTags(tt)
			defer server.Close()

			client := &Client{
				httpClient: server.Client(),
				baseURL:    server.URL,
			}

			tags, err := client.ListTags(context.Background(), "lexfrei/papermc", 100)

			verifyListTagsResult(t, tags, err, tt)
		})
	}
}

func createTestServerForListTags(tt listTagsTestCase) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(tt.statusCode)
		if tt.statusCode == http.StatusOK {
			_ = json.NewEncoder(w).Encode(tt.responseBody)
		}
	}))
}

func verifyListTagsResult(t *testing.T, tags []string, err error, tt listTagsTestCase) {
	t.Helper()

	if (err != nil) != tt.wantErr {
		t.Errorf("ListTags() error = %v, wantErr %v", err, tt.wantErr)

		return
	}

	if !tt.wantErr {
		if len(tags) != len(tt.wantTags) {
			t.Errorf("ListTags() got %d tags, want %d", len(tags), len(tt.wantTags))

			return
		}

		for i := range tags {
			if tags[i] != tt.wantTags[i] {
				t.Errorf("ListTags() tag[%d] = %v, want %v", i, tags[i], tt.wantTags[i])
			}
		}
	}
}

func TestListTags_Pagination(t *testing.T) {
	t.Parallel()

	// Create test server that serves multiple pages
	var requestCount int
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		requestCount++

		if requestCount == 1 {
			// First page
			resp := TagsResponse{
				Count: 4,
				Next:  serverURL + "/page2",
				Results: []TagResult{
					{Name: "tag1"},
					{Name: "tag2"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		} else {
			// Second page (no next)
			resp := TagsResponse{
				Count: 4,
				Results: []TagResult{
					{Name: "tag3"},
					{Name: "tag4"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	serverURL = server.URL

	client := &Client{
		httpClient: server.Client(),
		baseURL:    server.URL,
	}

	tags, err := client.ListTags(context.Background(), "test/repo", 2)
	if err != nil {
		t.Fatalf("ListTags() unexpected error: %v", err)
	}

	expectedTags := []string{"tag1", "tag2", "tag3", "tag4"}
	if len(tags) != len(expectedTags) {
		t.Errorf("ListTags() got %d tags, want %d", len(tags), len(expectedTags))

		return
	}

	for i := range tags {
		if tags[i] != expectedTags[i] {
			t.Errorf("ListTags() tag[%d] = %v, want %v", i, tags[i], expectedTags[i])
		}
	}

	if requestCount != 2 {
		t.Errorf("Expected 2 requests for pagination, got %d", requestCount)
	}
}

type imageExistsTestCase struct {
	name       string
	tag        string
	statusCode int
	wantExists bool
	wantErr    bool
}

func getImageExistsTestCases() []imageExistsTestCase {
	return []imageExistsTestCase{
		{
			name:       "tag exists",
			tag:        "1.21.10-91",
			statusCode: http.StatusOK,
			wantExists: true,
			wantErr:    false,
		},
		{
			name:       "tag not found",
			tag:        "nonexistent",
			statusCode: http.StatusNotFound,
			wantExists: false,
			wantErr:    false,
		},
		{
			name:       "server error",
			tag:        "error",
			statusCode: http.StatusInternalServerError,
			wantExists: false,
			wantErr:    true,
		},
		{
			name:       "unauthorized",
			tag:        "unauthorized",
			statusCode: http.StatusUnauthorized,
			wantExists: false,
			wantErr:    true,
		},
	}
}

func TestImageExists(t *testing.T) {
	t.Parallel()

	tests := getImageExistsTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := createTestServerForImageExists(tt)
			defer server.Close()

			client := &Client{
				httpClient: server.Client(),
				baseURL:    server.URL,
			}

			exists, err := client.ImageExists(context.Background(), "lexfrei/papermc", tt.tag)

			verifyImageExistsResult(t, exists, err, tt)
		})
	}
}

func createTestServerForImageExists(tt imageExistsTestCase) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(tt.statusCode)
	}))
}

func verifyImageExistsResult(t *testing.T, exists bool, err error, tt imageExistsTestCase) {
	t.Helper()

	if (err != nil) != tt.wantErr {
		t.Errorf("ImageExists() error = %v, wantErr %v", err, tt.wantErr)

		return
	}

	if exists != tt.wantExists {
		t.Errorf("ImageExists() = %v, want %v", exists, tt.wantExists)
	}
}

func TestNewClient(t *testing.T) {
	t.Parallel()

	client := NewClient()

	if client == nil {
		t.Fatal("NewClient() returned nil")
	}

	if client.httpClient == nil {
		t.Error("NewClient() httpClient is nil")
	}

	if client.baseURL != defaultBaseURL {
		t.Errorf("NewClient() baseURL = %v, want %v", client.baseURL, defaultBaseURL)
	}

	if client.httpClient.Timeout != defaultHTTPTimeout {
		t.Errorf("NewClient() timeout = %v, want %v", client.httpClient.Timeout, defaultHTTPTimeout)
	}
}
