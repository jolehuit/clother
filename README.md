```text
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

🔒 Secure • 🚀 Fast • 📦 Lightweight (~500 lines)

---

## Installation

```bash
# 1. Install Claude Code CLI
npm install -g @anthropic-ai/claude-code

# 2. Install Clother
curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/clother.sh | bash

# 3. Add to PATH (if prompted)
echo 'export PATH="$HOME/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```

---

## Quick Start

### Got a Claude Pro/Team subscription?

```bash
clother-native              # Use your subscription, no setup needed!
```

### Want to use alternative models?

```bash
clother config              # Set up Z.AI, MiniMax, Kimi, etc.
clother-zai                 # Launch with Z.AI (GLM)
clother-minimax             # Launch with MiniMax
```

---

## Providers

### Native Anthropic (Your Subscription)

```bash
clother-native              # Claude Sonnet/Opus/Haiku
                           # Uses your Claude Pro/Team subscription
                           # No API key needed
```

### Alternative Models

| Command | Provider | Models | Get API Key |
|---------|----------|--------|-------------|
| `clother-zai` | Z.AI | GLM-4.5-air, GLM-4.6 | [z.ai](https://z.ai) |
| `clother-minimax` | MiniMax | MiniMax-M2 | [minimax.io](https://minimax.io) |
| `clother-kimi` | Kimi Coding | kimi-k2-thinking-turbo | [kimi.com](https://kimi.com) |
| `clother-moonshot` | Moonshot AI | kimi-k2-turbo-preview | [moonshot.ai](https://moonshot.ai) |
| `clother-deepseek` | DeepSeek | deepseek-chat | [deepseek.com](https://platform.deepseek.com) |
| `clother-katcoder` | KAT-Coder | KAT-Coder | [streamlake.ai](https://streamlake.ai) |

### China Endpoints 🇨🇳

| Command | Provider | Endpoint |
|---------|----------|----------|
| `clother-zai-cn` | Z.AI (China) | open.bigmodel.cn |
| `clother-minimax-cn` | MiniMax (China) | api.minimaxi.com |
| `clother-ve` | VolcEngine | ark.cn-beijing.volces.com |

### Custom Providers

Add your own Anthropic-compatible endpoint:

```bash
clother config              # Choose "custom"
clother-myprovider          # Ready to use!
```

---

## Commands

```bash
clother config              # Set up a provider
clother list                # Show configured providers
clother info <name>         # Show provider details
clother uninstall           # Remove everything
```

---

## Examples

```bash

# Pass any Claude Code options
clother-zai --dangerously-skip-permissions

# Check what's configured
clother list
clother info zai
```

---

## How It Works

Clother creates tiny launcher scripts that set environment variables:

```bash
# When you run: clother-zai
# It does:
export ANTHROPIC_BASE_URL="https://api.z.ai/api/anthropic"
export ANTHROPIC_AUTH_TOKEN="$ZAI_API_KEY"
exec claude "$@"
```

No proxy, no overhead, just environment variables. API keys stored securely in `~/.clother/secrets.env` (chmod 600).

---

## FAQ

**Can I use multiple providers at once?**  
Open multiple terminals — each can use a different provider.

**Where are my API keys stored?**  
In `~/.clother/secrets.env` with `chmod 600` (only you can read it).

**What providers work with Clother?**  
Any provider with an Anthropic-compatible API. For non-compatible providers (OpenRouter, LiteLLM), use [claude-code-router](https://github.com/musistudio/claude-code-router) instead.

**Does this modify my Claude installation?**  
No. It only sets environment variables before launching `claude`.

---

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `claude: command not found` | `npm install -g @anthropic-ai/claude-code` |
| `clother: command not found` | Add `~/bin` to PATH (see installation) |
| `API key not set` | Run `clother config` |

---

## Platform Support

✅ macOS (zsh/bash) • ✅ Linux (zsh/bash) • ✅ Windows (WSL)

---

## Contributors

Thanks to everyone who helped improve Clother:

- [@darkokoa](https://github.com/darkokoa) — China endpoints (zai-cn, minimax-cn, ve)
- [@RawToast](https://github.com/RawToast) — Kimi Coding Plan endpoint fix

PRs welcome! 🙏

---

## License

MIT © [jolehuit](https://github.com/jolehuit)
