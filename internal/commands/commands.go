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

var registry = map[string]Handler{
	"/new": handleNew,
}

func handleNew(ctx context.Context, cmdCtx CommandContext, args string) (string, error) {
	ResetActiveSession(cmdCtx.SessionKey)
	return "🔄 Started a new chat session! The previous session history has been archived.", nil
}

// Process checks if the query is a command and executes it.
func Process(ctx context.Context, cmdCtx CommandContext, query string) (string, bool, error) {
	query = strings.TrimSpace(query)
	if !strings.HasPrefix(query, "/") {
		return "", false, nil
	}

	parts := strings.SplitN(query, " ", 2)
	cmd := parts[0]
	var args string
	if len(parts) > 1 {
		args = parts[1]
	}

	handler, exists := registry[cmd]
	if !exists {
		return "", false, nil
	}

	resp, err := handler(ctx, cmdCtx, args)
	if err != nil {
		return "", true, err
	}
	return resp, true, nil
}
