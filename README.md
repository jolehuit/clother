<div align="center">
  <img src="docs/logo.png" alt="Clother logo" width="220" />
  <h1>Clother</h1>
  <p><strong>One CLI to switch between Claude Code providers instantly.</strong></p>
  <p>
    <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="MIT License" /></a>
    <a href="https://go.dev/"><img src="https://img.shields.io/badge/Language-Go-00ADD8.svg" alt="Go" /></a>
    <a href="#platform-support"><img src="https://img.shields.io/badge/Platform-macOS%20%7C%20Linux-lightgrey.svg" alt="Platform macOS and Linux" /></a>
    <a href="https://github.com/jolehuit/clother/stargazers"><img src="https://img.shields.io/github/stars/jolehuit/clother?style=social" alt="GitHub stars" /></a>
  </p>
</div>

<br/>

<div align="center">
  <img src="docs/demo-fast.gif" alt="Clother terminal demo" width="900" />
</div>

## Why Clother?

Switching Claude Code providers usually means changing env vars, endpoints, models, and launcher scripts by hand.
Clother gives you one install and one command pattern across Claude, Z.AI, Kimi, Alibaba, OpenRouter, local backends, China endpoints, and many other Anthropic-compatible providers.

## Table of Contents

- [Installation](#installation)
- [Core Usage](#core-usage)
  - [Benchmarking](#benchmarking)
- [Provider Reference](#provider-reference)
- [Troubleshooting](#troubleshooting)
- [VS Code Integration](#vs-code-integration)
- [Platform Support](#platform-support)
- [Under the Hood](#under-the-hood)
- [Contributors](#contributors)
- [Star History](#star-history)
- [License](#license)

## Installation

### Homebrew (macOS recommended)

```bash
# 1. Install Claude Code CLI
curl -fsSL https://claude.ai/install.sh | bash

# 2. Install Clother via tap
brew tap jolehuit/tap
brew install clother

# 3. Start using it — all launchers are ready immediately
clother-native                          # Use your Claude Pro/Max/Team subscription
clother-zai                             # Z.AI (GLM-5.3)
clother-zai --yolo                      # Skip permission prompts
clother-kimi                            # Kimi (K3)
clother config                          # Configure providers
```

All `clother-*` provider launchers are installed directly into `$(brew --prefix)/bin` by the formula — no extra setup needed. `brew upgrade clother` keeps everything up to date.

**Update:**
```bash
clother update          # routes to brew upgrade under Homebrew
# or equivalently:
brew upgrade clother
```

### curl (macOS / Linux)

```bash
# 1. Install Claude Code CLI
curl -fsSL https://claude.ai/install.sh | bash

# 2. Install Clother
curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/scripts/install.sh | bash

# 3. Start using it
clother-native                          # Use your Claude Pro/Max/Team subscription
clother-zai                             # Z.AI (GLM-5.3)
clother-zai --yolo                      # Skip permission prompts
clother-kimi                            # Kimi (K3)
clother-ollama --model qwen3-coder      # Local with Ollama
clother config                          # Configure providers
```

**Update:**
```bash
clother update          # downloads and installs latest release
```

This installs:
- `clother`
- `clother-*` provider launchers
- resume compatibility for `claude --resume ...`

### Install Options

By default, Clother installs launchers to:
- the same directory as your existing `claude` binary, when `claude` is already on `PATH`
- otherwise **macOS**: `~/bin`
- otherwise **Linux**: `~/.local/bin` (XDG standard)

If the chosen bin directory is not on `PATH`, `clother install` prints a warning with the exact directory to add.

You can override this with `--bin-dir` or the `CLOTHER_BIN` environment variable:

```bash
# Using --bin-dir flag
curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/scripts/install.sh | bash -s -- --bin-dir ~/.local/bin

# Using environment variable
export CLOTHER_BIN="$HOME/.local/bin"
curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/scripts/install.sh | bash
```

Clother keeps `claude --resume ...` working with Clother features after install.

## Core Usage

### Commands

| Command | Description |
|---------|-------------|
| `clother config [provider]` | Configure provider |
| `clother list` | List profiles |
| `clother info <provider>` | Show provider details |
| `clother test` | Test connectivity |
| `clother bench [provider...] [--prompt "..."]` | Benchmark provider latency |
| `clother status` | Installation status |
| `clother install` | Install/update Clother (create/refresh symlinks) |
| `clother update` | Update to latest version |
| `clother uninstall` | Remove everything |

### Update

```bash
clother update
```

Routes to `brew upgrade clother` under Homebrew, or downloads the latest release for curl installs. Also refreshes provider symlinks.

### Changing the Default Model

Each provider launcher comes with a default model (for example `glm-5.3[1m]` for Z.AI) and maps Claude Code's `opus`, `sonnet`, `haiku` and `fable` tiers to the provider's own models. You can override it in two ways:

```bash
# One-time: pass --model through to Claude CLI
clother-zai --model glm-5.3-flash       # a concrete model ID pins every tier
clother-zai --model haiku               # a tier alias resolves through the mapping

# Permanent: configure the provider and pick a different default
clother config zai
```

Use `clother info <provider>` to inspect the resolved model and tiers.

A `[1m]` suffix (e.g. `glm-5.3[1m]`) tells Claude Code the model has a 1M-token
context window; Claude Code strips it before sending the model ID. Without it,
Claude Code compacts sessions on models it does not know at 200K tokens. The
catalog uses the suffix wherever the provider documents it.

### Benchmarking

Compare latency across all configured providers at once:

```bash
clother bench                              # all configured providers
clother bench zai kimi                     # specific providers only
clother bench --prompt "Write a haiku"     # custom prompt
```

Output shows **TTFT** (time to first token) and total response time, sorted fastest first:

```
  Provider           Model                      TTFT    Total   Preview
  ──────────────────────────────────────────────────────────────────────────────
  kimi               k3-256k                    180ms    0.9s   "Hello!"
  zai                glm-5.3                    312ms    1.2s   "Hello!"
  deepseek           deepseek-flash             890ms    3.1s   "Hello!"
```

Only providers with a configured API key are included. Local providers are skipped.

### Resume

Clother keeps the resume command printed by Claude Code working across providers.

After a provider-launched session, Clother also prints a provider-aware reopen
command such as:

```bash
clother-kimi --resume <session-id>
```

When resuming a non-Claude session into native Claude, Clother temporarily
sanitizes incompatible non-Claude thinking blocks for the duration of that
single launch, then restores the original session file afterwards.

## Provider Reference

### Cloud

| Command | Provider | Default model | API Key |
|---------|----------|---------------|---------|
| `clother-native` | Anthropic | Claude | Your subscription |
| `clother-zai` | Z.AI GLM Coding Plan | `glm-5.3[1m]` (haiku: `glm-5.3-flash[1m]`) | [z.ai](https://z.ai) |
| `clother-minimax` | MiniMax Token Plan | `MiniMax-M3[1m]` | [minimax.io](https://minimax.io) |
| `clother-kimi` | Kimi Code subscription | `k3-256k` | [kimi.com/code](https://www.kimi.com/code) |
| `clother-moonshot` | Kimi Open Platform (pay-as-you-go) | `kimi-k3[1m]` | [platform.kimi.ai](https://platform.kimi.ai) |
| `clother-deepseek` | DeepSeek | `deepseek-flash[1m]` (V4.1 Flash) | [deepseek.com](https://platform.deepseek.com) |
| `clother-mimo` | Xiaomi MiMo | `mimo-v2.6-pro[1m]` | [mimo.mi.com](https://mimo.mi.com) |
| `clother-alibaba` | Alibaba Coding Plan (Singapore) | `qwen3.7-plus` | [modelstudio](https://modelstudio.console.alibabacloud.com) |
| `clother-alibaba-token-plan` | Alibaba Token Plan (Singapore) | `auto` (opus: `qwen3.8-max`) | [modelstudio](https://modelstudio.console.alibabacloud.com) |

A Kimi Code key and a Kimi Open Platform key are not interchangeable: they are
two products with two endpoints. `clother-kimi` sends its key as
`ANTHROPIC_API_KEY`, as Kimi documents, so Claude Code asks once whether to use
it: answer yes. Every other provider receives its key as `ANTHROPIC_AUTH_TOKEN`.

### OpenRouter (100+ Models)

```bash
clother config openrouter               # Set API key + add models
# Example: alias moonshotai/kimi-k3[1m] as kimi-k3
clother-or kimi-k3                      # Works on every install
clother-or-kimi-k3                      # Per-alias shortcut (curl installs)
```

Append `[1m]` to the model ID of a model with a 1M-token context window, as in
the example: Claude Code strips it before the request and otherwise compacts
the session at 200K tokens.

`clother-or <alias>` works on every install. curl installs additionally get a
`clother-or-<alias>` symlink per alias when you run `clother config`; Homebrew
installs skip per-alias symlinks (the formula owns its bin directory), so use
the `clother-or <alias>` form there.

> **Tip**: Find model IDs on [openrouter.ai/models](https://openrouter.ai/models) — click the copy icon next to any model name.

> OpenRouter only guarantees its Anthropic-compatible endpoint for Anthropic models; other models usually work but are best effort. If tool calling misbehaves, try the `:exacto` variant (e.g. `moonshotai/kimi-k3:exacto`).

### China Endpoints

| Command | Provider | Endpoint |
|---------|----------|----------|
| `clother-zai-cn` | Z.AI China (BigModel) | open.bigmodel.cn |
| `clother-minimax-cn` | MiniMax China | api.minimaxi.com |
| `clother-kimi-cn` | Kimi Code China | api.kimi.com/coding |
| `clother-moonshot-cn` | Kimi Open Platform China | api.moonshot.cn |
| `clother-ve` | VolcEngine Ark Coding Plan | ark.cn-beijing.volces.com |
| `clother-alibaba-cn` | Alibaba Coding Plan China | coding.dashscope.aliyuncs.com |
| `clother-alibaba-token-plan-cn` | Alibaba Token Plan China | token-plan.cn-beijing.maas.aliyuncs.com |

Each China endpoint takes its own key: a key from the international platform
returns 401 there, and the reverse.

`clother-ve` defaults to `ark-code-latest`, the model selected in the Ark
console, because VolcEngine retires plan models every few weeks.

### Local (No API Key)

| Command | Provider | Port | Setup |
|---------|----------|------|-------|
| `clother-ollama` | Ollama | 11434 | [ollama.com](https://ollama.com) |
| `clother-lmstudio` | LM Studio | 1234 | [lmstudio.ai](https://lmstudio.ai) |
| `clother-llamacpp` | llama.cpp | 8000 | [github.com/ggml-org/llama.cpp](https://github.com/ggml-org/llama.cpp) |

```bash
# Ollama (set the context length to 64K or more)
ollama pull qwen3-coder && ollama serve
clother-ollama --model qwen3-coder

# LM Studio
clother-lmstudio --model <model>

# llama.cpp
./llama-server --model model.gguf --port 8000 --jinja
clother-llamacpp --model <model>
```

#### Remote servers

Local launchers default to `localhost`, but the backend can run on another
machine. Point a launcher at a remote base URL with `clother config`:

```bash
clother config lmstudio
# Base URL [http://localhost:1234]: http://192.168.123.123:1234
clother-lmstudio --model <model>
```

Works the same for `ollama` and `llamacpp`. Enter the default localhost URL
again to switch back.

If LM Studio runs with **Require Authentication**, `clother config lmstudio`
also stores its API token (in `secrets.env`, never in `config.json`); leave it
empty for an open server, or enter `-` to remove a stored token.

#### Default model and tiers

`clother config <ollama|lmstudio|llamacpp>` also takes an optional default
model and the backend model each Claude tier resolves to, so that
`--model opus`, `--model sonnet`, `--model haiku` and `/model` pick the models
you want. An empty tier falls back to the default model; `-` clears a value.

```bash
clother config lmstudio
# Default model (optional): qwen3.8-27b-mtp
# Opus model: qwen3.8-27b-mtp
# Sonnet model: qwopus3.6-27b-v2-mtp
# Haiku model: qwen3.6-35b-a3b-mtp
clother-lmstudio --model sonnet
```

### Custom

Any Anthropic-compatible endpoint:

```bash
clother config custom                   # e.g. name it "myprovider"
clother-custom myprovider               # Works on every install
clother-myprovider                      # Per-provider shortcut (curl installs)
```

As with OpenRouter aliases, the per-provider `clother-<name>` symlink is only
created on curl installs — under Homebrew, use `clother-custom <name>`.

Custom providers take the same optional opus / sonnet / haiku mapping as the
local backends.

### Alibaba Coding Plan Models

`clother-alibaba` (Singapore) and `clother-alibaba-cn` (China) read the same
`ALIBABA_API_KEY`, but Alibaba binds keys to their region: configure the one
that matches your subscription. Alibaba has no US Coding Plan endpoint, so
`clother-alibaba-us` was removed. The Coding Plan takes its own `sk-sp-...`
key and enforces an exact-string model allowlist; the currently supported IDs
are:

| Model |
|-------|
| `qwen3.7-plus` (default) |
| `qwen3.6-plus` |
| `qwen3.5-plus` |
| `kimi-k2.5` |
| `glm-5` |
| `MiniMax-M2.5` |
| `qwen3-coder-next` |
| `qwen3-coder-plus` |
| `qwen3-max-2026-01-23` |
| `glm-4.7` |

Switch models with `--model`:

```bash
clother-alibaba --model kimi-k2.5
clother-alibaba --model glm-5
clother-alibaba-cn --model qwen3-coder-next
```

The **Token Plan** is a separate subscription with a separate key
(`clother-alibaba-token-plan`, `clother-alibaba-token-plan-cn`). It follows
Alibaba's Claude Code guide: `auto` as the main model, `qwen3.8-max` for opus,
`qwen3.8-flash` for sonnet and `qwen3.6-flash` for haiku. Other plan models:
`qwen3.7-max`, `qwen3.7-plus`, `deepseek-v4.1-flash`, `deepseek-v4-pro`,
`glm-5.3`, `glm-5.2`.

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `claude: command not found` | Install Claude CLI first |
| `clother: command not found` | Run `clother status` to see the installed bin dir, then add that directory to `PATH` and restart your shell |
| `claude --resume ...` does not behave like Clother | Restart your shell, then run `clother install` again |
| `--yolo` is not recognized | Restart your shell, then run `clother install` again |
| `API key not set` | Run `clother config` |
| `clother-kimi` asks "Do you want to use this API key?" | Answer yes: Kimi Code takes its key as `ANTHROPIC_API_KEY` |
| `clother-alibaba-us: alibaba-us was removed` | Use `clother-alibaba`; Alibaba has no US Coding Plan endpoint |

## VS Code Integration

Clother works with the official **Claude Code** extension.
Use Claude Code extension `2.6+`.

To configure it:

1. Open VS Code Settings (`Cmd+,` or `Ctrl+,`).
2. Search for **"Claude Process Wrapper"** (`claudeProcessWrapper`).
3. Set it to the **full path** of your chosen launcher:
   - macOS: `/Users/yourname/bin/clother-zai`
   - Linux: `/home/yourname/.local/bin/clother-zai`
4. Reload VS Code.

> **Note**: Requires Clother v2.6+ (which handles non-interactive shell output correctly).

## Platform Support

macOS (zsh/bash) • Linux (zsh/bash) • Windows (WSL)

## Under the Hood

### How It Works

Clother is a single Go binary. The installer downloads the release artifact,
installs `clother` into your bin directory, then creates:
- `clother-*` symlinks for providers
- a `claude` shim symlink for resume compatibility

At runtime, the binary resolves the selected profile from its own invocation
name, loads config and secrets, sets the required Anthropic-compatible
environment variables, then launches the real Claude binary outside the Clother
bin directory.

Example for `clother-zai`:

```bash
export ANTHROPIC_BASE_URL="https://api.z.ai/api/anthropic"
export ANTHROPIC_AUTH_TOKEN="$ZAI_API_KEY"
export ANTHROPIC_MODEL="glm-5.3[1m]"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="glm-5.3-flash[1m]"   # and the other tiers
exec /path/to/the/real/claude "$@"
```

API keys stored in `~/.local/share/clother/secrets.env` (chmod 600).

### Your Anthropic credentials stay out of third-party sessions

Every non-Anthropic launcher runs Claude Code with a temporary
`CLAUDE_CONFIG_DIR` that mirrors your `~/.claude` without any Anthropic
credential: the Console key saved by `/login` (`primaryApiKey`), the claude.ai
OAuth token in `.credentials.json`, an `apiKeyHelper` script and any stale
`ANTHROPIC_*` value in `settings.json`. Claude Code would otherwise send your
Anthropic key as `x-api-key` to the provider, next to the provider's own token.
`~/.claude.json` and `.credentials.json` are sanitized copies; what the session
changes in them (project history, onboarding, MCP server tokens) is merged back
into your files when the session ends, your Anthropic credentials untouched.

`--yolo` is accepted by Clother launchers and by the Clother `claude` shim as
shorthand for `--dangerously-skip-permissions`.

### Local Release Testing

Test the binary installer locally against a local directory or server:

```bash
CLOTHER_RELEASE_BASE_URL=http://127.0.0.1:8000 \
  ./scripts/install.sh install
```

## Contributors

- [@darkokoa](https://github.com/darkokoa) — China endpoints
- [@RawToast](https://github.com/RawToast) — Kimi endpoint fix
- [@sammcj](https://github.com/sammcj) — Security hardening
- [@aprakasa](https://github.com/aprakasa) — Linux compatibility fixes in `load_secrets()`
- [@luciano-fiandesio](https://github.com/luciano-fiandesio) — Install directory improvement (issue)
- [@canberksinangil](https://github.com/canberksinangil) — Config overlay fix, GLM-5.2 default
- [@yasaricli](https://github.com/yasaricli) — `clother bench` command, GLM-5.1 support
- [@jeliseocd](https://github.com/jeliseocd) — Config overlay collision report and diagnosis, Anthropic key leak report and diagnosis (#40)
- [@Eponeshnikov](https://github.com/Eponeshnikov) — LM Studio token authentication and per-tier model mapping proposals (#37, #38)
- [@CurtisASmith](https://github.com/CurtisASmith) — Recovery from a dangling `claude-real` after Claude Code updates (#29)

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=jolehuit/clother&type=Date)](https://www.star-history.com/#jolehuit/clother&Date)

## License

MIT © [jolehuit](https://github.com/jolehuit)
