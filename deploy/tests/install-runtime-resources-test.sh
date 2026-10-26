#!/bin/bash
set -euo pipefail

if [ "${SUB2API_IN_VALIDATION:-}" != 1 ]; then
    echo "Run installer resource regressions inside the project validation container." >&2
    exit 1
fi

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

# Load the actual installer definitions without running its interactive main.
awk '!/^main "\$@"$/' "$ROOT_DIR/deploy/install.sh" > "$TEST_DIR/installer-functions.sh"

fail() {
    echo "installer runtime resources: $*" >&2
    exit 1
}

FALLBACK_REL=resources/model-pricing/model_prices_and_context_window.json
VERSION=v999.0.0+custom.001
ARCHIVE_NAME=sub2api_${VERSION#v}_linux_amd64.tar.gz

make_archive() {
    local scenario=$1
    local fixture="$TEST_DIR/$scenario"
    mkdir -p "$fixture/archive/deploy" "$fixture/download"
    cat > "$fixture/archive/sub2api" <<'BINARY'
#!/bin/sh
printf 'Sub2API Plus v999.0.0+custom.001\n'
BINARY
    chmod 755 "$fixture/archive/sub2api"
    printf 'new service fixture\n' > "$fixture/archive/deploy/sub2api.service"
    case "$scenario" in
        missing) ;;
        wrong-layout)
            mkdir -p "$fixture/archive/resources"
            cp "$ROOT_DIR/backend/$FALLBACK_REL" "$fixture/archive/resources/model-pricing"
            ;;
        *)
            mkdir -p "$fixture/archive/resources/model-pricing"
            if [ "$scenario" = empty ]; then
                : > "$fixture/archive/$FALLBACK_REL"
            else
                python3 - "$ROOT_DIR/backend/$FALLBACK_REL" "$fixture/archive/$FALLBACK_REL" <<'PY'
import json
import sys
from pathlib import Path

data = json.loads(Path(sys.argv[1]).read_text())
data["installer-offline-fixture"] = {
    "input_cost_per_token": 0.000000123,
    "output_cost_per_token": 0.000000456,
    "litellm_provider": "stepfun",
    "mode": "chat",
}
Path(sys.argv[2]).write_text(json.dumps(data))
PY
            fi
            printf 'bundled resource fixture\n' > "$fixture/archive/resources/.release-info"
            # Releases must remain service-readable even under a restrictive umask.
            chmod 700 "$fixture/archive/resources" "$fixture/archive/resources/model-pricing"
            chmod 600 "$fixture/archive/$FALLBACK_REL"
            ;;
    esac
    tar -czf "$fixture/download/$ARCHIVE_NAME" -C "$fixture/archive" .
    (cd "$fixture/download" && sha256sum "$ARCHIVE_NAME" > checksums.txt)
}

seed_existing_install() {
    local scenario=$1
    local installed="$TEST_DIR/$scenario/installed"
    mkdir -p "$installed/resources/model-pricing" "$installed/data"
    cat > "$installed/sub2api" <<'BINARY'
#!/bin/sh
printf 'Sub2API Plus v998.0.0+custom.001\n'
BINARY
    chmod 755 "$installed/sub2api"
    printf '{"stale-fixture":{"input_cost_per_token":9}}\n' > "$installed/$FALLBACK_REL"
    printf 'operator config; preserve verbatim\n' > "$installed/config.yaml"
    printf 'operator data; preserve verbatim\n' > "$installed/data/preserved"
    printf 'operator resource; preserve verbatim\n' > "$installed/resources/operator-file"
    cp "$installed/sub2api" "$TEST_DIR/$scenario/old-binary"
    cp "$installed/$FALLBACK_REL" "$TEST_DIR/$scenario/old-fallback"
}

run_install() {
    local scenario=$1
    local operation=$2
    bash -s -- "$TEST_DIR/installer-functions.sh" "$TEST_DIR/$scenario" "$operation" "$VERSION" "$ARCHIVE_NAME" <<'HARNESS'
set -euo pipefail
source "$1"
FIXTURE_DIR=$2
OPERATION=$3
LATEST_VERSION=$4
ARCHIVE_NAME=$5
INSTALL_DIR="$FIXTURE_DIR/installed"
OS=linux
ARCH=amd64
LANG_CHOICE=en
SERVICE_USER=synthetic-installer-service
umask 077

# Only network and systemd/account ownership are isolated. Archive extraction,
# checksums, file installation and the actual installer flow execute unchanged.
curl() {
    local url= output= previous=
    local arg
    for arg in "$@"; do
        if [ "$previous" = -o ]; then output=$arg; fi
        case "$arg" in https://*) url=$arg ;; esac
        previous=$arg
    done
    case "$url" in
        "https://github.com/luckykuang/sub2api-plus/releases/download/$LATEST_VERSION/$ARCHIVE_NAME")
            command cp "$FIXTURE_DIR/download/$ARCHIVE_NAME" "$output"
            ;;
        "https://github.com/luckykuang/sub2api-plus/releases/download/$LATEST_VERSION/checksums.txt")
            command cp "$FIXTURE_DIR/download/checksums.txt" "$output"
            ;;
        https://api.github.com/repos/luckykuang/sub2api-plus/releases/latest)
            printf '{"tag_name":"%s"}\n' "$LATEST_VERSION"
            ;;
        "https://api.github.com/repos/luckykuang/sub2api-plus/releases/tags/$LATEST_VERSION")
            printf '200'
            ;;
        *) echo "Unexpected network request: $url" >&2; return 90 ;;
    esac
}
chown() { printf '%s\n' "$*" >> "$FIXTURE_DIR/ownership"; }
systemctl() {
    printf '%s\n' "$*" >> "$FIXTURE_DIR/systemd"
    case "$1" in
        is-active|stop) return 0 ;;
        start)
            cmp "$FIXTURE_DIR/archive/sub2api" "$INSTALL_DIR/sub2api"
            cmp "$FIXTURE_DIR/archive/resources/model-pricing/model_prices_and_context_window.json" \
                "$INSTALL_DIR/resources/model-pricing/model_prices_and_context_window.json"
            ;;
        *) echo "Unexpected systemctl: $*" >&2; return 91 ;;
    esac
}
cp() {
    case "${FIXTURE_DIR##*/}:${*: -1}" in
        fail-resource-copy:"$INSTALL_DIR/resources/"|fail-resource-copy:"$INSTALL_DIR/resources")
            echo "synthetic resource copy failure" >&2; return 74 ;;
        fail-binary-copy:"$INSTALL_DIR/sub2api")
            echo "synthetic binary copy failure" >&2; return 74 ;;
        fail-deploy-copy:"$INSTALL_DIR/")
            echo "synthetic deployment copy failure" >&2; return 74 ;;
    esac
    command cp "$@"
}

case "$OPERATION" in
    fresh) download_and_extract ;;
    upgrade) upgrade ;;
    exact) install_version "$LATEST_VERSION" ;;
    *) exit 92 ;;
esac
HARNESS
}

assert_preserved() {
    local installed="$TEST_DIR/$1/installed"
    test "$(cat "$installed/config.yaml")" = 'operator config; preserve verbatim' || fail "$1 config changed"
    test "$(cat "$installed/data/preserved")" = 'operator data; preserve verbatim' || fail "$1 data changed"
    test "$(cat "$installed/resources/operator-file")" = 'operator resource; preserve verbatim' || fail "$1 local resource changed"
}

assert_installed() {
    local scenario=$1
    local installed="$TEST_DIR/$scenario/installed"
    test -x "$installed/sub2api" || fail "$scenario binary not executable"
    cmp "$TEST_DIR/$scenario/archive/sub2api" "$installed/sub2api" || fail "$scenario wrong binary"
    test -f "$installed/$FALLBACK_REL" || fail "$scenario default fallback is not a regular file"
    cmp "$TEST_DIR/$scenario/archive/$FALLBACK_REL" "$installed/$FALLBACK_REL" || fail "$scenario missing or stale bundled pricing"
    cmp "$TEST_DIR/$scenario/archive/resources/.release-info" "$installed/resources/.release-info" || fail "$scenario other bundled resource missing"
    cmp "$TEST_DIR/$scenario/archive/deploy/sub2api.service" "$installed/sub2api.service" || fail "$scenario deploy file missing"
    python3 - "$installed" <<'PY'
import os
import stat
import sys
from pathlib import Path

root = Path(sys.argv[1])
for relative in ("resources", "resources/model-pricing"):
    mode = stat.S_IMODE((root / relative).stat().st_mode)
    assert mode & 0o005 == 0o005, (relative, oct(mode), "service cannot traverse/read")
path = root / "resources/model-pricing/model_prices_and_context_window.json"
assert stat.S_IMODE(path.stat().st_mode) & 0o004, "service cannot read fallback"
assert os.access(path, os.R_OK)
PY
    if [ -n "${SUB2API_INSTALL_PRICING_PROBE:-}" ]; then
        "$SUB2API_INSTALL_PRICING_PROBE" "$installed" "$scenario"
    fi
}

for scenario in fresh upgrade exact; do
    make_archive "$scenario"
    if [ "$scenario" != fresh ]; then seed_existing_install "$scenario"; fi
    run_install "$scenario" "$scenario" > "$TEST_DIR/$scenario/output" 2>&1 || {
        cat "$TEST_DIR/$scenario/output" >&2
        fail "$scenario installation failed"
    }
    assert_installed "$scenario"
    if [ "$scenario" != fresh ]; then
        assert_preserved "$scenario"
        grep -Fxq 'start sub2api' "$TEST_DIR/$scenario/systemd" || fail "$scenario did not start installed version"
        if [ "$scenario" = upgrade ]; then
            cmp "$TEST_DIR/$scenario/old-binary" "$TEST_DIR/$scenario/installed/sub2api.backup" || fail 'upgrade backup changed'
        fi
    fi
    echo "installer runtime resources: $scenario passed"
done

for scenario in missing empty wrong-layout fail-resource-copy fail-binary-copy fail-deploy-copy; do
    make_archive "$scenario"
    seed_existing_install "$scenario"
    if run_install "$scenario" fresh > "$TEST_DIR/$scenario/output" 2>&1; then
        cat "$TEST_DIR/$scenario/output" >&2
        fail "$scenario unexpectedly reported success"
    fi
    if grep -Fq 'Binary installed to' "$TEST_DIR/$scenario/output"; then
        fail "$scenario printed an installation success message"
    fi
    assert_preserved "$scenario"
    case "$scenario" in
        missing|empty|wrong-layout)
            cmp "$TEST_DIR/$scenario/old-binary" "$TEST_DIR/$scenario/installed/sub2api" || fail "$scenario replaced binary before validating resources"
            cmp "$TEST_DIR/$scenario/old-fallback" "$TEST_DIR/$scenario/installed/$FALLBACK_REL" || fail "$scenario replaced existing pricing"
            ;;
    esac
    echo "installer runtime resources: $scenario rejected without success"
done

echo 'installer runtime resources regressions passed'
