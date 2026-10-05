package internal

import (
	modulesdk "github.com/Muxcore-Media/core/sdk/go/module"

	manifest "github.com/Muxcore-Media/logging-file"
)

// Version is an optional build-time override set from cmd/module via
// -ldflags -X main.version. When empty (or "dev"/"0.0.0-dev") the reported
// version comes from muxcore.json (ADR-0021).
var Version string

// moduleVersion returns the version this module reports: the ldflags override
// if one was injected, otherwise the version in the embedded muxcore.json.
func moduleVersion() string {
	switch Version {
	case "", "dev", "0.0.0-dev":
		return modulesdk.ManifestVersion(manifest.ManifestJSON)
	}
	return Version
}
