#!/usr/bin/env bash
set -euo pipefail

# The whole body lives in main(), called on the very last line, so that a
# truncated download of this script (curl ... | bash) can never execute a partial
# installation: bash only ever runs a definition it has fully read.

REPO="jolehuit/clother"

log_error() {
  echo "error: $*" >&2
}

# require_secure_url refuses anything but https, with the usual exception for a
# loopback origin so that a local mirror stays usable for testing.
require_secure_url() {
  local url="$1"
  local label="$2"
  local host

  case "$url" in
    https://*)
      return 0
      ;;
    http://*)
      host="${url#http://}"
      host="${host%%/*}"
      case "$host" in
        \[::1\]|\[::1\]:*)
          return 0
          ;;
      esac
      host="${host%%:*}"
      case "$host" in
        127.0.0.1|localhost)
          return 0
          ;;
      esac
      log_error "$label must use https (got: $url)"
      return 1
      ;;
    *)
      log_error "$label must use https (got: $url)"
      return 1
      ;;
  esac
}

# fetch downloads a URL. An https origin is pinned to https, redirects included,
# so a redirect cannot silently downgrade the transfer to cleartext.
fetch() {
  local url="$1"
  local dest="$2"

  case "$url" in
    https://*)
      curl -fsSL --proto '=https' --proto-redir '=https' --max-redirs 3 "$url" -o "$dest"
      ;;
    *)
      curl -fsSL --proto '=http,https' --max-redirs 3 "$url" -o "$dest"
      ;;
  esac
}

run_from_source() {
  local source_dir="$1"
  shift
  command -v go >/dev/null 2>&1 || {
    log_error "go is required to build clother from source"
    exit 1
  }
  cd "$source_dir"
  exec go run ./cmd/clother "$@"
}

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux)  echo "linux" ;;
    *) log_error "unsupported operating system: $(uname -s)"; exit 1 ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) log_error "unsupported architecture: $(uname -m)"; exit 1 ;;
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

# compute_sha256 prints the lowercase hex digest of a file, or fails. There is no
# "continue without checking" branch: an environment with no SHA-256 tool at all
# aborts the installation instead of executing an unverified archive.
compute_sha256() {
  local target="$1"
  local out

  if command -v shasum >/dev/null 2>&1; then
    out="$(shasum -a 256 "$target")"
    out="${out%% *}"
  elif command -v sha256sum >/dev/null 2>&1; then
    out="$(sha256sum "$target")"
    out="${out%% *}"
  elif command -v openssl >/dev/null 2>&1; then
    # "SHA2-256(file)= <digest>" or "SHA256(file)= <digest>"
    out="$(openssl dgst -sha256 "$target")"
    out="${out##*= }"
  else
    log_error "no SHA-256 tool available (need shasum, sha256sum or openssl); refusing to install"
    return 1
  fi

  out="$(printf '%s' "$out" | tr '[:upper:]' '[:lower:]')"
  case "$out" in
    [0-9a-f][0-9a-f]*)
      printf '%s\n' "$out"
      ;;
    *)
      log_error "could not compute the SHA-256 of $target"
      return 1
      ;;
  esac
}

# expected_checksum extracts the digest recorded for an exact asset name. The
# previous `grep " ${asset}$"` treated the name as a regular expression, so the
# dots in clother_darwin_arm64.tar.gz matched any character.
expected_checksum() {
  local checksums_path="$1"
  local asset_name="$2"
  awk -v name="$asset_name" '$NF == name { print $1; exit }' "$checksums_path"
}

verify_checksum() {
  local asset_path="$1"
  local checksums_path="$2"
  local asset_name expected actual

  asset_name="$(basename "$asset_path")"
  expected="$(expected_checksum "$checksums_path" "$asset_name")"
  if [[ -z "$expected" ]]; then
    log_error "no checksum entry for ${asset_name}; refusing to install"
    return 1
  fi
  expected="$(printf '%s' "$expected" | tr '[:upper:]' '[:lower:]')"

  actual="$(compute_sha256 "$asset_path")" || return 1
  if [[ "$actual" != "$expected" ]]; then
    log_error "checksum mismatch for ${asset_name}: expected ${expected}, got ${actual}"
    return 1
  fi
}

# report_signature_state says the same thing the Go updater says. This script
# cannot verify an ed25519 signature in portable shell, but staying silent made
# the two channels tell different stories: `clother install` warns that a
# release publishes no signature, while curl|bash said nothing at all.
report_signature_state() {
  local dest="$1"
  local url
  url="$(download_url "checksums.txt.minisig")"

  if fetch "$url" "$dest" 2>/dev/null; then
    echo "note: this release publishes a signature (${url}); this installer checks the checksum only — run \`clother update\` afterwards to get a signature-verified binary" >&2
    return 0
  fi
  echo "warning: this release publishes no signature (${url}); falling back to a checksum served by the same origin, which does not prove authenticity" >&2
}

main() {
  if [[ $# -eq 0 ]]; then
    set -- install
  fi

  VERSION="${CLOTHER_VERSION:-latest}"
  RELEASE_BASE_URL="${CLOTHER_RELEASE_BASE_URL:-}"
  local install_mode="${CLOTHER_INSTALL_MODE:-auto}"

  if [[ -n "$RELEASE_BASE_URL" ]]; then
    require_secure_url "$RELEASE_BASE_URL" "CLOTHER_RELEASE_BASE_URL" || exit 1
  fi

  if [[ "$install_mode" != "release" && -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
    local source_dir
    source_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    if [[ -f "$source_dir/go.mod" && -d "$source_dir/cmd/clother" ]]; then
      run_from_source "$source_dir" "$@"
    fi
  fi

  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "$TMP_DIR"' EXIT

  local os arch asset asset_path checksums_path
  os="$(detect_os)"
  arch="$(detect_arch)"
  asset="clother_${os}_${arch}.tar.gz"
  asset_path="$TMP_DIR/$asset"
  checksums_path="$TMP_DIR/checksums.txt"

  fetch "$(download_url "$asset")" "$asset_path"
  fetch "$(download_url "checksums.txt")" "$checksums_path"
  verify_checksum "$asset_path" "$checksums_path"
  report_signature_state "$TMP_DIR/checksums.txt.minisig"

  tar -xzf "$asset_path" -C "$TMP_DIR"
  if [[ ! -f "$TMP_DIR/clother" ]]; then
    log_error "the archive does not contain a clother binary"
    exit 1
  fi
  chmod +x "$TMP_DIR/clother"

  # Not `exec`: the EXIT trap that cleans up TMP_DIR never fires when the shell
  # is replaced, so every installation left the archive and the extracted binary
  # behind. Move the binary out of the doomed directory, run it, propagate its
  # status.
  local runner="${TMP_DIR}.clother"
  mv "$TMP_DIR/clother" "$runner"
  trap 'rm -rf "$TMP_DIR" "$runner"' EXIT
  "$runner" "$@"
  exit $?
}

main "$@"
