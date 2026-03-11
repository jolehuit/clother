#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

pass() {
  echo "PASS: $*"
}

test_load_secrets_prefers_linux_stat_permissions() (
  local tmpdir
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT

  export HOME="$tmpdir/home"
  export XDG_CONFIG_HOME="$HOME/.config"
  export XDG_DATA_HOME="$HOME/.local/share"
  export XDG_CACHE_HOME="$HOME/.cache"

  mkdir -p "$XDG_DATA_HOME/clother"
  cat > "$XDG_DATA_HOME/clother/secrets.env" <<'EOF'
ZAI_API_KEY=test-key
EOF
  command chmod 600 "$XDG_DATA_HOME/clother/secrets.env"

  # shellcheck source=../clother.sh
  source "$REPO_ROOT/clother.sh"

  stat() {
    if [[ "$1" == "-c" && "$2" == "%a" ]]; then
      printf '600\n'
      return 0
    fi

    if [[ "$1" == "-f" && "$2" == "%Lp" ]]; then
      printf 'linux-filesystem-info\n'
      return 0
    fi

    command stat "$@"
  }

  chmod() {
    echo "load_secrets should not call chmod when GNU stat already returns 600" >&2
    return 99
  }

  load_secrets
  [[ "${ZAI_API_KEY:-}" == "test-key" ]] || fail "load_secrets did not source the secrets file"
)

test_load_secrets_uses_preincrement_guard() (
  local load_secrets_body
  load_secrets_body="$(sed -n '/^load_secrets() {/,/^}/p' "$REPO_ROOT/clother.sh")"

  [[ "$load_secrets_body" == *'((++line_num))'* ]] || fail "load_secrets no longer uses pre-increment for line_num"
  [[ "$load_secrets_body" != *'((line_num++))'* ]] || fail "load_secrets regressed to post-increment for line_num"
)

test_load_secrets_prefers_linux_stat_permissions
pass "load_secrets prefers GNU stat permissions over Linux stat -f output"

test_load_secrets_uses_preincrement_guard
pass "load_secrets keeps the pre-increment guard for set -e compatibility"
