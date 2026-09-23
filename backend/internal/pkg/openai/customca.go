package openai

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Custom CA environment variables, aligned with official
// codex-rs/http-client/src/custom_ca.rs: CODEX_CA_CERTIFICATE takes precedence
// over SSL_CERT_FILE, and an empty value means unset. A configured PEM bundle is
// appended to the system roots (official reqwest/rustls keeps the platform
// roots too); a bundle that cannot be read or carries no certificate block fails
// client creation early instead of silently using system roots.
const (
	CodexCAEnvPrimary  = "CODEX_CA_CERTIFICATE"
	CodexCAEnvFallback = "SSL_CERT_FILE"
)

// CodexCAHint matches the official CA_CERT_HINT so operators see the same
// remediation text as the Codex CLI prints.
const CodexCAHint = "If you set CODEX_CA_CERTIFICATE or SSL_CERT_FILE, ensure it points to a PEM file containing one or more CERTIFICATE blocks, or unset it to use system roots."

// CodexCABundle is the resolved custom-CA bundle and the environment variable
// that selected it.
type CodexCABundle struct {
	SourceEnv string
	Path      string
	Pool      *x509.CertPool
}

var (
	codexCAMu        sync.Mutex
	codexCASignature string
	codexCAResolved  CodexCABundle
	codexCAErr       error
)

// CodexCARootPool resolves the custom-CA bundle selected by
// CODEX_CA_CERTIFICATE / SSL_CERT_FILE and returns an error when a configured
// bundle is unusable. Only callers that build an official Codex HTTP client
// consult this; a bad generic SSL_CERT_FILE must not take down unrelated
// providers. The resolved bundle is cached by environment signature.
func CodexCARootPool() (CodexCABundle, error) {
	signature := strings.TrimSpace(os.Getenv(CodexCAEnvPrimary)) + "\x00" + strings.TrimSpace(os.Getenv(CodexCAEnvFallback))
	codexCAMu.Lock()
	defer codexCAMu.Unlock()
	if signature == codexCASignature {
		return codexCAResolved, codexCAErr
	}
	codexCASignature = signature
	codexCAResolved = CodexCABundle{}
	codexCAErr = nil
	for _, env := range []string{CodexCAEnvPrimary, CodexCAEnvFallback} {
		path := strings.TrimSpace(os.Getenv(env))
		if path == "" {
			continue
		}
		codexCAResolved = CodexCABundle{SourceEnv: env, Path: path}
		pool, err := buildCodexCARootPool(path)
		if err != nil {
			codexCAErr = err
			return codexCAResolved, codexCAErr
		}
		codexCAResolved.Pool = pool
		return codexCAResolved, nil
	}
	return codexCAResolved, nil
}

// buildCodexCARootPool appends every parseable certificate block of the bundle
// to the system root pool. Standard CERTIFICATE and OpenSSL-style TRUSTED
// CERTIFICATE labels are both accepted (matching the official PEM variant
// normalization); non-certificate blocks such as CRLs are skipped.
func buildCodexCARootPool(path string) (*x509.CertPool, error) {
	//nolint:gosec // G703: path comes from the operator-set CODEX_CA_CERTIFICATE /
	// SSL_CERT_FILE environment variables, never from request data, so there is no
	// attacker-controlled traversal here. G304 is excluded repo-wide; taint
	// analysis cannot see that the boundary is deployment configuration.
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate file %s: %v. %s", path, err, CodexCAHint)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	added := 0
	for {
		var block *pem.Block
		block, pemBytes = pem.Decode(pemBytes)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" && block.Type != "TRUSTED CERTIFICATE" {
			continue
		}
		certs, parseErr := x509.ParseCertificates(block.Bytes)
		if parseErr != nil || len(certs) == 0 {
			continue
		}
		for _, cert := range certs {
			pool.AddCert(cert)
			added++
		}
	}
	if added == 0 {
		return nil, fmt.Errorf("failed to load CA certificates from %s: no CERTIFICATE block found. %s", path, CodexCAHint)
	}
	return pool, nil
}
