package commands

import (
	"context"
	"fmt"
	"sync"
	"time"
)

var (
	mu             sync.RWMutex
	activeSessions = make(map[string]string) // maps key (e.g. "discord:<channelID>") to active session ID
)

// GetActiveSession returns the current active session ID for a given key.
// If none exists, it initializes it to the key itself.
func GetActiveSession(key string) string {
	mu.RLock()
	sessID, exists := activeSessions[key]
	mu.RUnlock()

	if exists {
		return sessID
	}

	mu.Lock()
	if sessID, exists = activeSessions[key]; !exists {
		sessID = key
		activeSessions[key] = sessID
	}
	mu.Unlock()

	return sessID
}

// ResetActiveSession generates a new active session ID for a given key.
func ResetActiveSession(key string) string {
	newSessID := fmt.Sprintf("%s-%d", key, time.Now().UnixNano())
	mu.Lock()
	activeSessions[key] = newSessID
	mu.Unlock()
	return newSessID
}

// CommandContext holds the environment for a command execution.
type CommandContext struct {
	SessionKey string
}

// Handler defines a function to process a command.
type Handler func(ctx context.Context, cmdCtx CommandContext, args string) (string, error)

// Command represents a metadata definition for a command.
type Command struct {
	Name        string
	Description string
	Handler     Handler
}

var registry = map[string]Command{
	"new": {
		Name:        "new",
		Description: "Start a fresh chat session in this channel",
		Handler:     handleNew,
	},
}

func handleNew(ctx context.Context, cmdCtx CommandContext, args string) (string, error) {
	ResetActiveSession(cmdCtx.SessionKey)
	return "🔄 Started a new chat session! The previous session history has been archived.", nil
}

// GetCommands returns a slice of all registered Command definitions.
func GetCommands() []Command {
	mu.RLock()
	defer mu.RUnlock()
	cmds := make([]Command, 0, len(registry))
	for _, cmd := range registry {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// Execute direct executes a registered command handler.
func Execute(ctx context.Context, cmdName string, cmdCtx CommandContext, args string) (string, error) {
	mu.RLock()
	cmd, exists := registry[cmdName]
	mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("command not found: %s", cmdName)
	}

	return cmd.Handler(ctx, cmdCtx, args)
}

