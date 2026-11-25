#!/usr/bin/env bash
# =============================================================================
# Fork: build the PatchMon server image (with its bundled agents) under the
# fork's version and push it to Harbor.
#
# Agents only update when the server's bundled agent version is HIGHER than
# their own, so every fork build needs a version above the upstream release it
# is based on, yet below the next upstream release. Fork releases are tagged as
# that release plus "btl", which the fork's version comparison
# (server-source-code/internal/util/version_fork.go and
# frontend/src/utils/versionFork.js) ranks ABOVE the release itself:
#
#   2.1.8 (upstream) < 2.1.8-btl.1 < 2.1.8-btl.1.4 (4 commits after the tag)
#     < 2.1.8-btl.2 < 2.1.9 (upstream) < 2.1.9-btl.1
#
# Nothing in the repository carries the version, so it never conflicts in a
# merge; only the tag does.
#
# Release:   git tag v2.1.8-btl.1 && git push origin v2.1.8-btl.1
#            ./docker/build-fork.sh --image harbor.example.com/project/patchmon --push
#
# After merging upstream release vX.Y.Z, tag vX.Y.Z-btl.1 before the next
# build; this script refuses to build below a release it can see.
#
# Usage:
#   ./docker/build-fork.sh --print-version         # show the version and exit
#   ./docker/build-fork.sh                          # build patchmon-server:<version>
#   ./docker/build-fork.sh --image I [--push]       # also tag (and push) I:<version>
#   ./docker/build-fork.sh --allow-dirty            # build with uncommitted changes
#   PATCHMON_IMAGE=I ./docker/build-fork.sh --push
# =============================================================================

set -euo pipefail

FORK_ID="btl"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

IMAGE="${PATCHMON_IMAGE:-}"
PUSH="false"
ALLOW_DIRTY="false"
PRINT_ONLY="false"

die() { echo "error: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --image) IMAGE="${2:-}"; shift 2 ;;
    --push) PUSH="true"; shift ;;
    --allow-dirty) ALLOW_DIRTY="true"; shift ;;
    --print-version) PRINT_ONLY="true"; shift ;;
    -h|--help) sed -n '2,31p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done

if [ "$PUSH" = "true" ] && [ -z "$IMAGE" ]; then
  die "--push needs --image (or PATCHMON_IMAGE), e.g. harbor.example.com/project/patchmon"
fi

# The highest fork tag in HEAD's history, not the nearest: after an upstream
# merge the new tag may share a commit with the previous one.
FORK_TAG="$(git tag --merged HEAD --list "v*-${FORK_ID}.*" | sort -V | tail -1)"
if [ -z "$FORK_TAG" ]; then
  die "no fork release tag (v*-${FORK_ID}.N) is reachable from HEAD.
  Tag the first one on the upstream release this branch is based on, e.g.:
      git tag v2.1.8-${FORK_ID}.1 && git push origin v2.1.8-${FORK_ID}.1"
fi
if ! printf '%s' "$FORK_TAG" | grep -qE "^v[0-9]+\.[0-9]+\.[0-9]+-${FORK_ID}\.[0-9]+$"; then
  die "fork tag '$FORK_TAG' is not vMAJOR.MINOR.PATCH-${FORK_ID}.N"
fi
BASE="${FORK_TAG#v}"
CORE="${BASE%%-*}"

# A merged upstream release above our core would outrank this build, and its
# agents would never take ours. Only visible when upstream tags are fetched
# (git fetch upstream --tags), which the merge workflow does.
RELEASE_TAG="$(git describe --tags --abbrev=0 --match 'v[0-9]*' --exclude 'v*-*' 2>/dev/null || true)"
if [ -n "$RELEASE_TAG" ]; then
  RELEASE="${RELEASE_TAG#v}"
  HIGHEST="$(printf '%s\n%s\n' "$RELEASE" "$CORE" | sort -V | tail -1)"
  if [ "$RELEASE" != "$CORE" ] && [ "$HIGHEST" = "$RELEASE" ]; then
    die "upstream release $RELEASE_TAG is merged, so ${FORK_TAG} would sort below it.
  Tag the fork release on it first: git tag ${RELEASE_TAG}-${FORK_ID}.1"
  fi
fi

DISTANCE="$(git rev-list --count "${FORK_TAG}..HEAD")"
VERSION="$BASE"
if [ "$DISTANCE" -gt 0 ]; then
  VERSION="${BASE}.${DISTANCE}"
fi

if [ "$PRINT_ONLY" = "true" ]; then
  echo "$VERSION"
  exit 0
fi

# The version names a commit; uncommitted changes would ship under it.
if [ "$ALLOW_DIRTY" != "true" ] && ! git diff --quiet HEAD --; then
  die "uncommitted changes would be built as $VERSION. Commit them, or pass --allow-dirty for a test build."
fi

echo "==> Fork version: $VERSION (tag $FORK_TAG + $DISTANCE commits)"
"$ROOT/docker/build.sh" --version "$VERSION" --tag "$VERSION"

if [ -n "$IMAGE" ]; then
  TARGET="${IMAGE%/}:${VERSION}"
  docker tag "patchmon-server:${VERSION}" "$TARGET"
  echo "==> Tagged $TARGET"
  if [ "$PUSH" = "true" ]; then
    docker push "$TARGET"
    echo "==> Pushed $TARGET"
  fi
fi
