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

assert_count() {
  file=$1
  line=$2
  expected=$3
  actual=$(normalized_file "$file" | grep -Fxc "$line" || true)
  [ "$actual" -eq "$expected" ] || fail "$file has $actual occurrences of '$line', expected $expected"
}

test -s backend/resources/model-pricing/model_prices_and_context_window.json || \
  fail 'fallback pricing data is missing or empty'

assert_line Dockerfile.goreleaser 'COPY ${TARGETPLATFORM}/sub2api /app/sub2api'
assert_line Dockerfile.goreleaser 'COPY --chown=sub2api:sub2api backend/resources /app/resources'
assert_line deploy/Dockerfile 'COPY --from=backend-builder --chown=sub2api:sub2api /app/backend/resources /app/resources'
assert_line .goreleaser.yaml '      - src: backend/resources/model-pricing/model_prices_and_context_window.json'
assert_line .goreleaser.yaml '        dst: resources/model-pricing'
assert_count .goreleaser.yaml '    extra_files: [deploy/docker-entrypoint.sh, backend/resources]' 6

printf 'docker runtime resources test passed\n'
