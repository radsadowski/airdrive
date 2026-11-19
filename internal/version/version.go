// Package version carries the build version stamped at link time.
package version

// Version is injected by the build with
// -ldflags "-X github.com/radosny/airdrive/internal/version.Version=<version>".
// Untagged local builds report "dev".
var Version = "dev"
