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

## Installation

```bash
# 1. Install Claude Code CLI
curl -fsSL https://claude.ai/install.sh | bash

# 2. Install Clother
curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/clother.sh | bash
```

## Quick Start

```bash
clother-native                          # Use your Claude Pro/Team subscription
clother-zai                             # Z.AI (GLM-5)
clother-ollama --model qwen3-coder      # Local with Ollama
clother config                          # Configure providers
```

## Providers

### Cloud

| Command | Provider | Model | API Key |
|---------|----------|-------|---------|
| `clother-native` | Anthropic | Claude | Your subscription |
| `clother-zai` | Z.AI | GLM-5 | [z.ai](https://z.ai) |
| `clother-minimax` | MiniMax | MiniMax-M2.5 | [minimax.io](https://minimax.io) |
| `clother-kimi` | Kimi | kimi-k2.5 | [kimi.com](https://kimi.com) |
| `clother-moonshot` | Moonshot AI | kimi-k2.5 | [moonshot.ai](https://moonshot.ai) |
| `clother-deepseek` | DeepSeek | deepseek-chat | [deepseek.com](https://platform.deepseek.com) |
| `clother-mimo` | Xiaomi MiMo | mimo-v2-flash | [xiaomimimo.com](https://platform.xiaomimimo.com) |

### OpenRouter (100+ Models)

Access Grok, Gemini, Mistral and more via [openrouter.ai](https://openrouter.ai).

```bash
clother config openrouter               # Set API key + add models
clother-or-kimi-k2                      # Use it
```

> For non-Claude models, use the `:exacto` variant (e.g. `moonshotai/kimi-k2-0905:exacto`).

### China Endpoints

| Command | Endpoint |
|---------|----------|
| `clother-zai-cn` | open.bigmodel.cn |
| `clother-minimax-cn` | api.minimaxi.com |
| `clother-ve` | ark.cn-beijing.volces.com |

### Local (No API Key)

| Command | Provider | Port | Setup |
|---------|----------|------|-------|
| `clother-ollama` | Ollama | 11434 | [ollama.com](https://ollama.com) |
| `clother-lmstudio` | LM Studio | 1234 | [lmstudio.ai](https://lmstudio.ai) |
| `clother-llamacpp` | llama.cpp | 8000 | [github.com/ggml-org/llama.cpp](https://github.com/ggml-org/llama.cpp) |

```bash
# Ollama
ollama pull qwen3-coder && ollama serve
clother-ollama --model qwen3-coder

# LM Studio
clother-lmstudio --model <model>

# llama.cpp
./llama-server --model model.gguf --port 8000 --jinja
clother-llamacpp --model <model>
```

### Custom

```bash
clother config                          # Choose "custom"
clother-myprovider                      # Ready
```

## Commands

| Command | Description |
|---------|-------------|
| `clother config [provider]` | Configure provider |
| `clother list` | List profiles |
| `clother test` | Test connectivity |
| `clother status` | Installation status |
| `clother uninstall` | Remove everything |

## Changing the Default Model

Each provider launcher comes with a default model (e.g. `glm-5` for Z.AI). You can override it in several ways:

```bash
# One-time: use --model flag
clother-zai --model glm-4.7

# Permanent: set ANTHROPIC_MODEL in your shell profile (.zshrc / .bashrc)
export ANTHROPIC_MODEL="glm-4.7"
clother-zai

# Or edit the launcher directly
nano ~/bin/clother-zai    # Replace the model name on all relevant lines
```

> **Tip**: The `--model` flag is passed directly to Claude CLI and takes priority over everything else.

## How It Works

Clother creates launcher scripts that set environment variables:

```bash
# clother-zai does:
export ANTHROPIC_BASE_URL="https://api.z.ai/api/anthropic"
export ANTHROPIC_AUTH_TOKEN="$ZAI_API_KEY"
exec claude "$@"
```

API keys stored in `~/.local/share/clother/secrets.env` (chmod 600).

## Install Directory

By default, Clother installs launchers to:
- **macOS**: `~/bin`
- **Linux**: `~/.local/bin` (XDG standard)

You can override this with `--bin-dir` or the `CLOTHER_BIN` environment variable:

```bash
# Using --bin-dir flag
curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/clother.sh | bash -s -- --bin-dir ~/.local/bin

# Using environment variable
export CLOTHER_BIN="$HOME/.local/bin"
curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/clother.sh | bash
```

Make sure the chosen directory is in your `PATH`.

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `claude: command not found` | Install Claude CLI first |
| `clother: command not found` | Add your bin directory to PATH (see [Install Directory](#install-directory)) |
| `API key not set` | Run `clother config` |

## VS Code Integration

To use Clother with the official **Claude Code** extension:

1. Open VS Code Settings (`Cmd+,` or `Ctrl+,`).
2. Search for **"Claude Process Wrapper"** (`claudeProcessWrapper`).
3. Set it to the **full path** of your chosen launcher:
   - macOS: `/Users/yourname/bin/clother-zai`
   - Linux: `/home/yourname/.local/bin/clother-zai`
4. Reload VS Code.

> **Note**: Requires Clother v2.6+ (which handles non-interactive shell output correctly).

## Platform Support

macOS (zsh/bash) • Linux (zsh/bash) • Windows (WSL)

## Contributors

- [@darkokoa](https://github.com/darkokoa) — China endpoints
- [@RawToast](https://github.com/RawToast) — Kimi endpoint fix
- [@sammcj](https://github.com/sammcj) — Security hardening
- [@luciano-fiandesio](https://github.com/luciano-fiandesio) — Install directory improvement (issue)

## License

MIT © [jolehuit](https://github.com/jolehuit)
