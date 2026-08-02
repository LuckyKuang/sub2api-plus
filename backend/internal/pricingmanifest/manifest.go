// Package pricingmanifest defines release metadata used to update financial
// model-pricing data without trusting a mutable pricing-data URL.
package pricingmanifest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var releaseVersionPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)\+custom\.(\d{3})$`)

// Manifest binds a release version to an immutable pricing asset and digest.
type Manifest struct {
	Version    string `json:"version"`
	PricingURL string `json:"pricing_url"`
	SHA256     string `json:"sha256"`
}

// Validate checks fields whose semantics do not depend on deployment policy.
// Host authorization belongs to the caller because deployments own their
// trusted pricing-host allowlist. The manifest itself nonetheless requires the
// immutable GitHub Release asset layout so the manifest cannot turn the
// pricing URL into a mutable /latest/ discovery endpoint.
func (m Manifest) Validate() error {
	if _, err := parseVersion(m.Version); err != nil {
		return fmt.Errorf("invalid pricing manifest version: %w", err)
	}
	if m.PricingURL != strings.TrimSpace(m.PricingURL) {
		return fmt.Errorf("pricing manifest URL must not contain surrounding whitespace")
	}
	pricingURL, err := url.Parse(m.PricingURL)
	if err != nil || pricingURL.Host == "" {
		return fmt.Errorf("pricing manifest URL is invalid")
	}
	if pricingURL.RawQuery != "" || pricingURL.Fragment != "" || pricingURL.User != nil {
		return fmt.Errorf("pricing manifest URL must not include query, fragment, or user information")
	}
	expectedPathSuffix := "/releases/download/" + m.Version + "/model-pricing.json"
	decodedPath, err := url.PathUnescape(pricingURL.EscapedPath())
	if err != nil || !strings.HasSuffix(decodedPath, expectedPathSuffix) {
		return fmt.Errorf("pricing manifest URL must reference immutable release asset ending in %s", expectedPathSuffix)
	}
	if len(m.SHA256) != 64 {
		return fmt.Errorf("pricing manifest SHA-256 must contain 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(m.SHA256); err != nil {
		return fmt.Errorf("pricing manifest SHA-256 is invalid: %w", err)
	}
	return nil
}

// Parse strictly decodes and validates one pricing manifest JSON value.
func Parse(raw []byte) (Manifest, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode pricing manifest: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// CompareVersion returns -1, 0, or 1 when left is older than, equal to, or
// newer than right. Pricing releases follow the repository's custom tag format.
func CompareVersion(left, right string) (int, error) {
	leftParts, err := parseVersion(left)
	if err != nil {
		return 0, err
	}
	rightParts, err := parseVersion(right)
	if err != nil {
		return 0, err
	}
	for i := range leftParts {
		if leftParts[i] < rightParts[i] {
			return -1, nil
		}
		if leftParts[i] > rightParts[i] {
			return 1, nil
		}
	}
	return 0, nil
}

func parseVersion(raw string) ([4]int, error) {
	if raw != strings.TrimSpace(raw) {
		return [4]int{}, fmt.Errorf("must not contain surrounding whitespace")
	}
	matches := releaseVersionPattern.FindStringSubmatch(raw)
	if matches == nil {
		return [4]int{}, fmt.Errorf("must use vX.Y.Z+custom.NNN format")
	}
	var result [4]int
	for i := 0; i < len(result); i++ {
		value, err := strconv.Atoi(matches[i+1])
		if err != nil {
			return [4]int{}, fmt.Errorf("parse version component: %w", err)
		}
		result[i] = value
	}
	return result, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("pricing manifest must contain exactly one JSON value")
	}
	return fmt.Errorf("decode pricing manifest trailing data: %w", err)
}
