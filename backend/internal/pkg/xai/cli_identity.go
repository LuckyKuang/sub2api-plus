package xai

import (
	"net/http"
	"os"
	"strings"

	"golang.org/x/mod/semver"
)

// Fixed Grok Build / CLI-chat-proxy client identity.
// These values are intentionally pinned in-binary (not scraped from live CLI).
// Operators may bump the version via XAI_GROK_CLI_VERSION without a release.
const (
	// CLIProxyHost is the hostname that requires the official CLI identity headers.
	CLIProxyHost = "cli-chat-proxy.grok.com"

	// CLIStableVersion is the oldest official client identity this build advertises.
	CLIStableVersion = "1.0.41"

	// CLIVersionEnv is the optional operator override for CLIStableVersion.
	CLIVersionEnv = "XAI_GROK_CLI_VERSION"

	// CLITokenAuth is required by cli-chat-proxy for Grok Build OAuth tokens.
	CLITokenAuth = "xai-grok-cli"
	// CLIAuthenticateResponse is required by the CLI proxy auth middleware.
	CLIAuthenticateResponse = "authenticate-response"

	// CLIClientIdentifier is the x-grok-client-identifier value used by Grok shell/CLI.
	CLIClientIdentifier = "grok-shell"

	// CLIClientMode identifies this unattended gateway as a headless client.
	CLIClientMode = "headless"
)

// ResolveCLIVersion returns a supported CLI client version.
// Empty or invalid overrides fall back to CLIClientVersion (the pinned
// preferred client pin in billing.go). CLIStableVersion is only the minimum
// accepted by IsSupportedCLIVersion, not the default identity we advertise.
func ResolveCLIVersion() string {
	version := strings.TrimSpace(os.Getenv(CLIVersionEnv))
	if !IsSupportedCLIVersion(version) {
		return CLIClientVersion
	}
	return version
}

// IsSupportedCLIVersion reports whether version is a valid semver string at or
// above CLIStableVersion (prereleases below a higher release are rejected when
// they compare less than the stable pin).
func IsSupportedCLIVersion(version string) bool {
	canonical := "v" + version
	minimum := "v" + CLIStableVersion
	return semver.IsValid(canonical) &&
		semver.Canonical(canonical) == canonical &&
		semver.Compare(canonical, minimum) >= 0
}

// CLIUserAgent builds the official generic Grok shell User-Agent.
func CLIUserAgent(version string) string {
	if strings.TrimSpace(version) == "" {
		version = CLIClientVersion
	}
	return "grok-shell/" + version + " (" + cliPlatformOS() + "; " + cliPlatformArch() + ")"
}

func cliPlatformOS() string   { return "linux" }
func cliPlatformArch() string { return "x86_64" }

// ApplyCLIProxyHeaders stamps the fixed Grok CLI identity when the request
// targets cli-chat-proxy. Direct api.x.ai traffic is left unchanged.
func ApplyCLIProxyHeaders(req *http.Request) {
	if req == nil || req.URL == nil || !strings.EqualFold(strings.TrimSpace(req.URL.Hostname()), CLIProxyHost) {
		return
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	version := ResolveCLIVersion()
	req.Header.Set("X-XAI-Token-Auth", CLITokenAuth)
	req.Header.Set("x-authenticateresponse", CLIAuthenticateResponse)
	req.Header.Set("x-grok-client-version", version)
	req.Header.Set("x-grok-client-identifier", CLIClientIdentifier)
	req.Header.Set("x-grok-client-mode", CLIClientMode)
	req.Header.Set("User-Agent", CLIUserAgent(version))
}
