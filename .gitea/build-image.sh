#!/usr/bin/env bash
# Build an OCI image with buildctl and push it to the local registry.
#
# CANONICAL COPY: mathias/infra ci-templates/build-image.sh (infra#309).
# Every repo carries a byte-identical copy at .gitea/build-image.sh, written by
# `task ci:sync` and held in place by the drift gate. DO NOT EDIT THE COPIES --
# edit here, re-sync, and the gate will tell you which repos still disagree.
#
# Why this exists: this step was hand-copied into a dozen repos, so migrating it
# off rootless buildah (infra#241) meant eleven near-identical commits, eleven
# verifications, and three separate rediscoveries of the same fix in the two
# repo templates and the gitea-ci skill. The differences between the copies were
# incidental -- a label here, a build-arg there -- never intentional. Those are
# inputs now, not forks of the implementation.
#
# Why buildctl and not buildah: the gitea-runner pod runs with
# `capabilities: drop [ALL]` and `allowPrivilegeEscalation: false`, which
# unconditionally blocks the setuid-root exec that rootless buildah's uid/gid
# remapping needs, and the pod has no OCI runtime at all. buildctl is a thin
# client -- it hands the build to the host's buildkitd over a socket, so the
# privileged layer work happens on the host and this pod's securityContext is
# not in the picture. buildah is absent from the runner image as of baked-v14
# and its absence is asserted at pod start (infra#308).
#
# Inputs, all via environment:
#
#   IMAGE            (required) image name, e.g. cobalt-dingo
#   REGISTRY         default localhost:5000
#   SHA_TAG          default `git rev-parse --short HEAD`
#   VERSION_TAG      optional extra tag, e.g. v1.2.3 on a tag build
#   PUSH_LATEST      default 1; set 0 to skip the :latest tag
#   CONTEXT          default . -- build context directory
#   DOCKERFILE_DIR   default $CONTEXT -- directory holding the Dockerfile
#   DOCKERFILE_NAME  optional, when the file is not named `Dockerfile`
#   TARGET           optional, multi-stage build target (--opt target=)
#   BUILD_ARGS       optional, space-separated K=V pairs
#   BUILD_SECRETS    optional, space-separated buildkit secret specs, each
#                    `id=NAME,src=PATH`. Pass the SPEC only -- the caller
#                    writes the secret to PATH itself. A secret VALUE must
#                    never be passed here: it would land in this script's
#                    argv and therefore in the CI log.
#   EXTRA_LABELS     optional, space-separated K=V pairs
#   GIT_SHA          optional, stamped as org.opencontainers.image.revision
#   IMAGE_SOURCE     optional, stamped as org.opencontainers.image.source
#   DIGEST_FILE      optional path to write the pushed sha256:... digest for $REF
#                    (also written to $GITHUB_OUTPUT as digest=... and ref=... when set)
#
# Note on shell style below: every conditional is a full `if` block, never
# `[ -n "$X" ] && cmd`. Under `set -e` the && form exits the script whenever the
# test is false, which is a real bug this estate has already shipped once.

set -euo pipefail

if [ -z "${IMAGE:-}" ]; then
  echo "build-image: IMAGE is required" >&2
  exit 2
fi

REGISTRY="${REGISTRY:-localhost:5000}"
BUILDKIT_HOST="${BUILDKIT_HOST:-unix:///run/buildkit/buildkitd.sock}"
CONTEXT="${CONTEXT:-.}"
DOCKERFILE_DIR="${DOCKERFILE_DIR:-$CONTEXT}"
PUSH_LATEST="${PUSH_LATEST:-1}"
VERSION_TAG="${VERSION_TAG:-}"

if [ -z "${SHA_TAG:-}" ]; then
  SHA_TAG="$(git rev-parse --short HEAD)"
fi

REF="${REGISTRY}/${IMAGE}:${SHA_TAG}"

if [ -n "${DIGEST_FILE:-}" ]; then
  rm -f "$DIGEST_FILE"
fi

TAR_FILE=""
TMP_DIGEST=""
trap 'rm -f "${TAR_FILE:-}" "${TMP_DIGEST:-}"' EXIT
TAR_FILE="$(mktemp)"
TMP_DIGEST="$(mktemp)"

args=(
  --frontend dockerfile.v0
  --local "context=${CONTEXT}"
  --local "dockerfile=${DOCKERFILE_DIR}"
)

if [ -n "${DOCKERFILE_NAME:-}" ]; then
  args+=(--opt "filename=${DOCKERFILE_NAME}")
fi
if [ -n "${TARGET:-}" ]; then
  args+=(--opt "target=${TARGET}")
fi
for spec in ${BUILD_SECRETS:-}; do
  # Opaque id=NAME,src=PATH -- buildkit reads the file, nothing is echoed.
  args+=(--secret "${spec}")
done
if [ -n "${GIT_SHA:-}" ]; then
  args+=(--opt "label:org.opencontainers.image.revision=${GIT_SHA}")
fi
if [ -n "${IMAGE_SOURCE:-}" ]; then
  args+=(--opt "label:org.opencontainers.image.source=${IMAGE_SOURCE}")
fi
for kv in ${BUILD_ARGS:-}; do
  args+=(--opt "build-arg:${kv}")
done
for kv in ${EXTRA_LABELS:-}; do
  args+=(--opt "label:${kv}")
done

echo "build-image: building ${REF}"
buildctl --addr "$BUILDKIT_HOST" build "${args[@]}" --output "type=oci,dest=${TAR_FILE}"

# buildctl has no registry-push equivalent to `buildah push`, so skopeo copies
# the OCI archive up -- once per tag, from the same archive, so every tag points
# at bit-identical content. Record the pushed manifest digest via --digestfile
# (infra#753) and fail closed if skopeo returns no valid sha256 digest.
PRIMARY_DIGEST=""
push_tag() {
  local target="$1"
  local record="${2:-0}"
  echo "build-image: pushing ${target}"
  : > "$TMP_DIGEST"
  skopeo copy --dest-tls-verify=false --digestfile "$TMP_DIGEST" "oci-archive:${TAR_FILE}" "docker://${target}"
  local raw lines digest
  raw="$(cat "$TMP_DIGEST")"
  raw="${raw%$'\r'}"
  lines="$(wc -l < "$TMP_DIGEST" | tr -d '[:space:]')"
  if [ "$lines" -gt 1 ]; then
    echo "build-image: skopeo digestfile had multiple lines for ${target}" >&2
    return 1
  fi
  digest="$raw"
  if [[ ! "$digest" =~ ^sha256:[0-9a-fA-F]{64}$ ]]; then
    echo "build-image: skopeo produced no valid sha256 digest for ${target}" >&2
    return 1
  fi
  echo "build-image: ${target} digest ${digest}"
  if [ "$record" = "1" ]; then
    PRIMARY_DIGEST="$digest"
  fi
}

push_tag "$REF" 1
if [ "$PUSH_LATEST" = "1" ]; then
  push_tag "${REGISTRY}/${IMAGE}:latest"
fi
if [ -n "$VERSION_TAG" ]; then
  push_tag "${REGISTRY}/${IMAGE}:${VERSION_TAG}"
fi

if [ -n "${DIGEST_FILE:-}" ]; then
  printf '%s\n' "$PRIMARY_DIGEST" > "$DIGEST_FILE"
fi
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  printf 'digest=%s\nref=%s@%s\n' "$PRIMARY_DIGEST" "$REF" "$PRIMARY_DIGEST" >> "$GITHUB_OUTPUT"
fi

echo "✓ build-image: ${REF} pushed"
