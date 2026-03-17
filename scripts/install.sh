#!/usr/bin/env bash
set -euo pipefail

if [[ $# -eq 0 ]]; then
  set -- install
fi

REPO="jolehuit/clother"
VERSION="${CLOTHER_VERSION:-latest}"
RELEASE_BASE_URL="${CLOTHER_RELEASE_BASE_URL:-}"
INSTALL_MODE="${CLOTHER_INSTALL_MODE:-auto}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

run_from_source() {
  local source_dir="$1"
  shift
  command -v go >/dev/null 2>&1 || {
    echo "go is required to build clother from source" >&2
    exit 1
  }
  cd "$source_dir"
  exec go run ./cmd/clother "$@"
}

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux)  echo "linux" ;;
    *) echo "unsupported operating system: $(uname -s)" >&2; exit 1 ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
  esac
}

download_url() {
  local asset="$1"
  if [[ -n "$RELEASE_BASE_URL" ]]; then
    echo "${RELEASE_BASE_URL%/}/${asset}"
    return
  fi
  if [[ "$VERSION" == "latest" ]]; then
    echo "https://github.com/${REPO}/releases/latest/download/${asset}"
  else
    echo "https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
  fi
}

verify_checksum() {
  local asset_path="$1"
  local checksums_path="$2"
  local asset_name
  asset_name="$(basename "$asset_path")"
  if command -v shasum >/dev/null 2>&1; then
    grep " ${asset_name}\$" "$checksums_path" | (cd "$(dirname "$asset_path")" && shasum -a 256 -c -)
  elif command -v sha256sum >/dev/null 2>&1; then
    grep " ${asset_name}\$" "$checksums_path" | (cd "$(dirname "$asset_path")" && sha256sum -c -)
  else
    echo "warning: no checksum tool found, skipping verification" >&2
  fi
}

if [[ "$INSTALL_MODE" != "release" && -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
  SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  if [[ -f "$SOURCE_DIR/go.mod" && -d "$SOURCE_DIR/cmd/clother" ]]; then
    run_from_source "$SOURCE_DIR" "$@"
  fi
fi

OS="$(detect_os)"
ARCH="$(detect_arch)"
ASSET="clother_${OS}_${ARCH}.tar.gz"
CHECKSUMS="checksums.txt"
ASSET_PATH="$TMP_DIR/$ASSET"
CHECKSUMS_PATH="$TMP_DIR/$CHECKSUMS"

curl -fsSL "$(download_url "$ASSET")" -o "$ASSET_PATH"
curl -fsSL "$(download_url "$CHECKSUMS")" -o "$CHECKSUMS_PATH"
verify_checksum "$ASSET_PATH" "$CHECKSUMS_PATH"

tar -xzf "$ASSET_PATH" -C "$TMP_DIR"
chmod +x "$TMP_DIR/clother"
exec "$TMP_DIR/clother" "$@"
