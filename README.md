# Clother

```
  ____ _       _   _
 / ___| | ___ | |_| |__   ___ _ __
| |   | |/ _ \| __| '_ \ / _ \ '__|
| |___| | (_) | |_| | | |  __/ |
 \____|_|\___/ \__|_| |_|\___|_|
```

**One CLI to switch between Claude Code providers instantly.**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Shell](https://img.shields.io/badge/Shell-Bash-green.svg)](https://www.gnu.org/software/bash/)
[![Platform](https://img.shields.io/badge/Platform-macOS%20|%20Linux-lightgrey.svg)](#platform-support)

🔒 Secure • 🚀 Fast • 📦 XDG Compliant

## Installation

**Requirements:** Claude Code CLI

```bash
# 1. Install Claude Code CLI
curl -fsSL https://claude.ai/install.sh | bash

# 2. Install Clother
curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/clother.sh | bash
```

## Quick Start

**Got a Claude Pro/Team subscription?**
```bash
clother-native              # Use your subscription, no setup needed!
```

**Want to use alternative models?**
```bash
clother config              # Set up Z.AI, MiniMax, Kimi, etc.
clother-zai                 # Launch with Z.AI (GLM)
clother-minimax             # Launch with MiniMax
```

**Want 100+ models via OpenRouter?**
```bash
clother config openrouter   # Set up OpenRouter
clother-or-devstral         # Launch with Devstral
```

## Providers

### Native Anthropic (Your Subscription)

```bash
clother-native              # Claude Sonnet/Opus/Haiku
                           # Uses your Claude Pro/Team subscription
                           # No API key needed
```

### International

| Command | Provider | Models | Get API Key |
|---------|----------|--------|-------------|
| `clother-zai` | Z.AI | GLM-4.5-air, GLM-4.7 | [z.ai](https://z.ai) |
| `clother-minimax` | MiniMax | MiniMax-M2 | [minimax.io](https://minimax.io) |
| `clother-kimi` | Kimi | kimi-k2-thinking-turbo | [kimi.com](https://kimi.com) |
| `clother-moonshot` | Moonshot AI | kimi-k2-turbo-preview | [moonshot.ai](https://moonshot.ai) |
| `clother-deepseek` | DeepSeek | deepseek-chat | [deepseek.com](https://platform.deepseek.com) |
| `clother-mimo` | Xiaomi MiMo | mimo-v2-flash | [xiaomimimo.com](https://platform.xiaomimimo.com) |

### China Endpoints 🇨🇳

| Command | Provider | Endpoint |
|---------|----------|----------|
| `clother-zai-cn` | Z.AI (China) | open.bigmodel.cn |
| `clother-minimax-cn` | MiniMax (China) | api.minimaxi.com |
| `clother-ve` | VolcEngine | ark.cn-beijing.volces.com |

### Advanced

| Command | Provider | Description |
|---------|----------|-------------|
| `clother-or-*` | OpenRouter | 100+ models via native API |
| `clother-<custom>` | Custom | Any Anthropic-compatible endpoint |

## OpenRouter (100+ Models)

Access Grok, Gemini, Mistral and more through OpenRouter's native Anthropic API.

```bash
clother config openrouter   # Enter API key from https://openrouter.ai/keys

# Add models interactively:
Model ID: moonshotai/kimi-k2-0905:exacto
Short name: kimi-k2         # Creates: clother-or-kimi-k2

clother-or-kimi-k2          # Use it!
```

> **Important:** For non-Claude models, use the `:exacto` variant (e.g. `moonshotai/kimi-k2-0905:exacto`).
> Exacto handles Claude Code's message format better and provides reliable tool use support.
> See [OpenRouter Exacto docs](https://openrouter.ai/docs/features/exacto) for details.

## Custom Providers

Add any Anthropic-compatible endpoint:

```bash
clother config              # Choose "custom"
clother-myprovider          # Ready to use!
```

## Commands

| Command | Description |
|---------|-------------|
| `clother config [provider]` | Configure provider (interactive menu if no args) |
| `clother list [--json]` | List configured profiles |
| `clother info <name>` | Show provider details |
| `clother test [provider]` | Test connectivity |
| `clother status` | Show installation status |
| `clother uninstall` | Remove everything |

### Flags

| Flag | Description |
|------|-------------|
| `-v, --verbose` | Verbose output |
| `-q, --quiet` | Minimal output |
| `-y, --yes` | Auto-confirm prompts |
| `--json` | JSON output |
| `--no-color` | Disable colors |

## Examples

```bash
# Pass any Claude Code options
clother-zai --dangerously-skip-permissions

# Check what's configured
clother list
clother info zai

# Machine-readable output
clother list --json | jq '.profiles[].name'
```

## How It Works

Clother creates lightweight launcher scripts that set environment variables:

```bash
# When you run: clother-zai
# It does:
export ANTHROPIC_BASE_URL="https://api.z.ai/api/anthropic"
export ANTHROPIC_AUTH_TOKEN="$ZAI_API_KEY"
exec claude "$@"
```

API keys stored securely in `~/.local/share/clother/secrets.env` (chmod 600).

## File Locations

Follows [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html):

```
~/.config/clother/           # Configuration
~/.local/share/clother/      # Data (secrets)
~/bin/clother-*              # Launcher scripts
```

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `claude: command not found` | Install Claude CLI first |
| `clother: command not found` | Add `~/bin` to PATH |
| `API key not set` | Run `clother config` |

## Platform Support

✅ macOS (zsh/bash) • ✅ Linux (zsh/bash) • ✅ Windows (WSL)

**Requirements:** Bash 4.0+, Claude Code CLI

## Contributors

Thanks to everyone who helped improve Clother:

- [@darkokoa](https://github.com/darkokoa) — China endpoints (zai-cn, minimax-cn, ve)
- [@RawToast](https://github.com/RawToast) — Kimi Coding Plan endpoint fix

PRs welcome! 🙏

## License

MIT © [jolehuit](https://github.com/jolehuit)
