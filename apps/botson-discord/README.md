# Botson Discord Client

This is a focused, lightweight standalone gateway client for Discord. It bypasses all Web UIs, launchers, and APIs, providing an optimized connection to your Discord bot.

---

## 🚀 Usage

### 1. Configure the Discord Token
Write your Discord credentials to the configuration:
```bash
# Set token
./botson config set discord_token YOUR_DISCORD_TOKEN
```

### 2. Run the Discord Client
Boot the client up:
```bash
./botson-discord
```
The bot will connect to the Discord gateway and process events in background loops.

### 3. Approve Pairing Codes
When a new user sends a DM to the bot, the bot will request pairing approval. Run this command on the host:
```bash
./botson-discord pairing approve discord <pairing-code>
```
