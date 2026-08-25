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

TAR_FILE="$(mktemp)"
trap 'rm -f "$TAR_FILE"' EXIT

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
# at bit-identical content.
push_tag() {
  echo "build-image: pushing ${1}"
  skopeo copy --dest-tls-verify=false "oci-archive:${TAR_FILE}" "docker://${1}"
}

push_tag "$REF"
if [ "$PUSH_LATEST" = "1" ]; then
  push_tag "${REGISTRY}/${IMAGE}:latest"
fi
if [ -n "$VERSION_TAG" ]; then
  push_tag "${REGISTRY}/${IMAGE}:${VERSION_TAG}"
fi

echo "✓ build-image: ${REF} pushed"
