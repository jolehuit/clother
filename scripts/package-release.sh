#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${1:-$ROOT_DIR/dist}"
DEFAULT_VERSION="$(sed -n 's/^var Value = "\(.*\)"$/\1/p' "$ROOT_DIR/internal/version/version.go" | head -1)"
VERSION="${VERSION:-${GITHUB_REF_NAME:-${DEFAULT_VERSION:-dev}}}"

mkdir -p "$DIST_DIR"
rm -f "$DIST_DIR"/clother_*.tar.gz "$DIST_DIR"/checksums.txt "$DIST_DIR"/latest.json

build_target() {
  local os="$1"
  local arch="$2"
  local work="$DIST_DIR/${os}-${arch}"
  local asset="clother_${os}_${arch}.tar.gz"

  rm -rf "$work"
  mkdir -p "$work"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w -X github.com/jolehuit/clother/internal/version.Value=${VERSION}" \
    -o "$work/clother" \
    ./cmd/clother
  tar -C "$work" -czf "$DIST_DIR/$asset" clother
}

build_target darwin amd64
build_target darwin arm64
build_target linux amd64
build_target linux arm64

TAG_VERSION="$VERSION"
[[ "$TAG_VERSION" != v* ]] && TAG_VERSION="v$TAG_VERSION"

if command -v shasum >/dev/null 2>&1; then
  (cd "$DIST_DIR" && shasum -a 256 clother_*.tar.gz > checksums.txt)
else
  (cd "$DIST_DIR" && sha256sum clother_*.tar.gz > checksums.txt)
fi

cat > "$DIST_DIR/latest.json" <<EOF
{
  "version": "$TAG_VERSION",
  "url": "https://github.com/jolehuit/clother/releases/tag/$TAG_VERSION"
}
EOF
