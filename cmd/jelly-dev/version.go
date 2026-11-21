package main

import (
	"fmt"
	"io"
	"runtime/debug"
)

const devVersion = "dev"

// version is set at link time: go build -ldflags "-X main.version=1.2.3" (GoReleaser does this).
var version = devVersion

// resolveVersion picks the version to show: the linked-in one, else the module version recorded by
// `go install module@version`, else "dev".
func resolveVersion(linked string, info *debug.BuildInfo) string {
	if linked != "" && linked != devVersion {
		return linked
	}
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return devVersion
}

func printVersion(stdout io.Writer) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		info = nil
	}
	fmt.Fprintf(stdout, "%s %s\n", toolName, resolveVersion(version, info))
}
