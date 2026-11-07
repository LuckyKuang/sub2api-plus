// Package codexcli manages the reviewed official Codex executable. It does not
// implement the Responses protocol or start an app-server.
package codexcli

const targetVersion = "0.167.0"

const releaseURL = "https://github.com/openai/codex/releases/download/rust-v" + targetVersion + "/"

// TargetVersion is the official version reviewed with this application release.
// Its manifest advances with the compiled outbound identity; the consistency
// gate rejects drift. Installation never resolves latest or changes identity.
func TargetVersion() string { return targetVersion }

type artifact struct {
	Name   string
	SHA256 string
	// BinarySHA256 authorizes directly installed native executables. Empty
	// values never authorize a system CLI; managed archives remain verifiable.
	BinarySHA256 string
	Size         int64
	Entry        string
}

// These SHA-256 digests are the official rust-v0.167.0 release asset digests.
// Source: https://github.com/openai/codex/releases/tag/rust-v0.167.0
// Native digests were independently computed from those verified archives.
// Keep them embedded: release metadata downloaded at installation time cannot
// authorize another executable or another version.
var reviewedArtifacts = map[string]artifact{
	"darwin/arm64": {
		Name:         "codex-aarch64-apple-darwin.tar.gz",
		SHA256:       "4b22a2a971be2791c3e31f912f3e77a88ebd64a65812a7289ccba65dd6425fe9",
		BinarySHA256: "030316ecfbb41f4e7946e584b0063ef333b7a36cd312ba349f2f6869e39b62cc",
		Size:         108379331,
		Entry:        "codex-aarch64-apple-darwin",
	},
	"darwin/amd64": {
		Name:         "codex-x86_64-apple-darwin.tar.gz",
		SHA256:       "d8eb4a865a0b17319cde10347523e5bab0978d01725b39f4a639f40905e01221",
		BinarySHA256: "a3b4df639ed5808100fe9d3dacdb324c9fb50b2804d72d5ab59def67bb2d6ab6",
		Size:         117941240,
		Entry:        "codex-x86_64-apple-darwin",
	},
	"linux/arm64": {
		Name:         "codex-aarch64-unknown-linux-musl.tar.gz",
		SHA256:       "843489acb4d4b1079745c36b03c5963b887b83155af208fd53584e33ee8eb281",
		BinarySHA256: "2fe94d848540ff2a6868c7ebcc494126a974aa409711676d14fa0379e49e6143",
		Size:         114300898,
		Entry:        "codex-aarch64-unknown-linux-musl",
	},
	"linux/amd64": {
		Name:         "codex-x86_64-unknown-linux-musl.tar.gz",
		SHA256:       "9d64836106d5b374e90ee4454c7ebcde7ca7106558bd7e41a34b9f26ebda251a",
		BinarySHA256: "cdcb45aaa01d0f14e298985e09465aebccd47f64ecd298aea44814af7b62bbda",
		Size:         122587028,
		Entry:        "codex-x86_64-unknown-linux-musl",
	},
}
