#!/usr/bin/env bash
set -euo pipefail
: "${RELEASE_VERSION:?}" "${RELEASE_SHA:?}" "${GITHUB_REPOSITORY:?}" "${RUNNER_TEMP:?}"
[[ "$RELEASE_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+\+custom\.(00[1-9]|0[1-9][0-9]|[1-9][0-9]{2})$ ]] || exit 1
[[ "$RELEASE_SHA" =~ ^[0-9a-f]{40}$ ]] || exit 1
[[ "${DRY_RUN:-false}" == true || "${RELEASE_MODE:-}" == publish ]] || exit 1
owner=${GITHUB_REPOSITORY%%/*}
registry="ghcr.io/${owner,,}/sub2api-plus"
oci_tag="v${RELEASE_VERSION/+custom./-custom.}"
for arch in amd64 arm64; do
  args=(--platform "linux/$arch" --file ".release-context/$arch/Dockerfile"
    --provenance=false --sbom=false
    --label "org.opencontainers.image.version=$RELEASE_VERSION"
    --label "org.opencontainers.image.revision=$RELEASE_SHA"
    --label "org.opencontainers.image.source=https://github.com/$GITHUB_REPOSITORY"
    --tag "$registry:$oci_tag-$arch")
  if [[ ${DRY_RUN:-false} == true ]]; then
    args+=(--output "type=oci,compression=gzip,dest=$RUNNER_TEMP/sub2api-$arch.oci.tar")
  else
    args+=(--push)
  fi
  docker buildx build "${args[@]}" ".release-context/$arch"
done
if [[ ${DRY_RUN:-false} != true ]]; then
  major=${RELEASE_VERSION%%.*}
  minor=${RELEASE_VERSION#*.}; minor=${minor%%.*}
  docker buildx imagetools create \
    --tag "$registry:$oci_tag" --tag "$registry:latest" \
    --tag "$registry:$major.$minor" --tag "$registry:$major" \
    "$registry:$oci_tag-amd64" "$registry:$oci_tag-arm64"
fi
