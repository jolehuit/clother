#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
LOCAL_INSTALLER="$SCRIPT_DIR/scripts/install.sh"

if [[ -f "$LOCAL_INSTALLER" ]]; then
  exec "$LOCAL_INSTALLER" "$@"
fi

BOOTSTRAP_URL="${CLOTHER_BOOTSTRAP_URL:-https://raw.githubusercontent.com/jolehuit/clother/main/scripts/install.sh}"

# The bootstrap script is downloaded and then executed, so its origin has to be
# authenticated. https only, plus file:// for a local checkout and a loopback
# origin for testing.
case "$BOOTSTRAP_URL" in
  https://*|file://*) ;;
  http://127.0.0.1|http://127.0.0.1:*|http://127.0.0.1/*) ;;
  http://localhost|http://localhost:*|http://localhost/*) ;;
  'http://[::1]'|'http://[::1]:'*|'http://[::1]/'*) ;;
  *)
    echo "error: CLOTHER_BOOTSTRAP_URL must use https (got: $BOOTSTRAP_URL)" >&2
    exit 1
    ;;
esac

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

BOOTSTRAP_PATH="$TMP_DIR/install.sh"
case "$BOOTSTRAP_URL" in
  https://*)
    curl -fsSL --proto '=https' --proto-redir '=https' --max-redirs 3 "$BOOTSTRAP_URL" -o "$BOOTSTRAP_PATH"
    ;;
  *)
    curl -fsSL --max-redirs 3 "$BOOTSTRAP_URL" -o "$BOOTSTRAP_PATH"
    ;;
esac
chmod +x "$BOOTSTRAP_PATH"
"$BOOTSTRAP_PATH" "$@"
