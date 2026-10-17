#!/usr/bin/env bash
set -euo pipefail

# Signs dist/checksums.txt with the release key and writes dist/checksums.txt.minisig.
#
# The client verifies that signature before it trusts checksums.txt, because a
# checksum served by the same origin as the archive proves nothing. See
# internal/update/keys/README.md for how the key is generated and stored.
#
# Inputs:
#   MINISIGN_SECRET_KEY            content of the passwordless minisign secret key
#   CLOTHER_ALLOW_UNSIGNED_RELEASE set to 1 to publish without a signature
#
# minisign itself is not installed: it is run once through the Go toolchain that
# is already present, pinned to a version, and never added to go.mod.

MINISIGN_MODULE="aead.dev/minisign/cmd/minisign@v0.3.0"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${1:-$ROOT_DIR/dist}"
CHECKSUMS="$DIST_DIR/checksums.txt"
SIGNATURE="$CHECKSUMS.minisig"
PUBLIC_KEY="$ROOT_DIR/internal/update/keys/current.pub"

if [[ ! -f "$CHECKSUMS" ]]; then
  echo "error: $CHECKSUMS not found, run scripts/package-release.sh first" >&2
  exit 1
fi

if [[ -z "${MINISIGN_SECRET_KEY:-}" ]]; then
  echo "warning: MINISIGN_SECRET_KEY is not set" >&2
  echo "warning: this release will be published WITHOUT a signature, clients cannot" >&2
  echo "warning: authenticate it. See internal/update/keys/README.md." >&2
  if [[ "${CLOTHER_ALLOW_UNSIGNED_RELEASE:-0}" != "1" ]]; then
    echo "error: refusing to publish an unsigned release" >&2
    echo "error: set CLOTHER_ALLOW_UNSIGNED_RELEASE=1 to do it on purpose" >&2
    exit 1
  fi
  exit 0
fi

command -v go >/dev/null 2>&1 || {
  echo "error: the Go toolchain is required to run minisign" >&2
  exit 1
}

KEY_DIR="$(mktemp -d)"
trap 'rm -rf "$KEY_DIR"' EXIT
KEY_FILE="$KEY_DIR/clother-release.key"
(umask 077; printf '%s\n' "$MINISIGN_SECRET_KEY" > "$KEY_FILE")

go run "$MINISIGN_MODULE" -S -W \
  -s "$KEY_FILE" \
  -m "$CHECKSUMS" \
  -x "$SIGNATURE" \
  -c "clother release signature" \
  -t "clother checksums, tag ${GITHUB_REF_NAME:-local}"

# Self check: the signature must verify against the public key that is compiled
# into the clients, otherwise the release would be refused by every user.
if grep -qv '^untrusted comment:' "$PUBLIC_KEY" 2>/dev/null; then
  go run "$MINISIGN_MODULE" -V -p "$PUBLIC_KEY" -x "$SIGNATURE" -m "$CHECKSUMS" -q
  echo "signed $CHECKSUMS and verified against $PUBLIC_KEY"
else
  echo "warning: $PUBLIC_KEY is still a placeholder, the signature was NOT checked" >&2
  echo "warning: against the key embedded in clients. Publish the public key first." >&2
fi
