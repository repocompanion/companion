// Package version exposes the companion build version to the CLI and MCP
// server without creating an import cycle between their packages.
package version

import "runtime/debug"

// Version is set by the release build with -ldflags "-X .../version.Version=vX.Y.Z".
// It falls back to the module version reported by the Go toolchain, so
// `go install ...@vX.Y.Z` no longer reports "dev".
var Version = "dev"

// String returns the release version when available, then the module version,
// then the development placeholder.
func String() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	if Version == "" {
		return "dev"
	}
	return Version
}
