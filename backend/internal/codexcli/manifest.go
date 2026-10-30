// Package codexcli manages the reviewed official Codex executable. It does not
// implement the Responses protocol or start an app-server.
package codexcli

const targetVersion = "0.165.0"

const releaseURL = "https://github.com/openai/codex/releases/download/rust-v" + targetVersion + "/"

// TargetVersion is the official version reviewed with this application release.
// Installation never resolves a latest release or updates this declaration.
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

// These SHA-256 digests are the official rust-v0.165.0 release asset digests.
// Keep them embedded: release metadata downloaded at installation time cannot
// authorize another executable or another version.
var reviewedArtifacts = map[string]artifact{
	"darwin/arm64": {
		Name:         "codex-aarch64-apple-darwin.tar.gz",
		SHA256:       "78ea9c178d2dc8fde71d6494df3b9eab902fd6d6de0a84e65234220d93bb6144",
		BinarySHA256: "a45099154d7fb4a5ed8a56f40966a95deb938e508bab23e6985035c709e9a85a",
		Size:         102366249,
		Entry:        "codex-aarch64-apple-darwin",
	},
	"darwin/amd64": {
		Name:         "codex-x86_64-apple-darwin.tar.gz",
		SHA256:       "f46451741936ebdf5201b8becd0820a41be2a01d271b98703e0d9a1670c99993",
		BinarySHA256: "f8096ee535ca7af9c57e315e5680f58457ff49dcc5fc619e5ba5eca24293ffe2",
		Size:         111288240,
		Entry:        "codex-x86_64-apple-darwin",
	},
	"linux/arm64": {
		Name:         "codex-aarch64-unknown-linux-musl.tar.gz",
		SHA256:       "6522e056d0be7f531da915c9a81ae05bc70811b782542499cc0a9776d3abf5d3",
		BinarySHA256: "b32ece31af06440da980b733fb8fefdd1f193439595fda84096f1caaceec543f",
		Size:         107804617,
		Entry:        "codex-aarch64-unknown-linux-musl",
	},
	"linux/amd64": {
		Name:         "codex-x86_64-unknown-linux-musl.tar.gz",
		SHA256:       "be635d3486a5bd060d0fff582ae6d30160280c31344dd85719707fa920e187f0",
		BinarySHA256: "c06a67fd8ac76508dc78bd99ce20c9f317b8d568beda59485f3f4134df9962d5",
		Size:         115837472,
		Entry:        "codex-x86_64-unknown-linux-musl",
	},
}
