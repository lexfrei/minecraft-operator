/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcv1beta1 "github.com/lexfrei/minecraft-operator/api/v1beta1"
)

func TestBuildServicePorts_PortNamesWithin15Chars(t *testing.T) {
	t.Parallel()

	server := &mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			RCON: mcv1beta1.RCONConfig{Enabled: false},
		},
	}
	plugins := []mcv1beta1.Plugin{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "bluemap-very-long-name"},
			Spec: mcv1beta1.PluginSpec{
				Endpoints: []mcv1beta1.PluginEndpoint{
					{Name: "web-ui-long-endpoint-name", Port: 8100, Protocol: gcProtocolHTTP},
				},
			},
		},
	}

	ports := buildServicePorts(server, plugins)

	for _, port := range ports {
		assert.LessOrEqual(t, len(port.Name), 15,
			"Service port name %q exceeds 15-character IANA limit", port.Name)
	}
}

func TestBuildServicePorts_SamePortDifferentProtocol(t *testing.T) {
	t.Parallel()

	server := &mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			RCON: mcv1beta1.RCONConfig{Enabled: false},
		},
	}
	plugins := []mcv1beta1.Plugin{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "dual-proto"},
			Spec: mcv1beta1.PluginSpec{
				Endpoints: []mcv1beta1.PluginEndpoint{
					{Name: "game-tcp", Port: 8123, Protocol: "TCP"},
					{Name: "game-udp", Port: 8123, Protocol: gcProtocolUDP},
				},
			},
		},
	}

	ports := buildServicePorts(server, plugins)

	var tcpFound, udpFound bool
	for _, port := range ports {
		if port.Port == 8123 && port.Protocol == corev1.ProtocolTCP {
			tcpFound = true
		}
		if port.Port == 8123 && port.Protocol == corev1.ProtocolUDP {
			udpFound = true
		}
	}

	assert.True(t, tcpFound, "Should have TCP port 8123")
	assert.True(t, udpFound, "Should have UDP port 8123")
}

func TestBuildServicePorts_HTTPEndpointCreatesTCP(t *testing.T) {
	t.Parallel()

	server := &mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			RCON: mcv1beta1.RCONConfig{Enabled: false},
		},
	}
	plugins := []mcv1beta1.Plugin{
		{
			ObjectMeta: metav1.ObjectMeta{Name: gcPluginBluemap},
			Spec: mcv1beta1.PluginSpec{
				Endpoints: []mcv1beta1.PluginEndpoint{
					{Name: gcWebUI, Port: 8100, Protocol: gcProtocolHTTP},
				},
			},
		},
	}

	ports := buildServicePorts(server, plugins)

	var found bool
	for _, port := range ports {
		if port.Port == 8100 {
			found = true
			assert.Equal(t, corev1.ProtocolTCP, port.Protocol,
				"HTTP endpoint should create TCP Service port")
		}
	}

	assert.True(t, found, "Should have port 8100")
}

func TestBuildServicePorts_DeduplicateAcrossPlugins(t *testing.T) {
	t.Parallel()

	server := &mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			RCON: mcv1beta1.RCONConfig{Enabled: false},
		},
	}
	plugins := []mcv1beta1.Plugin{
		{
			ObjectMeta: metav1.ObjectMeta{Name: gcPluginDynmap},
			Spec: mcv1beta1.PluginSpec{
				Endpoints: []mcv1beta1.PluginEndpoint{
					{Name: gcWebUI, Port: 8123, Protocol: gcProtocolHTTP},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: gcPluginBluemap},
			Spec: mcv1beta1.PluginSpec{
				Endpoints: []mcv1beta1.PluginEndpoint{
					{Name: gcWebUI, Port: 8123, Protocol: gcProtocolHTTP},
				},
			},
		},
	}

	ports := buildServicePorts(server, plugins)

	count := 0
	for _, port := range ports {
		if port.Port == 8123 {
			count++
		}
	}

	assert.Equal(t, 1, count, "Duplicate port 8123 across plugins should be deduplicated")
}

func TestBuildServicePorts_MinecraftAndRCON(t *testing.T) {
	t.Parallel()

	server := &mcv1beta1.PaperMCServer{
		Spec: mcv1beta1.PaperMCServerSpec{
			RCON: mcv1beta1.RCONConfig{
				Enabled: true,
				Port:    25575,
				PasswordSecret: mcv1beta1.SecretKeyRef{
					Name: gcRCON,
					Key:  "pass",
				},
			},
		},
	}

	ports := buildServicePorts(server, nil)
	require.Len(t, ports, 2)
	assert.Equal(t, gcNamespaceMinecraft, ports[0].Name)
	assert.Equal(t, int32(25565), ports[0].Port)
	assert.Equal(t, gcRCON, ports[1].Name)
	assert.Equal(t, int32(25575), ports[1].Port)
}
