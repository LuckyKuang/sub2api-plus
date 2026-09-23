//go:build unit || !integration

package openai

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 只按 env 路径做缓存键不够：运维修好 PEM 文件但没改 env 时，缓存的错误会一直
// 用到进程重启，而 PAT 校验与 agent task 注册共用这份缓存。
func TestCodexCARootPool_RepairedFileIsReRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, codexTestCACertPEM(t), 0o600))
	t.Setenv(CodexCAEnvPrimary, path)
	t.Setenv(CodexCAEnvFallback, "")

	// Break the bundle: the cached entry is an error.
	require.NoError(t, os.WriteFile(path, []byte("not a pem bundle"), 0o600))
	if _, err := CodexCARootPool(); err == nil {
		t.Fatal("a broken bundle must report an error")
	}

	// Repair the file without touching the environment. The size/mtime signature
	// must make the repaired bundle visible.
	require.NoError(t, os.WriteFile(path, codexTestCACertPEM(t), 0o600))
	// mtime granularity can hide a same-second rewrite; force a later stamp.
	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, future, future))

	bundle, err := CodexCARootPool()
	require.NoError(t, err, "a repaired bundle must be picked up without a restart")
	require.NotNil(t, bundle.Pool)
}

// 文件没变时不重复解析（缓存命中）。
func TestCodexCARootPool_CachesUnchangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, codexTestCACertPEM(t), 0o600))
	t.Setenv(CodexCAEnvPrimary, path)
	t.Setenv(CodexCAEnvFallback, "")

	first, err := CodexCARootPool()
	require.NoError(t, err)
	require.NotNil(t, first.Pool)

	second, err := CodexCARootPool()
	require.NoError(t, err)
	require.Equal(t, first.Path, second.Path)
	require.NotNil(t, second.Pool)
}

func codexTestCACertPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sub2api-codex-ca-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
