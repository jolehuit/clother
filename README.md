# Clother

Manage and switch between multiple Claude Code–compatible LLM providers from one tiny CLI.
Works with native Anthropic and any vendor exposing a Claude-compatible endpoint (currently Z.AI GLM, MiniMax, KAT-Coder).

Why? More and more LLM vendors ship Claude-compatible APIs. Clother lets you configure them once, then launch with quick commands like clother-zai, clother-minimax, etc.
---

## Install

Requires the **Claude Code CLI** (`claude`):

```bash
npm install -g @anthropic-ai/claude-code
```

Install Clother:

```bash
curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/clother.sh | bash
```

If prompted, add `~/bin` (or `$CLOTHER_BIN`) to your `PATH`, then reload your shell.

---

## Quick start

```bash
clother config   # set up a provider (native/zai/minimax/katcoder/custom)
clother list     # show launchers
clother info zai # show provider details
clother-zai      # run Claude via Z.AI
clother help     # all commands
```

---

## What it does

* Creates secure `~/.clother/secrets.env` (not a symlink, `chmod 600`)
* Writes launchers: `clother-<name>` that export `ANTHROPIC_BASE_URL`/`ANTHROPIC_AUTH_TOKEN` and exec `claude`
---

## Providers (built-in defaults)

* **native** — Anthropic CLI as-is
* **zai** — `https://api.z.ai/api/anthropic` (needs `ZAI_API_KEY`)
* **minimax** — `https://api.minimax.io/anthropic` (needs `MINIMAX_API_KEY`)
* **katcoder** — `https://vanchin.streamlake.ai/api/gateway/v1/endpoints/$VC_ENDPOINT_ID/claude-code-proxy` (needs `VC_API_KEY`, `VC_ENDPOINT_ID`)

---

## OS notes

* **macOS/Linux:** zsh/bash; PATH update via `~/.zshrc` or `~/.bashrc`.
* **Windows:** use **WSL** (recommended) or **Git Bash**. Native PowerShell isn’t supported.

---

## Uninstall

```bash
clother uninstall
```

Removes `~/.clother` and all `clother-*` launchers (with `$HOME` safety check).

---

## Troubleshooting

* `claude: command not found` → install the CLI, ensure npm bin is on `PATH`.
* `... API key not set` → run `clother config`.
* `clother-*: command not found` → add `$HOME/bin` to `PATH` and reload shell.
