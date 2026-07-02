# Botson Web Client

This is a focused standalone Web client for Botson. It runs a lightweight HTTP server serving a custom, glassmorphic dark-mode chat UI configured specifically to use **OpenRouter**.

---

## 🚀 Usage

### 1. Configure OpenRouter Credentials
Ensure your OpenRouter API Key and model are configured on the host machine:
```bash
./botson config set openrouter.api_key YOUR_OPENROUTER_KEY
./botson config set openrouter.model openrouter/owl-alpha
```

### 2. Run the Web Server
Launch the server:
```bash
./botson-web
```
The server will start listening on port `:8080`.

### 3. Open the Interface
Navigate to your web browser:
```
http://localhost:8080
```
This serves a gorgeous dark-mode chat logs window with pulsing typing bubbles and markdown styling.
