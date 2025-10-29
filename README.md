```text
  ____ _       _   _               
 / ___| | ___ | |_| |__   ___ _ __ 
| |   | |/ _ \| __| '_ \ / _ \ '__|
| |___| | (_) | |_| | | |  __/ |   
 \____|_|\___/ \__|_| |_|\___|_|   
```

# Clother

**One CLI to manage them all.** Switch between multiple Claude Code–compatible LLM providers instantly.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Shell](https://img.shields.io/badge/Shell-Bash-green.svg)](https://www.gnu.org/software/bash/)
[![Platform](https://img.shields.io/badge/Platform-macOS%20|%20Linux-lightgrey.svg)](#platform-support)

Works with Anthropic and any Claude-compatible endpoint: Z.AI (GLM), MiniMax, Moonshot AI (Kimi), KAT-Coder, and custom providers.

---

## Features

- 🔒 **Secure** — API keys in `chmod 600` files, never in shell history
- 🚀 **Fast** — Zero overhead, pure bash scripts
- 🎯 **Simple** — One command: `clother-zai`, `clother-minimax`, etc.
- 🔧 **Extensible** — Add custom providers in 30 seconds
- 📦 **Lightweight** — Single ~700 line script

---

## Installation

**Requirements:** [Claude Code CLI](https://github.com/anthropics/claude-code)

```bash
npm install -g @anthropic-ai/claude-code
```

**Install Clother:**

```bash
curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/clother.sh | bash
```

If prompted, add `~/bin` to your `PATH` and reload your shell.

---

## Quick Start

```bash
# 1. Configure a provider
clother config

# 2. List available providers
clother list

# 3. Launch Claude with Z.AI
clother-zai

# 4. Check provider details
clother info zai
```

---

## Usage

### Switch Between Providers

```bash
clother-zai              # Use Z.AI (GLM models)
clother-minimax          # Use MiniMax
clother-kimi             # Use Moonshot AI (Kimi)
clother-native           # Use native Anthropic
```

### Pass Options to Claude

```bash
clother-zai --dangerously-skip-permissions
clother-minimax --help
```

### All Commands

```bash
clother config          # Configure a provider
clother list            # Show all launchers
clother info <name>     # Show provider details
clother uninstall       # Remove everything
clother help            # Show help
```

---

## Built-in Providers

| Provider | Endpoint | Models | API Key |
|----------|----------|--------|---------|
| **native** | Anthropic | Claude Sonnet/Opus/Haiku | [console.anthropic.com](https://console.anthropic.com) |
| **zai** | Z.AI | GLM-4.5-air, GLM-4.6 | [z.ai](https://z.ai) |
| **minimax** | MiniMax | MiniMax-M2 | [minimax.io](https://minimax.io) |
| **katcoder** | KAT-Coder | KAT-Coder | [streamlake.ai](https://streamlake.ai) |
| **kimi** | Moonshot AI | Kimi-K2 variants | [moonshot.ai](https://moonshot.ai) |
| **custom** | Your own | Any | — |

### Add a Custom Provider

```bash
clother config
# Choose: 6) custom
# Enter: name, base URL, API key
```

Creates `clother-<name>` immediately usable.

---

## How It Works

Clother creates launcher scripts that:

1. Load secrets from `~/.clother/secrets.env`
2. Export `ANTHROPIC_BASE_URL` and `ANTHROPIC_AUTH_TOKEN`
3. Execute `claude` with your arguments

**Example:** `clother-zai` does this behind the scenes:

```bash
export ANTHROPIC_BASE_URL="https://api.z.ai/api/anthropic"
export ANTHROPIC_AUTH_TOKEN="$ZAI_API_KEY"
exec claude "$@"
```

---

## Platform Support

| OS | Shell | Status |
|----|-------|--------|
| macOS | zsh/bash | ✅ Fully supported |
| Linux | zsh/bash | ✅ Fully supported |
| Windows | WSL | ✅ Recommended |
| Windows | Git Bash | ⚠️ May work |
| Windows | PowerShell | ❌ Not supported |

---

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `claude: command not found` | Install Claude Code CLI: `npm install -g @anthropic-ai/claude-code` |
| `clother: command not found` | Add `~/bin` to PATH: `echo 'export PATH="$HOME/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc` |
| `... API key not set` | Run `clother config` |

---

## Uninstall

```bash
clother uninstall
```

Removes `~/.clother` and all `clother-*` launchers (with `$HOME` safety check).

---

## FAQ

**Q: Can I use multiple providers at once?**  
A: Each terminal uses one provider. Open multiple terminals for different providers.

**Q: Where are my API keys stored?**  
A: In `~/.clother/secrets.env` with `chmod 600` (readable only by you).

**Q: Does this modify my Claude installation?**  
A: No. It only sets environment variables before launching `claude`.

---

## License

MIT © [jolehuit](https://github.com/jolehuit)

---

**Made with ☕ for developers who value simplicity.**
