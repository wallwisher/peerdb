#!/usr/bin/env bash
# Build and push the Flow images from a clean, committed PeerDB checkout.
# Default to development; production must be selected explicitly.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

if [[ $# -gt 1 ]]; then
  printf 'Usage: %s [development|production]\n' "$0" >&2
  exit 2
fi

PEERDB_ENVIRONMENT="${1:-development}"
case "$PEERDB_ENVIRONMENT" in
  development)
    PEERDB_GCP_PROJECT=development-234618
    PEERDB_GCP_REPOSITORY=padlet-dev
    ;;
  production)
    PEERDB_GCP_PROJECT=logical-handler-867
    PEERDB_GCP_REPOSITORY=mozart-deploy
    ;;
  *)
    printf 'Unknown environment: %s\nUsage: %s [development|production]\n' "$PEERDB_ENVIRONMENT" "$0" >&2
    exit 2
    ;;
esac

PEERDB_GCP_REGION=us-central1
PEERDB_REGISTRY_HOST="${PEERDB_GCP_REGION}-docker.pkg.dev"
PEERDB_IMAGE_REPOSITORY="${PEERDB_REGISTRY_HOST}/${PEERDB_GCP_PROJECT}/${PEERDB_GCP_REPOSITORY}"
printf 'Target registry: %s\n' "$PEERDB_IMAGE_REPOSITORY"

for command_name in git gcloud docker; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'Required command is missing: %s\n' "$command_name" >&2
    exit 1
  fi
done

if [[ -n "$(git status --porcelain)" ]]; then
  printf 'Commit all PeerDB changes before publishing images. The worktree is not clean.\n' >&2
  git status --short >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1 || ! docker buildx version >/dev/null 2>&1; then
  printf 'Docker and Docker Buildx must be running and available.\n' >&2
  exit 1
fi

# Generated protobufs are gitignored, so a fresh checkout cannot build Flow
# until they are generated. Use the Buf container to avoid a host install.
docker run --rm --user "$(id -u):$(id -g)" --env HOME=/tmp \
  --volume "$PWD:/workspace" --workdir /workspace \
  bufbuild/buf:1.72 generate protos

if ! gcloud --quiet auth print-access-token >/dev/null 2>&1; then
  printf 'A working gcloud login is required to publish images.\n' >&2
  exit 1
fi

gcloud --quiet artifacts repositories describe "$PEERDB_GCP_REPOSITORY" \
  --project="$PEERDB_GCP_PROJECT" \
  --location="$PEERDB_GCP_REGION" \
  --format='value(name)' >/dev/null
gcloud --quiet auth configure-docker "$PEERDB_REGISTRY_HOST" >/dev/null

PEERDB_IMAGE_TAG="$(git rev-parse --short=12 HEAD)"
printf 'Building and pushing Flow images from %s to %s\n' \
  "$(git rev-parse HEAD)" "$PEERDB_IMAGE_REPOSITORY"

REGISTRY="$PEERDB_IMAGE_REPOSITORY" TAG="$PEERDB_IMAGE_TAG" SHA_SHORT="$PEERDB_IMAGE_TAG" \
  docker buildx bake --set '*.platform=linux/amd64' --push \
    flow-api flow-worker flow-snapshot-worker

printf 'Published images:\n'
printf '  %s/%s:%s\n' "$PEERDB_IMAGE_REPOSITORY" flow-api "$PEERDB_IMAGE_TAG"
printf '  %s/%s:%s\n' "$PEERDB_IMAGE_REPOSITORY" flow-worker "$PEERDB_IMAGE_TAG"
printf '  %s/%s:%s\n' "$PEERDB_IMAGE_REPOSITORY" flow-snapshot-worker "$PEERDB_IMAGE_TAG"
