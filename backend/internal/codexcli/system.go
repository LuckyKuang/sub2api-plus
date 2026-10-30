package codexcli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
)

func (m *Manager) systemExecutable(ctx context.Context, detected string) (string, []byte, error) {
	a := m.artifacts[m.platform]
	if digest, err := verifySystemExecutable(ctx, detected, a.BinarySHA256); err == nil {
		return detected, digest, nil
	}
	// npm/pnpm install a JavaScript launcher. Never run that launcher as the
	// outbound shell: locate its native optional dependency using fixed known
	// package layouts, then require the same pinned official binary digest.
	for _, candidate := range npmNativeCandidates(detected, m.platform, a.Entry) {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		if digest, err := verifySystemExecutable(ctx, resolved, a.BinarySHA256); err == nil {
			return resolved, digest, nil
		}
	}
	return "", nil, errors.New("system CLI does not match the reviewed official native executable")
}

func npmNativeCandidates(launcher, platform, archiveEntry string) []string {
	if filepath.Base(launcher) != "codex.js" || filepath.Base(filepath.Dir(launcher)) != "bin" {
		return nil
	}
	root := filepath.Dir(filepath.Dir(launcher))
	metadata, err := regularFile(filepath.Join(root, "package.json"))
	if err != nil {
		return nil
	}
	defer func() { _ = metadata.Close() }()
	data, err := io.ReadAll(io.LimitReader(metadata, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return nil
	}
	var pkg struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &pkg) != nil || pkg.Name != "@openai/codex" {
		return nil
	}
	packageName := map[string]string{"darwin/arm64": "codex-darwin-arm64", "darwin/amd64": "codex-darwin-x64", "linux/arm64": "codex-linux-arm64", "linux/amd64": "codex-linux-x64"}[platform]
	if packageName == "" {
		return nil
	}
	triple := strings.TrimPrefix(archiveEntry, "codex-")
	relative := filepath.Join("vendor", triple, "bin", "codex")
	return []string{
		filepath.Join(root, "node_modules", "@openai", packageName, relative),
		filepath.Join(root, "..", packageName, relative),
		filepath.Join(root, relative),
	}
}
