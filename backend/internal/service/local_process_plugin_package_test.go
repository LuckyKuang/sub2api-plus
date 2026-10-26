//go:build integration

package service

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"runtime"
	"sort"
	"testing"

	pluginv1 "github.com/LuckyKuang/sub2api-plus/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
)

// localProcessPluginPackage signs an actual executable with an ephemeral publisher
// key. The caller must explicitly trust that key using the production installer;
// neither unsigned installation nor the built-in official trust root is changed.
func localProcessPluginPackage(t *testing.T) (archive []byte, publicKey string, uncompressedSize int64) {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	binary, err := os.ReadFile(executable)
	require.NoError(t, err)
	binaryPath := "runtimes/" + runtime.GOOS + "-" + runtime.GOARCH + "/plugin"
	files := map[string][]byte{
		binaryPath:               binary,
		"ui/index.html":          []byte("<!doctype html><title>Local process fixture</title>"),
		"ui/assets/styles.css":   []byte("body { color: black; }"),
		"ui/assets/bridge-v1.js": []byte("// Local process fixture; no external assets.\n"),
		"ui/assets/app.js":       []byte("// Local process fixture.\n"),
	}
	hashes := make(map[string]string, len(files))
	for name, contents := range files {
		digest := sha256.Sum256(contents)
		hashes[name] = hex.EncodeToString(digest[:])
	}
	manifest := PluginManifest{
		SchemaVersion: 1,
		ID:            "example.fixture.transport",
		Name:          "Local process transport fixture",
		Version:       "0.1.0",
		Requires: PluginRequirements{
			Sub2API:                   ">=0.1.170 <0.2.0",
			RecommendedSub2APIVersion: "0.1.179",
			TestedSub2APIVersions:     []string{"0.1.179"},
			PluginProtocol:            pluginv1.ProtocolVersion,
			TransportAPI:              pluginv1.TransportAPIVersion,
			UIBridge:                  pluginv1.UIBridgeVersion,
		},
		Capabilities: []PluginCapability{{
			ID:          PluginCapabilityOpenAIOAuthOutbound,
			Platform:    PlatformOpenAI,
			AccountType: AccountTypeOAuth,
		}},
		Runtimes: map[string]PluginRuntime{
			runtime.GOOS + "-" + runtime.GOARCH: {Path: binaryPath},
		},
		UI:    PluginUIManifest{Entrypoint: "ui/index.html"},
		Files: hashes,
	}
	manifestRaw, err := json.Marshal(manifest)
	require.NoError(t, err)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signatureRaw, err := json.Marshal(PluginSignature{
		Algorithm: "ed25519",
		KeyID:     "local-process-fixture-v1",
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, manifestRaw)),
	})
	require.NoError(t, err)
	files[pluginManifestFilename] = manifestRaw
	files[pluginSignatureFilename] = signatureRaw
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range names {
		entry, createErr := writer.Create(name)
		require.NoError(t, createErr)
		_, writeErr := entry.Write(files[name])
		require.NoError(t, writeErr)
		uncompressedSize += int64(len(files[name]))
	}
	require.NoError(t, writer.Close())
	return buffer.Bytes(), base64.StdEncoding.EncodeToString(public), uncompressedSize
}
