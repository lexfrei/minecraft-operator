/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package v1beta1

// Test fixture literals shared across the v1beta1 webhook test files.
const (
	testConfigCoreConf      = "core.conf"
	testConfigServerProps   = "server.properties"
	testOverwriteAlways     = "always"
	testPathEtcPasswd       = "/etc/passwd"
	testPathTraversalPasswd = "../etc/passwd"
	testEndpointWebUI       = "web-ui"
	testNameBad             = "bad"
	testNameTest            = "test"
	testPluginBlueMap       = "bluemap"
	testCMBlueMapConfig     = "bluemap-config"
	testCMMCConfig          = "mc-config"
	testCMEvil              = "evil"
	testGatewayName         = "my-gateway"
	testHostMCExample       = "mc.example.com"
	testPathMap             = "/map"
	testCronBad             = "bad cron"
	testHostMapExample      = "map.example.com"
	testCMKeyPayload        = "payload"
)
