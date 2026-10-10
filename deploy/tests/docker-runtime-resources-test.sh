#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

fail() {
  printf 'docker runtime resources test failed: %s\n' "$1" >&2
  exit 1
}

normalized_file() {
  tr -d '\015' < "$1"
}

assert_line() {
  file=$1
  line=$2
  normalized_file "$file" | grep -Fqx "$line" || fail "$file is missing: $line"
}

test -s backend/resources/model-pricing/model_prices_and_context_window.json || \
  fail 'fallback pricing data is missing or empty'

assert_line Dockerfile.goreleaser 'COPY ${TARGETPLATFORM}/sub2api /app/sub2api'
assert_line Dockerfile.goreleaser 'COPY --chown=sub2api:sub2api backend/resources /app/resources'
assert_line deploy/Dockerfile 'COPY --from=backend-builder --chown=sub2api:sub2api /app/backend/resources /app/resources'
assert_line .goreleaser.yaml '      - src: backend/resources/model-pricing/model_prices_and_context_window.json'
assert_line .goreleaser.yaml '        dst: resources/model-pricing'
# Distributed publication owns image contexts outside GoReleaser. Verify the
# actual two contexts retain same-source runtime resources and executable bytes.
python3 .github/release-tools/test_release_matrix.py \
  ReleaseMatrixTest.test_linux_context_uses_targetplatform_and_same_source_resources

printf 'docker runtime resources test passed\n'
