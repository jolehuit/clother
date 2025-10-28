#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'
umask 077

VERSION="1.1" # Version updated
BASE="${CLOTHER_HOME:-$HOME/.clother}"
BIN="${CLOTHER_BIN:-$HOME/bin}"
SECRETS="$BASE/secrets.env"

# --- Color Codes ---
RED=$'\033[0;31m'
GREEN=$'\033[0;32m'
YELLOW=$'\033[1;33m'
BLUE=$'\033[0;34m'
BOLD=$'\033[1m'
NC=$'\033[0m'

# --- Helper Functions ---
log() { echo -e "${BLUE}→${NC} $*"; }
success() { echo -e "${GREEN}✓${NC} $*"; }
warn() { echo -e "${YELLOW}!${NC} $*"; }
error() { echo -e "${RED}✗${NC} $*" >&2; }

# --- Shell Detection ---
if [[ "$OSTYPE" == "darwin"* ]]; then
  SHELL_RC="$HOME/.zshrc"
  SHELL_NAME="zsh"
else
  SHELL_RC="$HOME/.bashrc"
  SHELL_NAME="bash"
fi

# --- Installation Start ---
echo -e "${BOLD}Clother ${VERSION}${NC}"
echo
log "Checking for 'claude' command..."
if ! command -v claude &>/dev/null; then
  error "'claude' command not found."
  echo
  echo "Clother requires the 'claude' command-line tool to be installed and in your PATH."
  echo "Please install it first using the official command:"
  echo -e " ${YELLOW}npm install -g @anthropic-ai/claude-code${NC}"
  echo
  exit 1
fi
success "'claude' command found."

# --- Create directories safely ---
mkdir -p "$BASE"/{providers,cache,backup} "$BIN"

# --- Ensure secrets file is safe (no symlink) ---
if [[ -L "$SECRETS" ]]; then
  error "'$SECRETS' is a symlink. Refusing to continue for safety."
  exit 1
fi
touch "$SECRETS"
chmod 600 "$SECRETS"

# --- Main 'clother' command script ---
cat > "$BIN/clother" << 'CLOTHEREOF'
#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'
umask 077

VERSION="1.1"
BASE="${CLOTHER_HOME:-$HOME/.clother}"
BIN="${CLOTHER_BIN:-$HOME/bin}"
SECRETS="$BASE/secrets.env"
CACHE="$BASE/cache"

RED=$'\033[0;31m'; GREEN=$'\033[0;32m'; YELLOW=$'\033[1;33m'
BLUE=$'\033[0;34m'; BOLD=$'\033[1m'; NC=$'\033[0m'
log() { echo -e "${BLUE}→${NC} $*"; }
success() { echo -e "${GREEN}✓${NC} $*"; }
warn() { echo -e "${YELLOW}!${NC} $*"; }
error() { echo -e "${RED}✗${NC} $*" >&2; }

mkdir -p "$CACHE"

# Source secrets if present (shellcheck disable=SC1090)
[ -f "$SECRETS" ] && source "$SECRETS" || true

mask_key() {
  local key="${1:-}"
  [ -z "$key" ] && { echo ""; return; }
  [ ${#key} -le 8 ] && { echo "****"; return; }
  echo "${key:0:4}****${key: -4}"
}

# Safe append of KEY=VALUE into secrets using mktemp and %q escaping
save_kv() {
  local key="$1"; shift
  local value="$1"; shift
  local tmp
  tmp="$(mktemp "${SECRETS}.XXXXXX")"
  # Remove existing lines for this key (if any), preserving others
  if [ -f "$SECRETS" ]; then
    grep -v -E "^${key}=" "$SECRETS" > "$tmp" || true
  fi
  # Append escaped assignment
  printf '%s=%q\n' "$key" "$value" >> "$tmp"
  mv "$tmp" "$SECRETS"
  chmod 600 "$SECRETS"
}

cmd_config() {
  echo -e "${BOLD}Configure a Provider Profile${NC}"
  echo
  echo "Available providers:"
  echo " 1) native   - Anthropic (no key needed)"
  echo " 2) zai      - Z.AI"
  echo " 3) minimax  - MiniMax"
  echo " 4) katcoder - KAT-Coder"
  echo " 5) kimi     - Moonshot AI"
  echo " 6) custom   - Add your own"
  echo
  read -r -p "Choose [1-6]: " choice
  case "$choice" in
    1)
      echo
      success "Native Anthropic profile is ready."
      log "To use it, run: ${GREEN}clother-native${NC} (or just 'claude')."
      ;;
    2)
      echo
      echo "Z.AI Configuration"
      [ -n "${ZAI_API_KEY:-}" ] && echo "Current key: $(mask_key "$ZAI_API_KEY")"
      read -rs -p "API Key: " key; echo
      [ -z "$key" ] && { error "Key is required"; return 1; }
      save_kv "ZAI_API_KEY" "$key"
      success "Z.AI API Key saved."
      log "To use it, run: ${GREEN}clother-zai${NC}"
      ;;
    3)
      echo
      echo "MiniMax Configuration"
      [ -n "${MINIMAX_API_KEY:-}" ] && echo "Current key: $(mask_key "$MINIMAX_API_KEY")"
      read -rs -p "API Key: " key; echo
      [ -z "$key" ] && { error "Key is required"; return 1; }
      save_kv "MINIMAX_API_KEY" "$key"
      success "MiniMax API Key saved. You can now use 'clother-minimax'."
      ;;
    4)
      echo
      echo "KAT-Coder Configuration"
      [ -n "${VC_API_KEY:-}" ] && echo "Current key: $(mask_key "$VC_API_KEY")"
      [ -n "${VC_ENDPOINT_ID:-}" ] && echo "Current endpoint: $VC_ENDPOINT_ID"
      read -rs -p "API Key: " key; echo
      read -r -p "Endpoint ID (e.g., ep-xxx-xxx): " endpoint
      { [ -z "$key" ] || [ -z "$endpoint" ]; } && { error "Both fields are required"; return 1; }
      save_kv "VC_API_KEY" "$key"
      save_kv "VC_ENDPOINT_ID" "$endpoint"
      success "KAT-Coder configured. You can now use 'clother-katcoder'."
      ;;
    5)
      echo
      echo "Moonshot AI (Kimi) Configuration"
      [ -n "${KIMI_API_KEY:-}" ] && echo "Current key: $(mask_key "$KIMI_API_KEY")"
      read -rs -p "API Key: " key; echo
      [ -z "$key" ] && { error "Key is required"; return 1; }
      save_kv "KIMI_API_KEY" "$key"
      success "Kimi API Key saved."
      log "To use it, run: ${GREEN}clother-kimi${NC}"
      ;;
    6)
      echo
      echo "Custom Provider"
      read -r -p "Provider name (e.g., 'my-provider'): " name
      # Strict name validation to avoid path tricks
      if [[ ! "$name" =~ ^[a-z0-9_-]+$ ]]; then
        error "Invalid name. Allowed: lowercase letters, digits, '_' and '-'."
        return 1
      fi
      read -r -p "Base URL: " url
      read -rs -p "API key value: " key; echo
      { [ -z "$name" ] || [ -z "$url" ] || [ -z "$key" ]; } && { error "All fields are required"; return 1; }

      # KEY variable name derived from provider name
      local keyvar
      keyvar=$(echo "$name" | tr '[:lower:]-' '[:upper:]_' | tr -cd '[:alnum:]_')_API_KEY

      # Persist values safely
      save_kv "$keyvar" "$key"
      save_kv "CLOTHER_${keyvar}_BASE_URL" "$url"

      # Generate launcher with *quoted* heredoc to prevent expansion at write-time
      cat > "$BIN/clother-$name" <<'LAUNCHER'
#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

cat << "ART"
  ____ _          _   _
 / ___| | ___ | |_| |__   ___ _ __
| |   | |/ _ \| __| '_ \ / _ \ '__|
| |___| | (_) | |_| | | |  __/ |
 \____|_|\___/ \__|_| |_|\___|_|
ART

SECRETS="${CLOTHER_HOME:-$HOME/.clother}/secrets.env"
[ -f "$SECRETS" ] && source "$SECRETS"

# These placeholders will be substituted at runtime via environment lookups:
# - PROVIDER_KEYVAR: name of the API key variable to indirect-expand
# - PROVIDER_BASEVAR: name of the BASE_URL variable containing the URL

provider="${0##*/}"           # e.g., clother-myprovider
provname="${provider#clother-}"   # e.g., myprovider
upperprov="$(echo "$provname" | tr '[:lower:]-' '[:upper:]_' | tr -cd '[:alnum:]_')"

PROVIDER_KEYVAR="${upperprov}_API_KEY"
PROVIDER_BASEVAR="CLOTHER_${upperprov}_API_KEY_BASE_URL"

# Fallback compatibility: also check CLOTHER_<KEYVAR>_BASE_URL if set by installer
if [ -n "${!PROVIDER_BASEVAR:-}" ]; then
  base_url="${!PROVIDER_BASEVAR}"
else
  altvar="CLOTHER_${upperprov}_BASE_URL"
  base_url="${!altvar:-}"
fi

apikey="${!PROVIDER_KEYVAR:-}"

if [ -z "$apikey" ] || [ -z "$base_url" ]; then
  echo "Missing configuration for provider '$provname'." >&2
  echo "Expected key var: $PROVIDER_KEYVAR and base URL var: $PROVIDER_BASEVAR (or CLOTHER_${upperprov}_BASE_URL)." >&2
  echo "Run: clother config" >&2
  exit 1
fi

export ANTHROPIC_BASE_URL="$base_url"
# Indirect expansion: value of the var whose name is in PROVIDER_KEYVAR
export ANTHROPIC_AUTH_TOKEN="$apikey"

exec claude "$@"
LAUNCHER
      chmod +x "$BIN/clother-$name"
      success "Provider '$name' created. You can now use 'clother-$name'."
      ;;
    *)
      error "Invalid choice"
      return 1
      ;;
  esac
}

cmd_list() {
  echo -e "${BOLD}Available Profiles:${NC}"
  shopt -s nullglob
  for launcher in "$BIN"/clother-*; do
    [ ! -x "$launcher" ] && continue
    local name
    name=$(basename "$launcher" | sed 's/^clother-//')
    echo -e " → ${YELLOW}${name}${NC}"
  done
}

cmd_info() {
  local provider="${1:-}"
  [ -z "$provider" ] && { error "Usage: clother info <provider>"; return 1; }

  [ -f "$SECRETS" ] && source "$SECRETS" || true

  echo -e "${BOLD}Provider: ${YELLOW}$provider${NC}"
  echo "--------------------------"

  case "$provider" in
    native)
      echo "Base URL: (default Anthropic CLI behavior)"
      echo "Models:   (uses 'claude' defaults)"
      ;;
    zai)
      echo "Base URL: https://api.z.ai/api/anthropic"
      echo "Models:"
      echo "  Haiku:   glm-4.5-air"
      echo "  Sonnet:  glm-4.6"
      echo "  Opus:    glm-4.6"
      ;;
    minimax)
      echo "Base URL: https://api.minimax.io/anthropic"
      echo "Models:"
      echo "  Default: MiniMax-M2"
      ;;
    katcoder)
      local endpoint_id="${VC_ENDPOINT_ID:-<not_set>}"
      echo "Base URL: https://vanchin.streamlake.ai/api/gateway/v1/endpoints/$endpoint_id/claude-code-proxy"
      echo "Models:"
      echo "  Default:    KAT-Coder"
      echo "  Small/Fast: KAT-Coder"
      ;;
    kimi)
      echo "Base URL: https://api.moonshot.ai/anthropic"
      echo "Models:"
      echo "  Default/Fast: kimi-k2-turbo-preview"
      echo "  Latest:       kimi-k2-0905-preview"
      echo "  Alternate:    kimi-k2-0711-preview"
      ;;
    *)
      local launcher_file="$BIN/clother-$provider"
      if [ -x "$launcher_file" ]; then
        echo "Type:     Custom"
        # We no longer parse the base URL from the launcher. It's stored in secrets.
        local upperprov
        upperprov=$(echo "$provider" | tr '[:lower:]-' '[:upper:]_' | tr -cd '[:alnum:]_')
        local basevar="CLOTHER_${upperprov}_API_KEY_BASE_URL"
        local altvar="CLOTHER_${upperprov}_BASE_URL"
        local base_url="${!basevar:-${!altvar:-<not_found>}}"
        echo "Base URL: ${base_url}"
        echo "Models:   (not explicitly defined in launcher)"
      else
        error "Provider '$provider' not found."
        return 1
      fi
      ;;
  esac
}

cmd_uninstall() {
  echo "This will remove:"
  echo " - $BASE"
  echo " - all 'clother-*' launchers under $BIN"
  echo
  read -r -p "Type 'delete clother' to confirm: " confirm
  [[ "$confirm" == "delete clother" ]] || { log "Uninstall cancelled."; return; }

  case "$BIN" in
    "$HOME"/*) : ;;
    *)
      error "BIN ($BIN) is not under \$HOME. Aborting for safety."
      return 1
      ;;
  esac

  rm -rf -- "$BASE" "$BIN"/clother-* "$BIN/clother"
  success "Clother has been uninstalled."
}

cmd_help() {
  echo -e "${BOLD}Clother v${VERSION}${NC}
Manage and switch between different provider profiles for the \`claude\` command-line tool.

${YELLOW}Getting Started:${NC}
  1. Configure a provider:
     ${GREEN}clother config${NC}
  2. Use the new profile command:
     ${GREEN}clother-zai${NC}

${BOLD}Commands:${NC}
  config      Configure a new or existing provider profile.
  list        List all available profiles.
  info <name> Show information and models for a provider.
  uninstall   Remove Clother and all related files.
  help, -h    Show this help message."
}

case "${1:-help}" in
  config)     cmd_config ;;
  list)       cmd_list ;;
  info)       cmd_info "${2:-}" ;;
  uninstall)  cmd_uninstall ;;
  help|-h|--help) cmd_help ;;
  *) error "Unknown command: '$1'. Use 'clother help' for commands."; exit 1 ;;
esac
CLOTHEREOF
chmod +x "$BIN/clother"

# --- Provider Launchers ---
cat > "$BIN/clother-native" << 'EOF'
#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'
cat << "ART"
  ____ _          _   _
 / ___| | ___ | |_| |__   ___ _ __
| |   | |/ _ \| __| '_ \ / _ \ '__|
| |___| | (_) | |_| | | |  __/ |
 \____|_|\___/ \__|_| |_|\___|_|
ART
exec claude "$@"
EOF

cat > "$BIN/clother-zai" << 'EOF'
#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'
cat << "ART"
  ____ _          _   _
 / ___| | ___ | |_| |__   ___ _ __
| |   | |/ _ \| __| '_ \ / _ \ '__|
| |___| | (_) | |_| | | |  __/ |
 \____|_|\___/ \__|_| |_|\___|_|
ART
[ -f "$HOME/.clother/secrets.env" ] && source "$HOME/.clother/secrets.env"
if [ -z "${ZAI_API_KEY:-}" ]; then
  RED=$'\033[0;31m'; NC=$'\033[0m'
  echo -e "${RED}✗ Error: Z.AI API key not set. Run 'clother config'.${NC}" >&2
  exit 1
fi
export ANTHROPIC_BASE_URL="https://api.z.ai/api/anthropic"
export ANTHROPIC_AUTH_TOKEN="$ZAI_API_KEY"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="glm-4.5-air"
export ANTHROPIC_DEFAULT_SONNET_MODEL="glm-4.6"
export ANTHROPIC_DEFAULT_OPUS_MODEL="glm-4.6"
exec claude "$@"
EOF

cat > "$BIN/clother-minimax" << 'EOF'
#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'
cat << "ART"
  ____ _          _   _
 / ___| | ___ | |_| |__   ___ _ __
| |   | |/ _ \| __| '_ \ / _ \ '__|
| |___| | (_) | |_| | | |  __/ |
 \____|_|\___/ \__|_| |_|\___|_|
ART
[ -f "$HOME/.clother/secrets.env" ] && source "$HOME/.clother/secrets.env"
if [ -z "${MINIMAX_API_KEY:-}" ]; then
  RED=$'\033[0;31m'; NC=$'\033[0m'
  echo -e "${RED}✗ Error: MiniMax API key not set. Run 'clother config'.${NC}" >&2
  exit 1
fi
export ANTHROPIC_BASE_URL="https://api.minimax.io/anthropic"
export ANTHROPIC_AUTH_TOKEN="$MINIMAX_API_KEY"
export ANTHROPIC_MODEL="MiniMax-M2"
export API_TIMEOUT_MS="3000000"
exec claude "$@"
EOF

cat > "$BIN/clother-katcoder" << 'EOF'
#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'
cat << "ART"
  ____ _          _   _
 / ___| | ___ | |_| |__   ___ _ __
| |   | |/ _ \| __| '_ \ / _ \ '__|
| |___| | (_) | |_| | | |  __/ |
 \____|_|\___/ \__|_| |_|\___|_|
ART
[ -f "$HOME/.clother/secrets.env" ] && source "$HOME/.clother/secrets.env"
if [ -z "${VC_API_KEY:-}" ]; then
  RED=$'\033[0;31m'; NC=$'\033[0m'
  echo -e "${RED}✗ Error: KAT-Coder API key not set. Run 'clother config'.${NC}" >&2
  exit 1
fi
if [ -z "${VC_ENDPOINT_ID:-}" ]; then
  RED=$'\033[0;31m'; NC=$'\033[0m'
  echo -e "${RED}✗ Error: Endpoint ID is missing. Run 'clother config'.${NC}" >&2
  exit 1
fi
export ANTHROPIC_BASE_URL="https://vanchin.streamlake.ai/api/gateway/v1/endpoints/$VC_ENDPOINT_ID/claude-code-proxy"
export ANTHROPIC_AUTH_TOKEN="$VC_API_KEY"
export ANTHROPIC_MODEL="KAT-Coder"
export ANTHROPIC_SMALL_FAST_MODEL="KAT-Coder"
exec claude "$@"
EOF

# --- NOUVEAU LANCEUR POUR KIMI ---
cat > "$BIN/clother-kimi" << 'EOF'
#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'
cat << "ART"
  ____ _          _   _
 / ___| | ___ | |_| |__   ___ _ __
| |   | |/ _ \| __| '_ \ / _ \ '__|
| |___| | (_) | |_| | | |  __/ |
 \____|_|\___/ \__|_| |_|\___|_|
ART
[ -f "$HOME/.clother/secrets.env" ] && source "$HOME/.clother/secrets.env"
if [ -z "${KIMI_API_KEY:-}" ]; then
  RED=$'\033[0;31m'; NC=$'\033[0m'
  echo -e "${RED}✗ Error: Kimi (Moonshot AI) API key not set. Run 'clother config'.${NC}" >&2
  exit 1
fi
export ANTHROPIC_BASE_URL="https://api.moonshot.ai/anthropic"
export ANTHROPIC_AUTH_TOKEN="$KIMI_API_KEY"
export ANTHROPIC_MODEL="kimi-k2-turbo-preview"
export ANTHROPIC_SMALL_FAST_MODEL="kimi-k2-turbo-preview"
exec claude "$@"
EOF


chmod +x "$BIN"/clother-*

# --- Final Instructions ---
success "Installed Clother to '$BIN/clother'."
if [[ ":$PATH:" != *":$BIN:"* ]]; then
  echo
  warn "ACTION REQUIRED: Add '$BIN' to your PATH."
  echo "To use the 'clother' and 'clother-*' commands, run the following:"
  echo
  echo -e " ${YELLOW}echo 'export PATH=\"$BIN:\$PATH\"' >> $SHELL_RC${NC}"
  echo -e " ${YELLOW}source $SHELL_RC${NC}"
  echo
  echo "You may need to restart your terminal for the changes to take effect."
fi

echo
echo -e "${BOLD}What's next?${NC}"
echo " 1. Configure a provider by running:"
echo -e " ${GREEN}clother config${NC}"
echo
echo " 2. Use a configured profile:"
echo -e " ${GREEN}clother-<provider_name>${NC}"
echo
echo " For all commands, run:"
echo -e " ${GREEN}clother help${NC}"
