package service

import (
	"strings"

	"golang.org/x/mod/semver"
)

// IsStrictSemanticVersion validates a full SemVer 2.0 version without the
// optional leading v prefix. Settings that act as version boundaries must be
// unambiguous: shorthand versions such as 1.2 are useful for some outbound
// compatibility paths but are not valid policy bounds.
func IsStrictSemanticVersion(version string) bool {
	version = strings.TrimSpace(version)
	if version == "" || strings.HasPrefix(version, "v") {
		return false
	}
	base := version
	if index := strings.IndexAny(base, "-+"); index >= 0 {
		base = base[:index]
	}
	if strings.Count(base, ".") != 2 {
		return false
	}
	return semver.IsValid("v" + version)
}

// CompareVersions compares version precedence using SemVer rules. It accepts
// the historic optional v prefix and the shorthand forms accepted by
// golang.org/x/mod/semver, preserving outbound compatibility while correctly
// ordering prereleases below the corresponding release.
func CompareVersions(a, b string) int {
	return semver.Compare(comparableSemanticVersion(a), comparableSemanticVersion(b))
}

func comparableSemanticVersion(version string) string {
	version = strings.TrimSpace(version)
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return version
}
