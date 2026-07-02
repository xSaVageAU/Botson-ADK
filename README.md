# Botson: Modular Go Agent Ecosystem

Botson is a highly modular, self-contained, and cross-platform AI coding agent ecosystem built with the `adk-go` SDK. It is designed to run natively on the host machine or within isolated environments (WSL/gVisor sandboxes).

The project is structured to generate **three distinct standalone applications** from a single codebase, keeping binaries focused, fast, and lightweight.

---

## 📂 Project Directory Structure

- `apps/` — Executable entry points for different runtimes:
  - [`botson/`](/apps/botson) — The full core CLI, TUI, Visual Web UI, and background daemon app.
  - [`botson-discord/`](/apps/botson-discord) — A lightweight standalone Discord gateway client.
  - [`botson-web/`](/apps/botson-web) — A standalone web chat application configured for OpenRouter with a sleek custom UI.
- `agent/` — Core LLM loops, execution orchestration, and agent factories.
- `gateways/` — External connection adapters (Discord, Telegram).
- `internal/` — Internal package libraries:
  - `config/` — Thread-safe, dynamic map-backed dot-notation configuration store.
  - `executor/` — Commands manager running commands either on Host OS or WSL/gVisor guest namespaces.
  - `tools/` — Modular, isolated capability sets exposed to the agent.
- `providers/` — Standardized LLM completions wrapper (Gemini, OpenRouter).
- `scripts/` — Automated build scripts.

---

## ⚙️ Dynamic Configuration Store

Configurations reside in `~/.botson-adk/config.json`. The configuration manager uses a thread-safe `map[string]any` structure allowing apps to only read/write fields they require:

- Base settings: `provider`, `instruction`
- Secrets (safely blocked from agent reads): `discord_token`, `<provider>.api_key`
- Feature toggles (cascading dependency resolver):
  - `features.sandboxing` (WSL / gVisor)
  - `features.services` (Background service supervisor)
  - `features.coder` (File search & modification tools)

---

## 🛠️ Cross-Compilation

We compile the entire suite concurrently for Windows, Linux, and macOS (AMD64 & ARM64 targets). Build outputs are placed in separate folders:

```bash
go run scripts/build.go
```

Outputs will be organized as follows:
- `build/botson/` -> contains `botson-*` binaries.
- `build/botson-discord/` -> contains `botson-discord-*` binaries.
- `build/botson-web/` -> contains `botson-web-*` binaries.

---

## 📚 Application Guides

Refer to the individual `README.md` files for deployment instructions:
- [Core Application Usage](/apps/botson/README.md)
- [Standalone Discord Client Usage](/apps/botson-discord/README.md)
- [Standalone Web UI Usage](/apps/botson-web/README.md)
