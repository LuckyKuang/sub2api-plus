package service

import "github.com/LuckyKuang/sub2api-plus/internal/config"

func testPluginConfig(root string, allowUnsigned bool) *config.Config {
	return &config.Config{Plugins: config.PluginConfig{
		DataDir:              root,
		AllowUnsigned:        allowUnsigned,
		TrustedPublishers:    map[string]string{},
		MaxUploadBytes:       64 * 1024 * 1024,
		MaxUncompressedBytes: 128 * 1024 * 1024,
		StartTimeoutSeconds:  5,
	}}
}
