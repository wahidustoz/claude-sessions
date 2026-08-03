package main

import "testing"

func TestResolveVersionPrefersTheBuildFlag(t *testing.T) {
	if got := resolveVersion("v1.2.3", "v9.9.9"); got != "v1.2.3" {
		t.Errorf("resolveVersion = %q, want v1.2.3 (ldflags win)", got)
	}
}

// `go install pkg@v0.1.0` cannot set ldflags, but Go records the module version
// in the build info, so that is where the version comes from.
func TestResolveVersionFallsBackToModuleVersion(t *testing.T) {
	if got := resolveVersion("dev", "v0.1.0"); got != "v0.1.0" {
		t.Errorf("resolveVersion = %q, want v0.1.0", got)
	}
}

func TestResolveVersionIgnoresAnUnhelpfulModuleVersion(t *testing.T) {
	for _, mod := range []string{"(devel)", "", "devel"} {
		if got := resolveVersion("dev", mod); got != "dev" {
			t.Errorf("resolveVersion(dev, %q) = %q, want dev", mod, got)
		}
	}
}
