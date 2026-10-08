#!/bin/bash
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
fixture_dir=$(mktemp -d)
trap 'rm -rf "$fixture_dir"' EXIT
mkdir -p "$fixture_dir/bin" "$fixture_dir/first" "$fixture_dir/second" "$fixture_dir/failed"
export BOOTSTRAP_TEST_REPO="$repo_root"
export BOOTSTRAP_TEST_OPENSSL
BOOTSTRAP_TEST_OPENSSL=$(command -v openssl)

# Use the tracked deployment templates without network access.
cat > "$fixture_dir/bin/curl" <<'SH'
#!/bin/bash
set -eu
case "$2" in
  https://raw.githubusercontent.com/luckykuang/sub2api-plus/main/deploy/docker-compose.local.yml)
    cp "$BOOTSTRAP_TEST_REPO/deploy/docker-compose.local.yml" "$4" ;;
  https://raw.githubusercontent.com/luckykuang/sub2api-plus/main/deploy/.env.example)
    cp "$BOOTSTRAP_TEST_REPO/deploy/.env.example" "$4" ;;
  *) exit 1 ;;
esac
SH
cat > "$fixture_dir/bin/openssl" <<'SH'
#!/bin/bash
set -eu
if [[ "${BOOTSTRAP_TEST_FAIL_RANDOM:-}" == 1 && "$*" == 'rand -hex 6' ]]; then
  exit 1
fi
exec "$BOOTSTRAP_TEST_OPENSSL" "$@"
SH
chmod +x "$fixture_dir/bin/curl" "$fixture_dir/bin/openssl"
export PATH="$fixture_dir/bin:$PATH"

for installation in first second; do
  (cd "$fixture_dir/$installation" && bash "$repo_root/deploy/docker-deploy.sh" > setup.log 2>&1)
  grep -Eq '^ADMIN_EMAIL=admin-[0-9a-f]{12}@sub2api\.local$' "$fixture_dir/$installation/.env"
  grep -Fxq 'ADMIN_PASSWORD=' "$fixture_dir/$installation/.env"
done
first_email=$(sed -n 's/^ADMIN_EMAIL=//p' "$fixture_dir/first/.env")
second_email=$(sed -n 's/^ADMIN_EMAIL=//p' "$fixture_dir/second/.env")
[[ "$first_email" != "$second_email" ]] || { echo 'Fresh installations must receive distinct login emails' >&2; exit 1; }

# Declining replacement must preserve the existing installation's credentials.
(cd "$fixture_dir/first" && printf 'n\n' | bash "$repo_root/deploy/docker-deploy.sh" > repeat.log 2>&1)
[[ "$first_email" == "$(sed -n 's/^ADMIN_EMAIL=//p' "$fixture_dir/first/.env")" ]]

if (cd "$fixture_dir/failed" && BOOTSTRAP_TEST_FAIL_RANDOM=1 bash "$repo_root/deploy/docker-deploy.sh" > setup.log 2>&1); then
  echo 'Randomness failure must stop deployment preparation' >&2
  exit 1
fi
[[ ! -e "$fixture_dir/failed/.env" ]]
printf 'deployment bootstrap credential test passed\n'
