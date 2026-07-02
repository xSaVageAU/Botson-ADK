# Botson Core App

This is the full core application package of the Botson agent. It uses the `adk-go` launcher framework to expose multiple frontends (TUI console, visual Web UI, HTTP REST API) and manages a background gateway supervisor.

---

## 🚀 Usage

### 1. Interactive Terminal (TUI)
Run the agent in your local terminal:
```bash
./botson
```

### 2. Spawning the Visual Web UI & API
Run the interactive dashboard web app:
```bash
./botson web
```
This boots up the internal ADK graphical chat frontend and REST API interface.

### 3. Background Gateways Daemon
Run the background gateway listener daemon:
```bash
./botson service start
```
This spawns background listeners for registered channels (Discord, Telegram).

---

## 🛠️ CLI Configuration Engine

Manage configurations via subcommands:

- **Get a value**:
  ```bash
  ./botson config get provider
  ./botson config get features.sandboxing
  ```
- **Set a value**:
  ```bash
  ./botson config set provider gemini
  ./botson config set gemini.model gemini-2.5-flash
  ./botson config set gemini.api_key YOUR_API_KEY
  ./botson config set features.sandboxing false
  ```
- **Approve Gateway Pairings**:
  ```bash
  ./botson pairing approve discord <pairing-code>
  ```
- **Provision WSL Sandbox**:
  ```bash
  ./botson wslsetup
  ```
