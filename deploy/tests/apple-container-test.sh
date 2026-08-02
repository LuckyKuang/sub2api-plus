#!/bin/bash

set -euo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "${TEST_DIR}/.." && pwd)"
SCRIPT="${DEPLOY_DIR}/apple-container.sh"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/sub2api-apple-test.XXXXXX")"
STATE_DIR="${TEST_ROOT}/state"
ENV_FILE="${TEST_ROOT}/sub2api.env"

cleanup() {
    rm -rf "${TEST_ROOT}"
}
trap cleanup EXIT

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_exists() {
    [[ -e "$1" ]] || fail "Expected path to exist: $1"
}

assert_missing() {
    [[ ! -e "$1" ]] || fail "Expected path to be absent: $1"
}

count_command() {
    local wanted=$1

    awk -v wanted="${wanted}" '$0 == wanted { count++ } END { print count + 0 }' \
        "${STATE_DIR}/commands.log"
}

export FAKE_CONTAINER_STATE="${STATE_DIR}"
export PATH="${TEST_DIR}/fixtures/bin:${PATH}"
export SUB2API_ENV_FILE="${ENV_FILE}"

mkdir -p "${STATE_DIR}"

"${SCRIPT}" init
[[ "$(stat -f '%Lp' "${ENV_FILE}")" == "600" ]] || fail "init did not create a mode-600 env file"
grep -q '^POSTGRES_PASSWORD=change_this_secure_password$' "${ENV_FILE}" && fail "init retained the placeholder password"

chmod 644 "${ENV_FILE}"
if "${SCRIPT}" up >/dev/null 2>&1; then
    fail "up accepted an insecure env file"
fi
chmod 600 "${ENV_FILE}"

"${SCRIPT}" up
assert_exists "${STATE_DIR}/containers/sub2api-apple"
assert_exists "${STATE_DIR}/containers/sub2api-apple-postgres"
assert_exists "${STATE_DIR}/containers/sub2api-apple-redis"
assert_exists "${STATE_DIR}/running/sub2api-apple"
"${SCRIPT}" status >/dev/null
"${SCRIPT}" disk-usage | grep -q '^Images '

app_deletes_before="$(count_command "delete sub2api-apple")"
postgres_deletes_before="$(count_command "delete sub2api-apple-postgres")"
redis_deletes_before="$(count_command "delete sub2api-apple-redis")"
"${SCRIPT}" up
[[ "$(count_command "delete sub2api-apple")" -eq $((app_deletes_before + 1)) ]] || \
    fail "ordinary up did not replace the application container"
[[ "$(count_command "delete sub2api-apple-postgres")" -eq "${postgres_deletes_before}" ]] || \
    fail "ordinary up unexpectedly replaced PostgreSQL"
[[ "$(count_command "delete sub2api-apple-redis")" -eq "${redis_deletes_before}" ]] || \
    fail "ordinary up unexpectedly replaced Redis"

touch "${STATE_DIR}/images/fail-next-pull"
if "${SCRIPT}" upgrade >/dev/null 2>&1; then
    fail "upgrade accepted a failed application image pull"
fi
assert_exists "${STATE_DIR}/images/rollback-digest"
rm -f "${STATE_DIR}/images/fail-next-pull"

printf '%s\n' "sha256:2222222222222222222222222222222222222222222222222222222222222222" \
    >"${STATE_DIR}/images/next-digest"
touch "${STATE_DIR}/fail-host-probe"
if "${SCRIPT}" upgrade --prune-previous-image >/dev/null 2>&1; then
    fail "upgrade accepted a failed application host-port health probe"
fi
assert_exists "${STATE_DIR}/images/rollback-digest"
grep -q '^sha256:1111111111111111111111111111111111111111111111111111111111111111$' \
    "${STATE_DIR}/images/rollback-digest" || fail "failed deployment did not retain the previous application image"
rm -f "${STATE_DIR}/fail-host-probe"

printf '%s\n' "sha256:3333333333333333333333333333333333333333333333333333333333333333" \
    >"${STATE_DIR}/images/next-digest"
postgres_deletes_before="$(count_command "delete sub2api-apple-postgres")"
redis_deletes_before="$(count_command "delete sub2api-apple-redis")"
"${SCRIPT}" upgrade
assert_exists "${STATE_DIR}/images/rollback-digest"
grep -q '^sha256:2222222222222222222222222222222222222222222222222222222222222222$' \
    "${STATE_DIR}/images/rollback-digest" || fail "upgrade did not retain the previous application image"
[[ "$(count_command "delete sub2api-apple-postgres")" -eq "${postgres_deletes_before}" ]] || \
    fail "application upgrade unexpectedly replaced PostgreSQL"
[[ "$(count_command "delete sub2api-apple-redis")" -eq "${redis_deletes_before}" ]] || \
    fail "application upgrade unexpectedly replaced Redis"

printf '%s\n' "sha256:4444444444444444444444444444444444444444444444444444444444444444" \
    >"${STATE_DIR}/images/next-digest"
"${SCRIPT}" upgrade --prune-previous-image
assert_missing "${STATE_DIR}/images/rollback-digest"
grep -q '^localhost/sub2api-apple-rollback:previous$' "${STATE_DIR}/deleted-images.log" || \
    fail "upgrade did not delete the previous application image when requested"

"${SCRIPT}" up --recreate
assert_exists "${STATE_DIR}/running/sub2api-apple"

cat >>"${ENV_FILE}" <<EOF
APPLE_CONTAINER_SUB2API_DATA_DIR=${TEST_ROOT}/bind/app
APPLE_CONTAINER_POSTGRES_DATA_DIR=${TEST_ROOT}/bind/postgres
APPLE_CONTAINER_REDIS_DATA_DIR=${TEST_ROOT}/bind/redis
EOF
"${SCRIPT}" up --recreate
assert_exists "${TEST_ROOT}/bind/app"
assert_exists "${TEST_ROOT}/bind/postgres"
assert_exists "${TEST_ROOT}/bind/redis"
grep -F -- "--mount type=bind,source=${TEST_ROOT}/bind/app,target=/app/storage" "${STATE_DIR}/create-args.log" >/dev/null || fail "app bind mount was not passed to Apple container"
grep -F -- "--mount type=bind,source=${TEST_ROOT}/bind/postgres,target=/var/lib/postgresql" "${STATE_DIR}/create-args.log" >/dev/null || fail "postgres bind mount was not passed to Apple container"
grep -F -- "--mount type=bind,source=${TEST_ROOT}/bind/redis,target=/var/lib/redis" "${STATE_DIR}/create-args.log" >/dev/null || fail "redis bind mount was not passed to Apple container"

printf '%s\n' "${TEST_ROOT}/wrong-app-bind" >"${STATE_DIR}/mount-sources/sub2api-apple"
if "${SCRIPT}" cleanup --legacy-volumes --yes >/dev/null 2>&1; then
    fail "legacy volume cleanup accepted an unverified application bind mount"
fi
assert_exists "${STATE_DIR}/volumes/sub2api-apple-data"
assert_exists "${STATE_DIR}/volumes/sub2api-apple-postgres-data"
assert_exists "${STATE_DIR}/volumes/sub2api-apple-redis-data"
printf '%s\n' "${TEST_ROOT}/bind/app" >"${STATE_DIR}/mount-sources/sub2api-apple"

touch "${STATE_DIR}/containers/foreign-container"
printf '%s\n' "example.invalid/foreign:latest" >"${STATE_DIR}/container-images/foreign-container"
printf '%s\n' "sub2api-apple-data" >"${STATE_DIR}/mount-sources/foreign-container"
printf '%s\n' "/foreign-data" >"${STATE_DIR}/mount-destinations/foreign-container"
if "${SCRIPT}" cleanup --legacy-volumes --yes >/dev/null 2>&1; then
    fail "legacy volume cleanup accepted a volume referenced by another container"
fi
assert_exists "${STATE_DIR}/volumes/sub2api-apple-data"
rm -f "${STATE_DIR}/containers/foreign-container"
rm -f "${STATE_DIR}/container-images/foreign-container"
rm -f "${STATE_DIR}/mount-sources/foreign-container"
rm -f "${STATE_DIR}/mount-destinations/foreign-container"

printf 'n\n' | "${SCRIPT}" cleanup --legacy-volumes >/dev/null
assert_exists "${STATE_DIR}/volumes/sub2api-apple-data"
assert_exists "${STATE_DIR}/volumes/sub2api-apple-postgres-data"
assert_exists "${STATE_DIR}/volumes/sub2api-apple-redis-data"

"${SCRIPT}" cleanup --legacy-volumes --yes >/dev/null
assert_missing "${STATE_DIR}/volumes/sub2api-apple-data"
assert_missing "${STATE_DIR}/volumes/sub2api-apple-postgres-data"
assert_missing "${STATE_DIR}/volumes/sub2api-apple-redis-data"
assert_exists "${TEST_ROOT}/bind/app"
assert_exists "${TEST_ROOT}/bind/postgres"
assert_exists "${TEST_ROOT}/bind/redis"

dangling_cleanup_output="$(printf 'n\n' | "${SCRIPT}" cleanup --dangling-images 2>&1)"
[[ "${dangling_cleanup_output}" == *"may affect other projects"* ]] || \
    fail "dangling-image cleanup did not warn about its global scope before cancellation"
assert_missing "${STATE_DIR}/images-pruned"
"${SCRIPT}" cleanup --dangling-images --yes >/dev/null
assert_exists "${STATE_DIR}/images-pruned"

"${SCRIPT}" down
assert_missing "${STATE_DIR}/running/sub2api-apple"
assert_missing "${STATE_DIR}/running/sub2api-apple-postgres"
assert_missing "${STATE_DIR}/running/sub2api-apple-redis"

"${SCRIPT}" destroy --yes
assert_missing "${STATE_DIR}/containers/sub2api-apple"
assert_missing "${STATE_DIR}/networks/sub2api-apple"
assert_missing "${STATE_DIR}/volumes/sub2api-apple-data"

"${SCRIPT}" up
"${SCRIPT}" destroy --volumes --yes
assert_missing "${STATE_DIR}/volumes/sub2api-apple-data"
assert_missing "${STATE_DIR}/volumes/sub2api-apple-postgres-data"
assert_missing "${STATE_DIR}/volumes/sub2api-apple-redis-data"
assert_exists "${TEST_ROOT}/bind/app"
assert_exists "${TEST_ROOT}/bind/postgres"
assert_exists "${TEST_ROOT}/bind/redis"

touch "${STATE_DIR}/system-running"
touch "${STATE_DIR}/containers/sub2api-apple"
touch "${STATE_DIR}/unowned/container/sub2api-apple"
if "${SCRIPT}" status >/dev/null 2>&1; then
    fail "status accepted an unowned same-name container"
fi

printf 'Apple container lifecycle tests passed.\n'
