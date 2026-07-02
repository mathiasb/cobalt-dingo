// Package version exposes the build version of cobalt-dingo.
package version

// Version is the semantic version of the build. It defaults to "dev" for local
// builds and is overridden at release/build time via:
//
//	-ldflags "-X github.com/mathiasb/cobalt-dingo/internal/version.Version=v1.2.3"
var Version = "dev"
