#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

check_application_security_opt() {
  file=$1
  count=$(
    awk '
      $0 == "  sub2api:" {
        in_application = 1
        next
      }
      in_application && $0 ~ /^  [A-Za-z0-9_-]+:$/ {
        in_application = 0
      }
      in_application && $0 == "    security_opt:" {
        in_security_opt = 1
        next
      }
      in_application && in_security_opt && $0 == "      - no-new-privileges:true" {
        count++
      }
      END { print count + 0 }
    ' "$file"
  )

  if [ "$count" -ne 1 ]; then
    printf '%s must enable no-new-privileges exactly once for the sub2api service\n' "$file" >&2
    exit 1
  fi
}

check_application_environment() {
  file=$1
  expected=$2
  count=$(awk -v expected="$expected" '
    $0 == "  sub2api:" {
      in_application = 1
      next
    }
    in_application && $0 ~ /^  [A-Za-z0-9_-]+:$/ {
      in_application = 0
    }
    in_application && $0 == expected {
      count++
    }
    END { print count + 0 }
  ' "$file")

  if [ "$count" -ne 1 ]; then
    printf '%s must pass %s exactly once to the sub2api service\n' "$file" "$expected" >&2
    exit 1
  fi
}

for compose_file in \
  deploy/docker-compose.yml \
  deploy/docker-compose.local.yml \
  deploy/docker-compose.standalone.yml \
  deploy/docker-compose.dev.yml
do
  check_application_security_opt "$compose_file"
  check_application_environment "$compose_file" '      - SERVER_TRUSTED_PROXIES=${SERVER_TRUSTED_PROXIES:-}'
  check_application_environment "$compose_file" '      - SERVER_IP_ACCESS_EMERGENCY_ALLOWLIST=${SERVER_IP_ACCESS_EMERGENCY_ALLOWLIST:-}'
  check_application_environment "$compose_file" '      - SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL=${SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL:-false}'
  check_application_environment "$compose_file" '      - GATEWAY_OPENAI_PROXY_STREAM_CIRCUIT_DISABLED=${GATEWAY_OPENAI_PROXY_STREAM_CIRCUIT_DISABLED:-false}'
  if grep -Eq 'PRICING_MANIFEST_(PUBLIC|SIGNING)_KEY|model-pricing-manifest\.json\.sig' "$compose_file"; then
    printf '%s must not expose obsolete pricing signature configuration\n' "$compose_file" >&2
    exit 1
  fi
done

printf 'docker compose security test passed\n'
