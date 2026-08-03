package main

import "runtime/debug"

// version is stamped at build time with -ldflags "-X main.version=v1.2.3".
// Builds made by `go install pkg@version` cannot set it, so those fall back to
// the module version Go records in the binary's build info.
var version = "dev"

func resolveVersion(ldflagVersion, moduleVersion string) string {
	if ldflagVersion != "" && ldflagVersion != "dev" {
		return ldflagVersion
	}
	switch moduleVersion {
	case "", "devel", "(devel)":
		return "dev"
	}
	return moduleVersion
}

// buildVersion is the version to report to the user.
func buildVersion() string {
	mod := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		mod = info.Main.Version
	}
	return resolveVersion(version, mod)
}
