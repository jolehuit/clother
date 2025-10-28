```text
  ____ _       _   _               
 / ___| | ___ | |_| |__   ___ _ __ 
| |   | |/ _ \| __| '_ \ / _ \ '__|
| |___| | (_) | |_| | | |  __/ |   
 \____|_|\___/ \__|_| |_|\___|_|   
```

Manage and switch between multiple **Claude Code**–compatible LLM providers from one tiny CLI.

Works natively with Anthropic, and with any vendor exposing a Claude-compatible endpoint — including Z.AI (GLM), MiniMax, Moonshot AI (Kimi), and KAT-Coder.

---

## Install

Requires the **Claude Code CLI** (`claude`):

```bash
npm install -g @anthropic-ai/claude-code
```

Install **Clother**:

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
* Writes launchers: `clother-<name>` that export `ANTHROPIC_BASE_URL` / `ANTHROPIC_AUTH_TOKEN` and exec `claude`

---

## Providers (built-in defaults)

| Name         | Base URL                                                                                   | Required Env                   |
| ------------ | ------------------------------------------------------------------------------------------ | ------------------------------ |
| **native**   | Anthropic CLI as-is                                                                        | —                              |
| **zai**      | `https://api.z.ai/api/anthropic`                                                           | `ZAI_API_KEY`                  |
| **minimax**  | `https://api.minimax.io/anthropic`                                                         | `MINIMAX_API_KEY`              |
| **katcoder** | `https://vanchin.streamlake.ai/api/gateway/v1/endpoints/$VC_ENDPOINT_ID/claude-code-proxy` | `VC_API_KEY`, `VC_ENDPOINT_ID` |
| **kimi**     | `https://api.moonshot.ai/anthropic`                                                        | `KIMI_API_KEY`                   |

---

## OS notes

* **macOS/Linux:** zsh/bash; update PATH via `~/.zshrc` or `~/.bashrc`.
* **Windows:** use **WSL** (recommended) or **Git Bash**. Native PowerShell isn’t supported.

---

## Uninstall

```bash
clother uninstall
```

Removes `~/.clother` and all `clother-*` launchers (with `$HOME` safety check).

---

## 🧭 Troubleshooting

| Problem                        | Solution                                      |
| ------------------------------ | --------------------------------------------- |
| `claude: command not found`    | Install the CLI, ensure npm bin is on `PATH`. |
| `... API key not set`          | Run `clother config`.                         |
| `clother-*: command not found` | Add `$HOME/bin` to `PATH` and reload shell.   |
