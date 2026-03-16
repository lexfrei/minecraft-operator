/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTruncateK8sName_ShortNameUnchanged(t *testing.T) {
	name := "my-server-http-bluemap-web-ui"
	result := truncateK8sName(name)
	assert.Equal(t, name, result)
}

func TestTruncateK8sName_ExactlyMaxLengthUnchanged(t *testing.T) {
	name := strings.Repeat("a", maxK8sNameLength)
	result := truncateK8sName(name)
	assert.Equal(t, name, result)
	assert.Len(t, result, maxK8sNameLength)
}

func TestTruncateK8sName_LongNameTruncatedWithHash(t *testing.T) {
	name := strings.Repeat("a", maxK8sNameLength+100)
	result := truncateK8sName(name)
	assert.LessOrEqual(t, len(result), maxK8sNameLength)
	assert.NotEqual(t, name[:maxK8sNameLength], result,
		"Truncated name should include hash suffix, not just prefix")
}

func TestTruncateK8sName_DifferentLongNamesProduceDifferentResults(t *testing.T) {
	name1 := strings.Repeat("a", 200) + "-suffix-one"
	name2 := strings.Repeat("a", 200) + "-suffix-two"

	result1 := truncateK8sName(name1)
	result2 := truncateK8sName(name2)

	assert.NotEqual(t, result1, result2,
		"Different input names should produce different truncated results")
}

func TestBuildHTTPRoute_LongNameTruncatedTo253(t *testing.T) {
	reconciler := &PaperMCServerReconciler{}

	server := &mcv1beta1.PaperMCServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.Repeat("s", 100),
			Namespace: "default",
		},
		Spec: mcv1beta1.PaperMCServerSpec{
			Gateway: &mcv1beta1.GatewayConfig{
				ParentRefs: []mcv1beta1.GatewayParentRef{{Name: "gw"}},
			},
		},
	}

	hr := mcv1beta1.PluginHTTPRoute{
		PluginName:   strings.Repeat("p", 100),
		EndpointName: strings.Repeat("e", 100),
		Hostname:     "example.com",
	}

	route := reconciler.buildHTTPRoute(server, hr, 8080)
	require.NotNil(t, route)
	assert.LessOrEqual(t, len(route.Name), maxK8sNameLength,
		"HTTPRoute name must not exceed 253 characters")
}
