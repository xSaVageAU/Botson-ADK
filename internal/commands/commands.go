package commands

import (
	"context"
	"fmt"
	"strings"
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

// Process checks if the query is a command (prefixed with '/') and executes it.
func Process(ctx context.Context, cmdCtx CommandContext, query string) (string, bool, error) {
	// If the gateway supports native commands, we do not parse text commands.
	if HasNativeCommands(ctx) {
		return "", false, nil
	}

	// A command must start exactly with "/" (no leading whitespace)
	if !strings.HasPrefix(query, "/") {
		return "", false, nil
	}

	query = strings.TrimSpace(query)

	parts := strings.SplitN(query[1:], " ", 2) // strip "/" prefix
	cmdName := parts[0]
	var args string
	if len(parts) > 1 {
		args = parts[1]
	}

	mu.RLock()
	cmd, exists := registry[cmdName]
	mu.RUnlock()

	if !exists {
		return "", false, nil
	}

	resp, err := cmd.Handler(ctx, cmdCtx, args)
	if err != nil {
		return "", true, err
	}
	return resp, true, nil
}

type contextKey int

const supportsNativeCommandsKey contextKey = iota

// ContextWithNativeCommands returns a context decorated with native command capability indicator.
func ContextWithNativeCommands(ctx context.Context, val bool) context.Context {
	return context.WithValue(ctx, supportsNativeCommandsKey, val)
}

// HasNativeCommands returns true if the context indicates that native commands are supported.
func HasNativeCommands(ctx context.Context) bool {
	val, ok := ctx.Value(supportsNativeCommandsKey).(bool)
	return ok && val
}

