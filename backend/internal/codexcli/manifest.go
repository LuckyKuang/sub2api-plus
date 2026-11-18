// Package codexcli manages the reviewed official Codex executable. It does not
// implement the Responses protocol or start an app-server.
package codexcli

const targetVersion = "0.170.0"

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

// These SHA-256 digests are the official rust-v0.170.0 release asset digests.
// Source: https://github.com/openai/codex/releases/tag/rust-v0.170.0
// Native digests were independently computed from those verified archives.
// Keep them embedded: release metadata downloaded at installation time cannot
// authorize another executable or another version.
var reviewedArtifacts = map[string]artifact{
	"darwin/arm64": {
		Name:         "codex-aarch64-apple-darwin.tar.gz",
		SHA256:       "7c9f21d5cbae4860d14b379fdcf709ff399ce874fd714b58783e1b4199c2b9a9",
		BinarySHA256: "9248cfeebec9b4cc3034d5114caab10fbf426cbfa79dba410c5fa1b72762abd6",
		Size:         114545562,
		Entry:        "codex-aarch64-apple-darwin",
	},
	"darwin/amd64": {
		Name:         "codex-x86_64-apple-darwin.tar.gz",
		SHA256:       "c858bb0149b003a8cf8840a1143c4d953f18bea23ba6ea861d5fdf0b5e29a5a3",
		BinarySHA256: "7803e86b50e10c2c7d992c11aa6640e854ac57746274944a801528cbbfcf23dc",
		Size:         124603199,
		Entry:        "codex-x86_64-apple-darwin",
	},
	"linux/arm64": {
		Name:         "codex-aarch64-unknown-linux-musl.tar.gz",
		SHA256:       "0da6b061f6b5223901a5a73e841bd1466596e2ef620d68823360febed2ff7ebc",
		BinarySHA256: "6a95492ca9d85ee76bd9fc5a4d8f72dd98a6130d1bb409bcfc50e61573106616",
		Size:         121314071,
		Entry:        "codex-aarch64-unknown-linux-musl",
	},
	"linux/amd64": {
		Name:         "codex-x86_64-unknown-linux-musl.tar.gz",
		SHA256:       "d054916ccd28a53820376b8d2f7c875184ca8f40fcd44bbabeb8ee240ce247f5",
		BinarySHA256: "880819aab735adcf16f36cd628940b9f1e652a36c01d10a5d32f83cc84902971",
		Size:         119025052,
		Entry:        "codex-x86_64-unknown-linux-musl",
	},
}
