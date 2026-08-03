// pricing-manifest-build generates the immutable pricing asset manifest
// uploaded by the release workflow.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pricingmanifest"
)

func main() {
	var dataPath, repository, tag, manifestPath string
	flag.StringVar(&dataPath, "data", "", "path to model-pricing JSON")
	flag.StringVar(&repository, "repository", "", "GitHub owner/repository")
	flag.StringVar(&tag, "tag", "", "immutable release tag")
	flag.StringVar(&manifestPath, "manifest", "", "output manifest path")
	flag.Parse()

	if dataPath == "" || repository == "" || tag == "" || manifestPath == "" {
		fatal(fmt.Errorf("--data, --repository, --tag, and --manifest are required"))
	}
	if strings.Count(repository, "/") != 1 || strings.ContainsAny(repository, " \t\n") {
		fatal(fmt.Errorf("repository must use owner/name format"))
	}

	data, err := os.ReadFile(dataPath)
	if err != nil {
		fatal(fmt.Errorf("read pricing data: %w", err))
	}
	digest := sha256.Sum256(data)
	manifest := pricingmanifest.Manifest{
		Version:    tag,
		PricingURL: fmt.Sprintf("https://github.com/%s/releases/download/%s/model-pricing.json", repository, tag),
		SHA256:     hex.EncodeToString(digest[:]),
	}
	if err := manifest.Validate(); err != nil {
		fatal(err)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		fatal(fmt.Errorf("marshal pricing manifest: %w", err))
	}
	if err := writeOutput(manifestPath, append(raw, '\n')); err != nil {
		fatal(err)
	}
}

func writeOutput(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "pricing-manifest-build:", err)
	os.Exit(1)
}
