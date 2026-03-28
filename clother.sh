#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
LOCAL_INSTALLER="$SCRIPT_DIR/scripts/install.sh"

if [[ -f "$LOCAL_INSTALLER" ]]; then
  exec "$LOCAL_INSTALLER" "$@"
fi

BOOTSTRAP_URL="${CLOTHER_BOOTSTRAP_URL:-https://raw.githubusercontent.com/jolehuit/clother/main/scripts/install.sh}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

BOOTSTRAP_PATH="$TMP_DIR/install.sh"
curl -fsSL "$BOOTSTRAP_URL" -o "$BOOTSTRAP_PATH"
chmod +x "$BOOTSTRAP_PATH"
"$BOOTSTRAP_PATH" "$@"
