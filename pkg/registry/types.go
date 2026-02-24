/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package registry

// TagsResponse represents Docker Hub API response for tags listing.
type TagsResponse struct {
	Count    int         `json:"count"`
	Next     string      `json:"next,omitempty"`
	Previous string      `json:"previous,omitempty"`
	Results  []TagResult `json:"results"`
}

// TagResult represents a single tag in the response.
type TagResult struct {
	Name string `json:"name"`
}
