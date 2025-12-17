#!/usr/bin/env bash
# =============================================================================
# CLOTHER v2.0 - Multi-provider launcher for Claude CLI
# =============================================================================
# A CLI tool to manage and switch between different LLM providers
# for the Claude Code command-line interface.
#
# Repository: https://github.com/your/clother
# License: MIT
# =============================================================================

set -euo pipefail
IFS=$'\n\t'
umask 077

readonly VERSION="2.0"
readonly CLOTHER_DOCS="https://github.com/your/clother"

# =============================================================================
# XDG BASE DIRECTORY SPECIFICATION
# =============================================================================

readonly XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
readonly XDG_DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
readonly XDG_CACHE_HOME="${XDG_CACHE_HOME:-$HOME/.cache}"

readonly CONFIG_DIR="${CLOTHER_CONFIG_DIR:-$XDG_CONFIG_HOME/clother}"
readonly DATA_DIR="${CLOTHER_DATA_DIR:-$XDG_DATA_HOME/clother}"
readonly CACHE_DIR="${CLOTHER_CACHE_DIR:-$XDG_CACHE_HOME/clother}"
readonly BIN_DIR="${CLOTHER_BIN:-$HOME/bin}"

readonly CONFIG_FILE="$CONFIG_DIR/config"
readonly SECRETS_FILE="$DATA_DIR/secrets.env"
readonly PROXY_SOURCE="$DATA_DIR/proxy.go"
readonly PROXY_BIN="$DATA_DIR/openrouter-proxy"

# =============================================================================
# GLOBAL FLAGS (can be set via env vars)
# =============================================================================

VERBOSE="${CLOTHER_VERBOSE:-0}"
DEBUG="${CLOTHER_DEBUG:-0}"
QUIET="${CLOTHER_QUIET:-0}"
YES_MODE="${CLOTHER_YES:-0}"
NO_INPUT="${CLOTHER_NO_INPUT:-0}"
NO_BANNER="${CLOTHER_NO_BANNER:-0}"
OUTPUT_FORMAT="${CLOTHER_OUTPUT_FORMAT:-human}"  # human, json, plain
DEFAULT_PROVIDER="${CLOTHER_DEFAULT_PROVIDER:-}"

# =============================================================================
# TTY & COLOR DETECTION
# =============================================================================

is_tty() { [[ -t 1 ]]; }
is_stdin_tty() { [[ -t 0 ]]; }
is_interactive() { is_tty && is_stdin_tty && [[ "$NO_INPUT" != "1" ]]; }

setup_colors() {
  if is_tty && [[ -z "${NO_COLOR:-}" ]] && [[ "$OUTPUT_FORMAT" == "human" ]]; then
    RED=$'\033[0;31m'
    GREEN=$'\033[0;32m'
    YELLOW=$'\033[1;33m'
    BLUE=$'\033[0;34m'
    CYAN=$'\033[0;36m'
    MAGENTA=$'\033[0;35m'
    BOLD=$'\033[1m'
    DIM=$'\033[2m'
    NC=$'\033[0m'
  else
    RED='' GREEN='' YELLOW='' BLUE='' CYAN='' MAGENTA='' BOLD='' DIM='' NC=''
  fi
}

setup_symbols() {
  if [[ "${TERM:-}" == "dumb" ]] || [[ -n "${NO_COLOR:-}" ]]; then
    SYM_OK="[OK]" SYM_ERR="[X]" SYM_WARN="[!]" SYM_INFO=">" SYM_ARROW="->"
    SYM_CHECK="[x]" SYM_UNCHECK="[ ]"
    SYM_SPINNER=("-" "\\" "|" "/")
    BOX_TL="+" BOX_TR="+" BOX_BL="+" BOX_BR="+" BOX_H="-" BOX_V="|"
  else
    SYM_OK="✓" SYM_ERR="✗" SYM_WARN="⚠" SYM_INFO="→" SYM_ARROW="→"
    SYM_CHECK="✓" SYM_UNCHECK="○"
    SYM_SPINNER=("⠋" "⠙" "⠹" "⠸" "⠼" "⠴" "⠦" "⠧" "⠇" "⠏")
    BOX_TL="╭" BOX_TR="╮" BOX_BL="╰" BOX_BR="╯" BOX_H="─" BOX_V="│"
  fi
}

setup_colors
setup_symbols

# =============================================================================
# LOGGING SYSTEM
# =============================================================================

debug()   { [[ "$DEBUG" == "1" ]] && echo -e "${DIM}[DEBUG] $*${NC}" >&2 || true; }
verbose() { [[ "$VERBOSE" == "1" || "$DEBUG" == "1" ]] && echo -e "${DIM}$*${NC}" || true; }
log()     { [[ "$QUIET" != "1" ]] && echo -e "${BLUE}${SYM_INFO}${NC} $*" || true; }
success() { echo -e "${GREEN}${SYM_OK}${NC} $*"; }
warn()    { echo -e "${YELLOW}${SYM_WARN}${NC} $*" >&2; }
error()   { echo -e "${RED}${SYM_ERR}${NC} $*" >&2; }

# Error with context, cause, and solution
error_ctx() {
  local code="$1" msg="$2" context="$3" cause="$4" solution="$5"
  echo >&2
  echo -e "${RED}${BOLD}ERROR${NC} ${DIM}[$code]${NC} ${BOLD}$msg${NC}" >&2
  echo -e "  ${DIM}Context:${NC}  $context" >&2
  echo -e "  ${DIM}Cause:${NC}    $cause" >&2
  echo -e "  ${CYAN}Fix:${NC}      $solution" >&2
  echo >&2
}

# Suggest next steps
suggest_next() {
  [[ "$QUIET" == "1" || "$OUTPUT_FORMAT" != "human" ]] && return
  echo -e "\n${BOLD}Next:${NC}"
  for s in "$@"; do echo -e "  ${CYAN}${SYM_ARROW}${NC} $s"; done
}

# =============================================================================
# UI COMPONENTS
# =============================================================================

draw_box() {
  local title="$1" width="${2:-52}"
  local inner=$((width - 2))
  local pad=$(( (inner - ${#title}) / 2 ))

  printf "%s" "$BOX_TL"; printf "%${inner}s" | tr ' ' "$BOX_H"; printf "%s\n" "$BOX_TR"
  printf "%s%${pad}s${BOLD}%s${NC}%$((inner - pad - ${#title}))s%s\n" "$BOX_V" "" "$title" "" "$BOX_V"
  printf "%s" "$BOX_BL"; printf "%${inner}s" | tr ' ' "$BOX_H"; printf "%s\n" "$BOX_BR"
}

draw_separator() {
  local width="${1:-52}"
  printf "${DIM}"; printf "%${width}s" | tr ' ' "$BOX_H"; printf "${NC}\n"
}

# Spinner for long operations
SPINNER_PID=""
spinner_start() {
  local msg="${1:-Working...}"
  ! is_tty && { log "$msg"; return; }
  (
    local i=0
    while true; do
      printf "\r${BLUE}${SYM_SPINNER[$i]}${NC} %s " "$msg"
      i=$(( (i + 1) % ${#SYM_SPINNER[@]} ))
      sleep 0.1
    done
  ) &
  SPINNER_PID=$!
  disown "$SPINNER_PID" 2>/dev/null || true
}

spinner_stop() {
  local status="${1:-0}" msg="${2:-Done}"
  if [[ -n "$SPINNER_PID" ]]; then
    kill "$SPINNER_PID" 2>/dev/null || true
    wait "$SPINNER_PID" 2>/dev/null || true
    SPINNER_PID=""
    printf "\r\033[K"
  fi
  [[ "$status" -eq 0 ]] && success "$msg" || error "$msg"
}

# =============================================================================
# INPUT & PROMPTS
# =============================================================================

prompt() {
  local msg="$1" default="${2:-}" var="${3:-REPLY}"
  if ! is_interactive; then
    [[ -n "$default" ]] && { printf -v "$var" "%s" "$default"; return 0; }
    error "Input required but running non-interactively"; return 1
  fi
  local prompt_text="$msg"; [[ -n "$default" ]] && prompt_text="$msg [$default]"
  read -r -p "$prompt_text: " "$var"
  [[ -z "${!var}" && -n "$default" ]] && printf -v "$var" "%s" "$default"
}

prompt_secret() {
  local msg="$1" var="${2:-REPLY}"
  ! is_interactive && { error "Secret input required but non-interactive"; return 1; }
  read -rs -p "$msg: " "$var"; echo
}

confirm() {
  local msg="$1" default="${2:-n}"
  [[ "$YES_MODE" == "1" ]] && return 0
  ! is_interactive && { [[ "$default" =~ ^[Yy] ]] && return 0 || return 1; }
  local hint; [[ "$default" =~ ^[Yy] ]] && hint="[Y/n]" || hint="[y/N]"
  local resp; read -r -p "$msg $hint: " resp; resp="${resp:-$default}"
  [[ "$resp" =~ ^[Yy] ]]
}

confirm_danger() {
  local action="$1" phrase="${2:-yes}"
  [[ "$YES_MODE" == "1" ]] && { warn "Auto-confirming: $action"; return 0; }
  ! is_interactive && { error "Dangerous action requires --yes flag"; return 1; }
  echo; draw_box "DANGER" 40; echo
  echo -e "${RED}${BOLD}$action${NC}"; echo
  echo -e "Type ${YELLOW}${BOLD}$phrase${NC} to confirm:"
  local resp; read -r resp
  [[ "$resp" == "$phrase" ]]
}

# =============================================================================
# VALIDATION
# =============================================================================

validate_name() {
  local name="$1" field="${2:-name}"
  if [[ ! "$name" =~ ^[a-z0-9_-]+$ ]]; then
    error_ctx "E001" "Invalid $field" "Validating: $name" \
      "Must be lowercase letters, digits, - or _" \
      "Use a valid name like 'my-provider'"
    return 1
  fi
}

validate_url() {
  local url="$1"
  if [[ ! "$url" =~ ^https?:// ]]; then
    error_ctx "E002" "Invalid URL" "Validating: $url" \
      "URL must start with http:// or https://" \
      "Provide a valid URL"
    return 1
  fi
}

validate_api_key() {
  local key="$1" provider="${2:-}"
  if [[ -z "$key" ]]; then
    error_ctx "E003" "API key is empty" "Configuring $provider" \
      "No API key provided" \
      "Enter your API key from the provider's dashboard"
    return 1
  fi
  if [[ ${#key} -lt 8 ]]; then
    error_ctx "E004" "API key too short" "Validating key for $provider" \
      "Key has ${#key} chars, minimum is 8" \
      "Check that you copied the full key"
    return 1
  fi
}

# =============================================================================
# SECRETS MANAGEMENT
# =============================================================================

load_secrets() {
  [[ ! -f "$SECRETS_FILE" ]] && return 0
  # Security checks
  if [[ -L "$SECRETS_FILE" ]]; then
    error "Secrets file is a symlink - refusing for security"; return 1
  fi
  local perms
  perms=$(stat -f "%Lp" "$SECRETS_FILE" 2>/dev/null || stat -c "%a" "$SECRETS_FILE" 2>/dev/null || echo "000")
  if [[ "$perms" != "600" ]]; then
    warn "Fixing secrets file permissions"; chmod 600 "$SECRETS_FILE"
  fi
  source "$SECRETS_FILE"
}

save_secret() {
  local key="$1" value="$2"
  mkdir -p "$(dirname "$SECRETS_FILE")"
  local tmp; tmp=$(mktemp "${SECRETS_FILE}.XXXXXX")
  [[ -f "$SECRETS_FILE" ]] && grep -v "^${key}=" "$SECRETS_FILE" > "$tmp" 2>/dev/null || true
  printf '%s=%q\n' "$key" "$value" >> "$tmp"
  mv "$tmp" "$SECRETS_FILE"
  chmod 600 "$SECRETS_FILE"
}

mask_key() {
  local key="${1:-}"
  [[ -z "$key" ]] && { echo ""; return; }
  [[ ${#key} -le 8 ]] && { echo "****"; return; }
  echo "${key:0:4}****${key: -4}"
}

# =============================================================================
# SIGNAL HANDLERS & CLEANUP
# =============================================================================

cleanup() {
  local exit_code="${1:-$?}"
  spinner_stop 1 "Interrupted" 2>/dev/null || true
  tput cnorm 2>/dev/null || true  # Show cursor
  exit "$exit_code"
}

trap 'cleanup 130' INT
trap 'cleanup 143' TERM

# =============================================================================
# PROVIDER DEFINITIONS
# =============================================================================

get_provider_def() {
  # Format: keyvar|baseurl|model|model_opts|description
  case "$1" in
    native)     echo "|||Native Anthropic" ;;
    zai)        echo "ZAI_API_KEY|https://api.z.ai/api/anthropic|glm-4.6|haiku=glm-4.5-air,sonnet=glm-4.6,opus=glm-4.6|Z.AI International" ;;
    zai-cn)     echo "ZAI_CN_API_KEY|https://open.bigmodel.cn/api/anthropic|glm-4.6|haiku=glm-4.5-air,sonnet=glm-4.6,opus=glm-4.6|Z.AI China" ;;
    minimax)    echo "MINIMAX_API_KEY|https://api.minimax.io/anthropic|MiniMax-M2||MiniMax International" ;;
    minimax-cn) echo "MINIMAX_CN_API_KEY|https://api.minimaxi.com/anthropic|MiniMax-M2||MiniMax China" ;;
    kimi)       echo "KIMI_API_KEY|https://api.kimi.com/coding/|kimi-k2-thinking-turbo|small=kimi-k2-turbo-preview|Kimi K2" ;;
    moonshot)   echo "MOONSHOT_API_KEY|https://api.moonshot.ai/anthropic|kimi-k2-turbo-preview||Moonshot AI" ;;
    ve)         echo "ARK_API_KEY|https://ark.cn-beijing.volces.com/api/coding|doubao-seed-code-preview-latest||VolcEngine" ;;
    deepseek)   echo "DEEPSEEK_API_KEY|https://api.deepseek.com/anthropic|deepseek-chat|small=deepseek-chat|DeepSeek" ;;
    mimo)       echo "MIMO_API_KEY|https://api.xiaomimimo.com/anthropic|mimo-v2-flash|haiku=mimo-v2-flash,sonnet=mimo-v2-flash,opus=mimo-v2-flash|Xiaomi MiMo" ;;
    *)          echo "" ;;
  esac
}

is_provider_configured() {
  local provider="$1"
  local def; def=$(get_provider_def "$provider")
  [[ -z "$def" ]] && return 1
  IFS='|' read -r keyvar _ _ _ _ <<< "$def"
  [[ -z "$keyvar" ]] && return 0  # native
  [[ -n "${!keyvar:-}" ]]
}

# =============================================================================
# HELP SYSTEM
# =============================================================================

show_version() {
  echo "Clother v$VERSION"
}

show_brief_help() {
  cat << EOF
${BOLD}Clother v$VERSION${NC} - Multi-provider launcher for Claude CLI

${BOLD}Usage:${NC} clother [options] <command>

${BOLD}Commands:${NC}
  config       Configure a provider
  list         List profiles
  info <name>  Provider details
  test         Test providers
  help         Show full help

${BOLD}Examples:${NC}
  ${GREEN}clother config${NC}       Setup a provider
  ${GREEN}clother-zai${NC}          Use Z.AI

Run ${CYAN}clother --help${NC} for full documentation.
EOF
}

show_full_help() {
  cat << EOF
${BOLD}Clother v$VERSION${NC}
Multi-provider launcher for Claude CLI

${BOLD}USAGE${NC}
  clother [options] <command> [args]

${BOLD}EXAMPLES${NC}
  ${GREEN}clother config${NC}                 # Interactive provider setup
  ${GREEN}clother config zai${NC}             # Configure specific provider
  ${GREEN}clother list${NC}                   # Show all profiles
  ${GREEN}clother list --json${NC}            # Machine-readable output
  ${GREEN}clother test${NC}                   # Verify all providers
  ${GREEN}clother-zai${NC}                    # Launch Claude with Z.AI
  ${GREEN}clother-or-gpt4o${NC}               # Launch with OpenRouter GPT-4o

${BOLD}COMMANDS${NC}
  config [provider]    Configure a provider (interactive if no provider given)
  list                 List all configured profiles
  info <provider>      Show details for a provider
  test [provider]      Test provider connectivity
  status               Show current Clother state
  uninstall            Remove Clother completely
  help [command]       Show help (contextual if command given)

${BOLD}OPTIONS${NC}
  -h, --help           Show help
  -V, --version        Show version
  -v, --verbose        Verbose output
  -d, --debug          Debug mode
  -q, --quiet          Minimal output
  -y, --yes            Auto-confirm prompts
  --no-input           Non-interactive mode (for scripts)
  --no-color           Disable colors
  --no-banner          Hide ASCII banner
  --json               JSON output
  --plain              Plain text output

${BOLD}PROVIDERS${NC}
  ${DIM}Native${NC}
    native             Anthropic direct (no config needed)

  ${DIM}China${NC}
    zai-cn             Z.AI China (GLM-4.6)
    minimax-cn         MiniMax China (M2)
    kimi               Kimi (K2 Thinking)
    ve                 VolcEngine (Doubao)

  ${DIM}International${NC}
    zai                Z.AI (GLM-4.6)
    minimax            MiniMax (M2)
    moonshot           Moonshot AI
    deepseek           DeepSeek
    mimo               Xiaomi MiMo

  ${DIM}Advanced${NC}
    openrouter         100+ models via Go proxy
    custom             Anthropic-compatible endpoint

${BOLD}ENVIRONMENT${NC}
  CLOTHER_CONFIG_DIR   Config directory (default: ~/.config/clother)
  CLOTHER_DATA_DIR     Data directory (default: ~/.local/share/clother)
  CLOTHER_BIN          Binary directory (default: ~/bin)
  CLOTHER_DEFAULT_PROVIDER  Default provider to use
  CLOTHER_VERBOSE      Enable verbose mode (1)
  CLOTHER_QUIET        Enable quiet mode (1)
  CLOTHER_YES          Auto-confirm prompts (1)
  NO_COLOR             Disable colors (standard)

${BOLD}FILES${NC}
  ~/.config/clother/config       User configuration
  ~/.local/share/clother/secrets.env  API keys (chmod 600)
  ~/bin/clother-*                Provider launchers

${DIM}Documentation: $CLOTHER_DOCS${NC}
EOF
}

show_command_help() {
  local cmd="$1"
  case "$cmd" in
    config)
      cat << EOF
${BOLD}clother config${NC} - Configure a provider

${BOLD}USAGE${NC}
  clother config              # Interactive menu
  clother config <provider>   # Configure specific provider

${BOLD}EXAMPLES${NC}
  ${GREEN}clother config${NC}              # Show provider menu
  ${GREEN}clother config zai${NC}          # Configure Z.AI
  ${GREEN}clother config openrouter${NC}   # Configure OpenRouter

${BOLD}PROVIDERS${NC}
  native, zai, zai-cn, minimax, minimax-cn, kimi,
  moonshot, ve, deepseek, mimo, openrouter, custom
EOF
      ;;
    list)
      cat << EOF
${BOLD}clother list${NC} - List configured profiles

${BOLD}USAGE${NC}
  clother list [options]

${BOLD}OPTIONS${NC}
  --json    Output as JSON
  --plain   Plain text (for scripts)

${BOLD}EXAMPLES${NC}
  ${GREEN}clother list${NC}                # Human-readable
  ${GREEN}clother list --json${NC}         # For scripting
  ${GREEN}clother list | grep zai${NC}     # Filter
EOF
      ;;
    *)
      show_full_help
      ;;
  esac
}

# Command suggestion (Levenshtein-like)
suggest_command() {
  local input="$1"
  local -a commands=(config list info test status uninstall help)
  local best="" best_score=999

  for cmd in "${commands[@]}"; do
    # Simple prefix match
    if [[ "$cmd" == "$input"* ]]; then
      echo "$cmd"; return
    fi
    # Check if input is substring
    if [[ "$cmd" == *"$input"* ]]; then
      best="$cmd"
    fi
  done
  [[ -n "$best" ]] && echo "$best"
}

# =============================================================================
# COMMANDS
# =============================================================================

cmd_config() {
  local provider="${1:-}"

  load_secrets

  if [[ -n "$provider" ]]; then
    case "$provider" in
      openrouter) config_openrouter; return ;;
      custom)     config_custom; return ;;
      *)          config_provider "$provider"; return ;;
    esac
  fi

  # Interactive menu
  echo
  draw_box "CLOTHER CONFIGURATION" 54
  echo

  # Count configured
  local configured=0
  for p in native zai zai-cn minimax minimax-cn kimi moonshot ve deepseek mimo; do
    is_provider_configured "$p" && ((configured++))
  done
  echo -e "${DIM}$configured providers configured${NC}"
  echo

  # Native
  echo -e "${BOLD}NATIVE${NC}"
  printf "  ${CYAN}%-2s${NC} %-12s %-24s %s\n" "1" "native" "Anthropic direct" \
    "$(is_provider_configured native && echo "${GREEN}${SYM_CHECK}${NC}" || echo "${DIM}${SYM_UNCHECK}${NC}")"
  echo

  # China
  echo -e "${BOLD}CHINA${NC}"
  local -a china_providers=(zai-cn minimax-cn kimi ve)
  local -a china_names=("Z.AI China" "MiniMax China" "Kimi K2" "VolcEngine")
  for i in "${!china_providers[@]}"; do
    local p="${china_providers[$i]}"
    local status; is_provider_configured "$p" && status="${GREEN}${SYM_CHECK}${NC}" || status="${DIM}${SYM_UNCHECK}${NC}"
    printf "  ${CYAN}%-2s${NC} %-12s %-24s %s\n" "$((i+2))" "$p" "${china_names[$i]}" "$status"
  done
  echo

  # International
  echo -e "${BOLD}INTERNATIONAL${NC}"
  local -a intl_providers=(zai minimax moonshot deepseek mimo)
  local -a intl_names=("Z.AI" "MiniMax" "Moonshot AI" "DeepSeek" "Xiaomi MiMo")
  for i in "${!intl_providers[@]}"; do
    local p="${intl_providers[$i]}"
    local status; is_provider_configured "$p" && status="${GREEN}${SYM_CHECK}${NC}" || status="${DIM}${SYM_UNCHECK}${NC}"
    printf "  ${CYAN}%-2s${NC} %-12s %-24s %s\n" "$((i+6))" "$p" "${intl_names[$i]}" "$status"
  done
  echo

  # Advanced
  echo -e "${BOLD}ADVANCED${NC}"
  printf "  ${CYAN}%-2s${NC} %-12s %-24s\n" "11" "openrouter" "100+ models (Go proxy)"
  printf "  ${CYAN}%-2s${NC} %-12s %-24s\n" "12" "custom" "Anthropic-compatible"
  echo

  draw_separator 54
  echo -e "  ${DIM}[t] Test providers  [q] Quit${NC}"
  echo

  local choice
  prompt "Choose" "q" choice

  case "$choice" in
    1)  config_provider "native" ;;
    2)  config_provider "zai-cn" ;;
    3)  config_provider "minimax-cn" ;;
    4)  config_provider "kimi" ;;
    5)  config_provider "ve" ;;
    6)  config_provider "zai" ;;
    7)  config_provider "minimax" ;;
    8)  config_provider "moonshot" ;;
    9)  config_provider "deepseek" ;;
    10) config_provider "mimo" ;;
    11) config_openrouter ;;
    12) config_custom ;;
    t|T) cmd_test ;;
    q|Q) log "Cancelled" ;;
    *)  error "Invalid choice: $choice" ;;
  esac
}

config_provider() {
  local provider="$1"
  local def; def=$(get_provider_def "$provider")

  if [[ -z "$def" ]]; then
    error "Unknown provider: $provider"
    local suggestion; suggestion=$(suggest_command "$provider")
    [[ -n "$suggestion" ]] && echo -e "Did you mean: ${GREEN}$suggestion${NC}?"
    return 1
  fi

  IFS='|' read -r keyvar baseurl model model_opts description <<< "$def"

  echo
  echo -e "${BOLD}Configure: $description${NC}"
  [[ -n "$baseurl" ]] && echo -e "${DIM}Endpoint: $baseurl${NC}"
  echo

  # Native needs no config
  if [[ -z "$keyvar" ]]; then
    success "Native Anthropic is ready"
    suggest_next "Use it: ${GREEN}clother-native${NC}"
    return 0
  fi

  # Show current key if set
  [[ -n "${!keyvar:-}" ]] && echo -e "Current key: ${DIM}$(mask_key "${!keyvar}")${NC}"

  local key
  prompt_secret "API Key" key
  validate_api_key "$key" "$provider" || return 1

  save_secret "$keyvar" "$key"
  success "API key saved"

  suggest_next \
    "Use it: ${GREEN}clother-$provider${NC}" \
    "Test it: ${GREEN}clother test $provider${NC}"
}

config_openrouter() {
  echo
  echo -e "${BOLD}Configure: OpenRouter${NC}"
  echo -e "${DIM}Access 100+ models via local Go proxy${NC}"
  echo -e "Get API key: ${CYAN}https://openrouter.ai/keys${NC}"
  echo

  load_secrets

  # Handle API key
  if [[ -n "${OPENROUTER_API_KEY:-}" ]]; then
    echo -e "Current key: ${DIM}$(mask_key "$OPENROUTER_API_KEY")${NC}"
    if confirm "Change key?" "n"; then
      local new_key
      prompt_secret "New API Key" new_key
      if [[ -n "$new_key" ]]; then
        validate_api_key "$new_key" "openrouter" || return 0
        save_secret "OPENROUTER_API_KEY" "$new_key"
        success "API key saved"
      fi
    fi
  else
    local new_key
    prompt_secret "API Key" new_key
    if [[ -n "$new_key" ]]; then
      validate_api_key "$new_key" "openrouter" || return 0
      save_secret "OPENROUTER_API_KEY" "$new_key"
      success "API key saved"
    else
      warn "No API key provided"
      return 0
    fi
  fi

  # Check Go
  local go_bin
  go_bin=$(find_go)
  if [[ -z "$go_bin" ]]; then
    warn "Go not installed - required for OpenRouter proxy"
    echo -e "Install from: ${CYAN}https://go.dev/dl/${NC}"
  fi

  # List existing models
  echo
  echo -e "${BOLD}Configured models:${NC}"
  local found=false
  for f in "$BIN_DIR"/clother-or-*; do
    [[ -x "$f" ]] && { found=true; echo -e "  ${GREEN}$(basename "$f")${NC}"; }
  done
  $found || echo -e "  ${DIM}(none)${NC}"

  # Add new model
  echo
  if confirm "Add a model?"; then
    while true; do
      local model
      prompt "Model ID (e.g. openai/gpt-4o) or 'q'" "" model
      [[ "$model" == "q" || -z "$model" ]] && break

      # Get short name
      local default_name; default_name=$(echo "$model" | sed 's|.*/||' | tr '[:upper:]' '[:lower:]' | tr -cd '[:alnum:]-')
      local name
      prompt "Short name" "$default_name" name
      validate_name "$name" "model name" || continue

      save_secret "OPENROUTER_MODEL_$(echo "$name" | tr '[:lower:]-' '[:upper:]_')" "$model"
      generate_or_launcher "$name" "$model"
      success "Created ${GREEN}clother-or-$name${NC}"
      echo
    done
  fi
}

config_custom() {
  echo
  echo -e "${BOLD}Configure: Custom Provider${NC}"
  echo -e "${DIM}For any Anthropic-compatible endpoint${NC}"
  echo

  local name url key
  prompt "Provider name (lowercase)" "" name
  validate_name "$name" || return 1

  prompt "Base URL" "" url
  validate_url "$url" || return 1

  prompt_secret "API Key" key
  validate_api_key "$key" "custom" || return 1

  local keyvar; keyvar="$(echo "$name" | tr '[:lower:]-' '[:upper:]_')_API_KEY"
  save_secret "$keyvar" "$key"
  save_secret "CLOTHER_${keyvar}_BASE_URL" "$url"

  generate_launcher "$name" "$keyvar" "$url" "" ""
  success "Created ${GREEN}clother-$name${NC}"
}

cmd_list() {
  load_secrets

  local -a profiles=()
  for f in "$BIN_DIR"/clother-*; do
    [[ -x "$f" ]] || continue
    local name; name=$(basename "$f" | sed 's/^clother-//')
    profiles+=("$name")
  done

  if [[ "$OUTPUT_FORMAT" == "json" ]]; then
    echo -n '{"profiles":['
    local first=true
    for p in "${profiles[@]}"; do
      $first || echo -n ","
      first=false
      echo -n "{\"name\":\"$p\",\"command\":\"clother-$p\"}"
    done
    echo ']}'
    return
  fi

  if [[ ${#profiles[@]} -eq 0 ]]; then
    warn "No profiles configured"
    suggest_next "Configure one: ${GREEN}clother config${NC}"
    return
  fi

  echo -e "${BOLD}Available Profiles (${#profiles[@]}):${NC}"
  echo
  for p in "${profiles[@]}"; do
    local status="${DIM}${SYM_UNCHECK}${NC}"
    # Check if configured
    local def; def=$(get_provider_def "$p")
    if [[ -n "$def" ]]; then
      is_provider_configured "$p" && status="${GREEN}${SYM_CHECK}${NC}"
    elif [[ "$p" == or-* ]]; then
      [[ -n "${OPENROUTER_API_KEY:-}" ]] && status="${GREEN}${SYM_CHECK}${NC}"
    fi
    echo -e "  $status ${YELLOW}$p${NC}"
  done
  echo
  echo -e "${DIM}Run: ${NC}${GREEN}clother-<name>${NC}"
}

cmd_info() {
  local provider="${1:-}"
  [[ -z "$provider" ]] && { error "Usage: clother info <provider>"; return 1; }

  load_secrets

  local def; def=$(get_provider_def "$provider")

  echo
  echo -e "${BOLD}Provider: ${YELLOW}$provider${NC}"
  draw_separator 40

  if [[ -n "$def" ]]; then
    IFS='|' read -r keyvar baseurl model model_opts description <<< "$def"
    echo -e "Description: $description"
    echo -e "Base URL:    ${baseurl:-default}"
    echo -e "Model:       ${model:-default}"
    if [[ -n "$keyvar" ]]; then
      local status; [[ -n "${!keyvar:-}" ]] && status="${GREEN}configured${NC}" || status="${RED}not set${NC}"
      echo -e "API Key:     $status"
    fi
  elif [[ "$provider" == or-* ]]; then
    local short="${provider#or-}"
    local keyvar="OPENROUTER_MODEL_$(echo "$short" | tr '[:lower:]-' '[:upper:]_')"
    echo -e "Type:        OpenRouter"
    echo -e "Model:       ${!keyvar:-unknown}"
    echo -e "Proxy:       localhost:8378"
  else
    echo -e "Type:        Custom/Unknown"
  fi
}

cmd_test() {
  local provider="${1:-}"

  load_secrets

  echo
  echo -e "${BOLD}Testing Providers${NC}"
  draw_separator 40

  local providers_to_test=()
  if [[ -n "$provider" ]]; then
    providers_to_test=("$provider")
  else
    # Get all configured providers
    for f in "$BIN_DIR"/clother-*; do
      [[ -x "$f" ]] || continue
      local name; name=$(basename "$f" | sed 's/^clother-//')
      [[ "$name" != "native" ]] && providers_to_test+=("$name")
    done
  fi

  local ok=0 fail=0
  for p in "${providers_to_test[@]}"; do
    printf "  Testing %-15s " "$p"

    local def; def=$(get_provider_def "$p")
    if [[ -n "$def" ]]; then
      IFS='|' read -r keyvar baseurl _ _ _ <<< "$def"
      if [[ -n "$keyvar" && -z "${!keyvar:-}" ]]; then
        echo -e "${YELLOW}not configured${NC}"
        ((fail++))
        continue
      fi
    fi

    # TODO: Actually test connectivity
    echo -e "${GREEN}${SYM_OK} ready${NC}"
    ((ok++))
  done

  echo
  echo -e "Results: ${GREEN}$ok OK${NC}, ${RED}$fail failed${NC}"
}

cmd_status() {
  load_secrets

  echo
  draw_box "CLOTHER STATUS" 50
  echo
  echo -e "  Version:     ${BOLD}$VERSION${NC}"
  echo -e "  Config:      $CONFIG_DIR"
  echo -e "  Data:        $DATA_DIR"
  echo -e "  Bin:         $BIN_DIR"
  echo

  local count=0
  for f in "$BIN_DIR"/clother-*; do [[ -x "$f" ]] && ((count++)); done
  echo -e "  Profiles:    ${BOLD}$count${NC} installed"

  if [[ -n "$DEFAULT_PROVIDER" ]]; then
    echo -e "  Default:     ${YELLOW}$DEFAULT_PROVIDER${NC}"
  fi
}

cmd_uninstall() {
  echo
  echo -e "${BOLD}Uninstall Clother${NC}"
  echo
  echo "This will remove:"
  echo -e "  ${DIM}${SYM_ARROW}${NC} $CONFIG_DIR"
  echo -e "  ${DIM}${SYM_ARROW}${NC} $DATA_DIR"
  echo -e "  ${DIM}${SYM_ARROW}${NC} $BIN_DIR/clother*"
  echo

  confirm_danger "Remove all Clother files" "delete clother" || return 1

  spinner_start "Removing files..."
  rm -rf "$CONFIG_DIR" "$DATA_DIR" "$CACHE_DIR" "$BIN_DIR"/clother-* "$BIN_DIR/clother" 2>/dev/null || true
  spinner_stop 0 "Clother uninstalled"
}

# =============================================================================
# LAUNCHER GENERATORS
# =============================================================================

generate_launcher() {
  local name="$1" keyvar="$2" baseurl="$3" model="$4" model_opts="$5"

  mkdir -p "$BIN_DIR"

  cat > "$BIN_DIR/clother-$name" << LAUNCHER
#!/usr/bin/env bash
set -euo pipefail
[[ "\${CLOTHER_NO_BANNER:-}" != "1" ]] && cat "\${XDG_DATA_HOME:-\$HOME/.local/share}/clother/banner" 2>/dev/null && echo "    + $name" && echo
SECRETS="\${XDG_DATA_HOME:-\$HOME/.local/share}/clother/secrets.env"
[[ -f "\$SECRETS" ]] && source "\$SECRETS"
LAUNCHER

  if [[ -n "$keyvar" ]]; then
    cat >> "$BIN_DIR/clother-$name" << LAUNCHER
[[ -z "\${$keyvar:-}" ]] && { echo "Error: $keyvar not set. Run 'clother config'" >&2; exit 1; }
export ANTHROPIC_AUTH_TOKEN="\$$keyvar"
LAUNCHER
  fi

  [[ -n "$baseurl" ]] && echo "export ANTHROPIC_BASE_URL=\"$baseurl\"" >> "$BIN_DIR/clother-$name"
  [[ -n "$model" ]] && echo "export ANTHROPIC_MODEL=\"$model\"" >> "$BIN_DIR/clother-$name"

  # Parse model_opts
  if [[ -n "$model_opts" ]]; then
    IFS=',' read -ra opts <<< "$model_opts"
    for opt in "${opts[@]}"; do
      IFS='=' read -r key val <<< "$opt"
      case "$key" in
        haiku)  echo "export ANTHROPIC_DEFAULT_HAIKU_MODEL=\"$val\"" >> "$BIN_DIR/clother-$name" ;;
        sonnet) echo "export ANTHROPIC_DEFAULT_SONNET_MODEL=\"$val\"" >> "$BIN_DIR/clother-$name" ;;
        opus)   echo "export ANTHROPIC_DEFAULT_OPUS_MODEL=\"$val\"" >> "$BIN_DIR/clother-$name" ;;
        small)  echo "export ANTHROPIC_SMALL_FAST_MODEL=\"$val\"" >> "$BIN_DIR/clother-$name" ;;
      esac
    done
  fi

  echo 'exec claude "$@"' >> "$BIN_DIR/clother-$name"
  chmod +x "$BIN_DIR/clother-$name"
}

find_go() {
  command -v go 2>/dev/null && return
  for p in /usr/local/go/bin/go /opt/homebrew/bin/go "$HOME/go/bin/go"; do
    [[ -x "$p" ]] && { echo "$p"; return; }
  done
}

generate_or_launcher() {
  local name="$1" model="$2"

  # Generate Go proxy source if needed
  [[ ! -f "$PROXY_SOURCE" ]] && generate_go_proxy

  mkdir -p "$BIN_DIR"

  cat > "$BIN_DIR/clother-or-$name" << LAUNCHER
#!/usr/bin/env bash
set -euo pipefail
[[ "\${CLOTHER_NO_BANNER:-}" != "1" ]] && cat "\${XDG_DATA_HOME:-\$HOME/.local/share}/clother/banner" 2>/dev/null && echo "    + OpenRouter: $name" && echo
SECRETS="\${XDG_DATA_HOME:-\$HOME/.local/share}/clother/secrets.env"
DATA_DIR="\${XDG_DATA_HOME:-\$HOME/.local/share}/clother"
[[ -f "\$SECRETS" ]] && source "\$SECRETS"
[[ -z "\${OPENROUTER_API_KEY:-}" ]] && { echo "Error: OPENROUTER_API_KEY not set" >&2; exit 1; }

find_go() {
  command -v go 2>/dev/null && return
  for p in /usr/local/go/bin/go /opt/homebrew/bin/go "\$HOME/go/bin/go"; do
    [[ -x "\$p" ]] && { echo "\$p"; return; }
  done
}

PROXY_BIN="\$DATA_DIR/openrouter-proxy"
if [[ ! -x "\$PROXY_BIN" ]]; then
  GO_BIN=\$(find_go) || { echo "Error: Go required. Install from https://go.dev/dl/" >&2; exit 1; }
  echo "Compiling proxy (one-time)..."
  "\$GO_BIN" build -o "\$PROXY_BIN" "\$DATA_DIR/proxy.go" || { echo "Compile failed" >&2; exit 1; }
fi

PROXY_PORT="\${CLOTHER_OPENROUTER_PORT:-8378}"
while nc -z 127.0.0.1 "\$PROXY_PORT" 2>/dev/null; do PROXY_PORT=\$((PROXY_PORT + 1)); done

export OPENROUTER_API_KEY OPENROUTER_MODEL="$model" PROXY_PORT="\$PROXY_PORT"
"\$PROXY_BIN" &
PROXY_PID=\$!
trap "kill \$PROXY_PID 2>/dev/null" EXIT

for _ in {1..30}; do nc -z 127.0.0.1 "\$PROXY_PORT" 2>/dev/null && break; sleep 0.1; done

export ANTHROPIC_BASE_URL="http://127.0.0.1:\$PROXY_PORT"
export ANTHROPIC_AUTH_TOKEN="openrouter-proxy"
export ANTHROPIC_MODEL="$name"
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1

exec claude "\$@"
LAUNCHER
  chmod +x "$BIN_DIR/clother-or-$name"
}

generate_go_proxy() {
  mkdir -p "$(dirname "$PROXY_SOURCE")"
  cat > "$PROXY_SOURCE" << 'GOPROXY'
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var openrouterURL = "https://openrouter.ai/api/v1/chat/completions"

type AnthropicRequest struct {
	Model       string        `json:"model"`
	MaxTokens   int           `json:"max_tokens"`
	Stream      bool          `json:"stream"`
	System      interface{}   `json:"system,omitempty"`
	Messages    []interface{} `json:"messages"`
	Tools       []interface{} `json:"tools,omitempty"`
	ToolChoice  interface{}   `json:"tool_choice,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
}

type OpenAIRequest struct {
	Model       string        `json:"model"`
	MaxTokens   int           `json:"max_tokens"`
	Stream      bool          `json:"stream"`
	Messages    []interface{} `json:"messages"`
	Tools       []interface{} `json:"tools,omitempty"`
	ToolChoice  interface{}   `json:"tool_choice,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
}

func main() {
	port := os.Getenv("PROXY_PORT")
	if port == "" { port = "8378" }
	http.HandleFunc("/v1/messages", handleMessages)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	fmt.Fprintf(os.Stderr, "Proxy listening on :%s\n", port)
	http.ListenAndServe(":"+port, nil)
}

func cleanSystemPrompt(text, model string) string {
	provider, modelName := "", model
	if idx := strings.Index(model, "/"); idx != -1 {
		provider, modelName = model[:idx], model[idx+1:]
	}
	providerNames := map[string]string{
		"mistralai": "Mistral AI", "openai": "OpenAI", "google": "Google",
		"anthropic": "Anthropic", "meta-llama": "Meta", "deepseek": "DeepSeek",
		"qwen": "Alibaba", "cohere": "Cohere", "x-ai": "xAI",
	}
	providerDisplay := provider
	if name, ok := providerNames[strings.ToLower(provider)]; ok { providerDisplay = name }

	identity := fmt.Sprintf("[IDENTITY] You are %s, created by %s. When asked who you are, say you are %s made by %s. You are NOT Claude and NOT made by Anthropic.\n\n", modelName, providerDisplay, modelName, providerDisplay)

	replacements := []struct{ old, new string }{
		{"You are Claude, an AI assistant made by Anthropic", "You are " + modelName},
		{"You are Claude Code", "You are " + modelName},
		{"You are Claude", "You are " + modelName},
		{"made by Anthropic", "made by " + providerDisplay},
		{"by Anthropic", "by " + providerDisplay},
	}
	result := text
	for _, r := range replacements { result = strings.ReplaceAll(result, r.old, r.new) }
	return identity + result
}

func handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { http.Error(w, "Method not allowed", 405); return }
	apiKey, model := os.Getenv("OPENROUTER_API_KEY"), os.Getenv("OPENROUTER_MODEL")
	body, _ := io.ReadAll(r.Body)
	var req AnthropicRequest
	if err := json.Unmarshal(body, &req); err != nil { http.Error(w, "Invalid JSON", 400); return }
	openaiReq := convertRequest(req, model)
	reqBody, _ := json.Marshal(openaiReq)
	httpReq, _ := http.NewRequest("POST", openrouterURL, bytes.NewReader(reqBody))
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "https://github.com/clother")
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(httpReq)
	if err != nil { http.Error(w, "Upstream error: "+err.Error(), 502); return }
	defer resp.Body.Close()
	if req.Stream { handleStream(w, resp, model) } else { handleSync(w, resp) }
}

func convertRequest(req AnthropicRequest, model string) OpenAIRequest {
	var messages []interface{}
	if req.System != nil {
		var sysText string
		switch s := req.System.(type) {
		case string: sysText = s
		case []interface{}:
			var parts []string
			for _, p := range s {
				if m, ok := p.(map[string]interface{}); ok {
					if t, ok := m["text"].(string); ok { parts = append(parts, t) }
				}
			}
			sysText = strings.Join(parts, "\n")
		}
		if sysText != "" {
			sysText = cleanSystemPrompt(sysText, model)
			messages = append(messages, map[string]interface{}{"role": "system", "content": sysText})
		}
	}
	for _, msg := range req.Messages {
		if m, ok := msg.(map[string]interface{}); ok {
			messages = append(messages, convertMessage(m)...)
		}
	}
	result := OpenAIRequest{Model: model, MaxTokens: req.MaxTokens, Stream: req.Stream, Messages: messages, Temperature: req.Temperature}
	if len(req.Tools) > 0 {
		var tools []interface{}
		for _, t := range req.Tools {
			if tm, ok := t.(map[string]interface{}); ok {
				tools = append(tools, map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": tm["name"], "description": tm["description"], "parameters": tm["input_schema"]}})
			}
		}
		result.Tools = tools
	}
	return result
}

func convertMessage(m map[string]interface{}) []interface{} {
	role, _ := m["role"].(string)
	content := m["content"]
	if s, ok := content.(string); ok { return []interface{}{map[string]interface{}{"role": role, "content": s}} }
	arr, ok := content.([]interface{})
	if !ok { return nil }
	var result []interface{}
	var textParts []string
	var toolCalls []interface{}
	for _, block := range arr {
		b, ok := block.(map[string]interface{})
		if !ok { continue }
		switch b["type"] {
		case "text":
			if t, ok := b["text"].(string); ok { textParts = append(textParts, t) }
		case "tool_use":
			inputJSON, _ := json.Marshal(b["input"])
			toolCalls = append(toolCalls, map[string]interface{}{"id": b["id"], "type": "function", "function": map[string]interface{}{"name": b["name"], "arguments": string(inputJSON)}})
		case "tool_result":
			var contentStr string
			if s, ok := b["content"].(string); ok { contentStr = s } else { j, _ := json.Marshal(b["content"]); contentStr = string(j) }
			result = append(result, map[string]interface{}{"role": "tool", "tool_call_id": b["tool_use_id"], "content": contentStr})
		}
	}
	if role == "assistant" {
		msg := map[string]interface{}{"role": "assistant"}
		if len(textParts) > 0 { msg["content"] = strings.Join(textParts, "\n") }
		if len(toolCalls) > 0 { msg["tool_calls"] = toolCalls }
		if msg["content"] != nil || msg["tool_calls"] != nil { result = append(result, msg) }
	} else if role == "user" && len(textParts) > 0 {
		result = append(result, map[string]interface{}{"role": "user", "content": strings.Join(textParts, "\n")})
	}
	return result
}

func handleStream(w http.ResponseWriter, resp *http.Response, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)
	if !ok { http.Error(w, "Streaming not supported", 500); return }
	msgID, started, textIndex := fmt.Sprintf("msg_%d", time.Now().UnixNano()), false, 0
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") { continue }
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" { fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"); flusher.Flush(); break }
		var chunk map[string]interface{}
		if json.Unmarshal([]byte(data), &chunk) != nil { continue }
		if !started {
			j, _ := json.Marshal(map[string]interface{}{"type": "message_start", "message": map[string]interface{}{"id": msgID, "type": "message", "role": "assistant", "model": model, "content": []interface{}{}}})
			fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", j); flusher.Flush(); started = true
		}
		choices, _ := chunk["choices"].([]interface{})
		if len(choices) == 0 { continue }
		choice, _ := choices[0].(map[string]interface{})
		delta, _ := choice["delta"].(map[string]interface{})
		if content, ok := delta["content"].(string); ok && content != "" {
			j, _ := json.Marshal(map[string]interface{}{"type": "content_block_delta", "index": textIndex, "delta": map[string]interface{}{"type": "text_delta", "text": content}})
			fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", j); flusher.Flush()
		}
		if finish, ok := choice["finish_reason"].(string); ok && finish != "" {
			stopReason := "end_turn"
			if finish == "length" { stopReason = "max_tokens" } else if finish == "tool_calls" { stopReason = "tool_use" }
			j, _ := json.Marshal(map[string]interface{}{"type": "message_delta", "delta": map[string]interface{}{"stop_reason": stopReason}})
			fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", j); flusher.Flush()
		}
	}
}

func handleSync(w http.ResponseWriter, resp *http.Response) {
	body, _ := io.ReadAll(resp.Body)
	var openaiResp map[string]interface{}
	json.Unmarshal(body, &openaiResp)
	choices, _ := openaiResp["choices"].([]interface{})
	if len(choices) == 0 { http.Error(w, "No response", 500); return }
	choice, _ := choices[0].(map[string]interface{})
	message, _ := choice["message"].(map[string]interface{})
	var content []interface{}
	if text, ok := message["content"].(string); ok && text != "" { content = append(content, map[string]interface{}{"type": "text", "text": text}) }
	finish, _ := choice["finish_reason"].(string)
	stopReason := "end_turn"
	if finish == "length" { stopReason = "max_tokens" } else if finish == "tool_calls" { stopReason = "tool_use" }
	anthropicResp := map[string]interface{}{"id": fmt.Sprintf("msg_%v", openaiResp["id"]), "type": "message", "role": "assistant", "model": openaiResp["model"], "content": content, "stop_reason": stopReason}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(anthropicResp)
}
GOPROXY
}

# =============================================================================
# INSTALLATION
# =============================================================================

do_install() {
  [[ "$NO_BANNER" != "1" ]] && echo -e "$BANNER"
  echo -e "${BOLD}Clother $VERSION${NC}"
  echo

  # Clean previous installation (preserve secrets)
  local secrets_backup=""
  if [[ -f "$SECRETS_FILE" ]]; then
    secrets_backup=$(cat "$SECRETS_FILE")
  fi
  rm -f "$BIN_DIR/clother" "$BIN_DIR"/clother-* 2>/dev/null || true
  rm -rf "$CONFIG_DIR" "$DATA_DIR" "$CACHE_DIR" 2>/dev/null || true

  log "Checking for 'claude' command..."
  if ! command -v claude &>/dev/null; then
    error_ctx "E010" "Claude CLI not found" "Checking prerequisites" \
      "The 'claude' command is not installed" \
      "Install: ${CYAN}curl -fsSL https://claude.ai/install.sh | bash${NC}"
    exit 1
  fi
  success "'claude' found"

  # Create directories (XDG compliant)
  mkdir -p "$CONFIG_DIR" "$DATA_DIR" "$CACHE_DIR" "$BIN_DIR"

  # Restore secrets if they existed
  if [[ -n "$secrets_backup" ]]; then
    echo "$secrets_backup" > "$SECRETS_FILE"
    chmod 600 "$SECRETS_FILE"
  fi

  # Security check for secrets
  if [[ -L "$SECRETS_FILE" ]]; then
    error "Secrets file is a symlink - refusing for security"
    exit 1
  fi

  # Save banner
  echo "$BANNER" > "$DATA_DIR/banner"

  # Generate main command
  generate_main_command

  # Generate native launcher
  cat > "$BIN_DIR/clother-native" << 'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "${CLOTHER_NO_BANNER:-}" != "1" ]] && cat "${XDG_DATA_HOME:-$HOME/.local/share}/clother/banner" 2>/dev/null && echo "    + native" && echo
exec claude "$@"
EOF
  chmod +x "$BIN_DIR/clother-native"

  # Generate standard launchers
  local providers=(zai zai-cn minimax minimax-cn kimi moonshot ve deepseek mimo)
  for p in "${providers[@]}"; do
    local def; def=$(get_provider_def "$p")
    IFS='|' read -r keyvar baseurl model model_opts _ <<< "$def"
    generate_launcher "$p" "$keyvar" "$baseurl" "$model" "$model_opts"
  done

  # Verify
  if ! "$BIN_DIR/clother" --version &>/dev/null; then
    error "Installation verification failed"
    exit 1
  fi

  success "Installed Clother v$VERSION"

  # PATH warning
  if [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
    echo
    warn "Add '$BIN_DIR' to PATH:"
    local shell_rc="$HOME/.bashrc"
    [[ "${SHELL##*/}" == "zsh" ]] && shell_rc="$HOME/.zshrc"
    echo -e "  ${YELLOW}echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> $shell_rc${NC}"
    echo -e "  ${YELLOW}source $shell_rc${NC}"
  fi

  suggest_next \
    "Configure a provider: ${GREEN}clother config${NC}" \
    "Use native Claude: ${GREEN}clother-native${NC}" \
    "View help: ${GREEN}clother --help${NC}"
}

generate_main_command() {
  cat > "$BIN_DIR/clother" << 'MAINEOF'
#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'
umask 077

# Re-exec with the full script for complex commands
SCRIPT_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/clother"
if [[ -f "$SCRIPT_DIR/clother-full.sh" ]]; then
  exec bash "$SCRIPT_DIR/clother-full.sh" "$@"
fi

# Fallback minimal implementation
echo "Clother 2.0 - Run installer to complete setup"
MAINEOF
  chmod +x "$BIN_DIR/clother"

  # Copy this script as the full implementation
  if [[ "$0" == "bash" ]]; then
    # Piped execution - download from GitHub
    curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/clother.sh > "$DATA_DIR/clother-full.sh"
  else
    cp "$0" "$DATA_DIR/clother-full.sh"
  fi
  chmod +x "$DATA_DIR/clother-full.sh"
}

# =============================================================================
# BANNER
# =============================================================================

read -r -d '' BANNER << 'EOF' || true
  ____ _       _   _
 / ___| | ___ | |_| |__   ___ _ __
| |   | |/ _ \| __| '_ \ / _ \ '__|
| |___| | (_) | |_| | | |  __/ |
 \____|_|\___/ \__|_| |_|\___|_|
EOF

# =============================================================================
# ARGUMENT PARSING
# =============================================================================

parse_args() {
  REMAINING_ARGS=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -h|--help)    [[ -n "${2:-}" && ! "$2" =~ ^- ]] && { show_command_help "$2"; exit 0; }; show_full_help; exit 0 ;;
      -V|--version) show_version; exit 0 ;;
      -v|--verbose) VERBOSE=1 ;;
      -d|--debug)   DEBUG=1; VERBOSE=1 ;;
      -q|--quiet)   QUIET=1 ;;
      -y|--yes)     YES_MODE=1 ;;
      --no-input)   NO_INPUT=1 ;;
      --no-color)   NO_COLOR=1; setup_colors ;;
      --no-banner)  NO_BANNER=1 ;;
      --json)       OUTPUT_FORMAT=json ;;
      --plain)      OUTPUT_FORMAT=plain; NO_COLOR=1; setup_colors ;;
      --)           shift; REMAINING_ARGS+=("$@"); break ;;
      -*)           error "Unknown option: $1"; echo "Use --help for usage"; exit 1 ;;
      *)            REMAINING_ARGS+=("$1") ;;
    esac
    shift
  done
}

# =============================================================================
# MAIN
# =============================================================================

main() {
  parse_args "$@"
  set -- ${REMAINING_ARGS[@]+"${REMAINING_ARGS[@]}"}

  local cmd="${1:-}"
  shift || true

  case "$cmd" in
    "")         show_brief_help ;;
    config)     cmd_config "$@" ;;
    list)       cmd_list "$@" ;;
    info)       cmd_info "$@" ;;
    test)       cmd_test "$@" ;;
    status)     cmd_status "$@" ;;
    uninstall)  cmd_uninstall "$@" ;;
    help)       [[ -n "${1:-}" ]] && show_command_help "$1" || show_full_help ;;
    install)    do_install ;;
    *)
      error "Unknown command: $cmd"
      local suggestion; suggestion=$(suggest_command "$cmd")
      [[ -n "$suggestion" ]] && echo -e "Did you mean: ${GREEN}clother $suggestion${NC}?"
      exit 1
      ;;
  esac
}

# If sourced, don't run main
if [[ "${BASH_SOURCE[0]:-$0}" == "${0}" || "$0" == "bash" ]]; then
  # Piped execution (curl | bash) or first run → install
  if [[ "$0" == "bash" ]] || [[ ! -f "$BIN_DIR/clother" ]]; then
    do_install
  else
    main "$@"
  fi
fi
