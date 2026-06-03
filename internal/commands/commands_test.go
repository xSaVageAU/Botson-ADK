package commands

import (
	"context"
	"testing"
)

func TestProcessCommands(t *testing.T) {
	ctx := context.Background()
	cmdCtx := CommandContext{
		SessionKey: "test:channel",
	}

	tests := []struct {
		name        string
		query       string
		wantHandled bool
	}{
		{
			name:        "Valid command without args",
			query:       "/new",
			wantHandled: true,
		},
		{
			name:        "Command with leading whitespace",
			query:       " /new",
			wantHandled: false,
		},
		{
			name:        "Normal text message",
			query:       "hello world",
			wantHandled: false,
		},
		{
			name:        "Command prefix only",
			query:       "/",
			wantHandled: false,
		},
		{
			name:        "Unknown command",
			query:       "/unknowncommand",
			wantHandled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, handled, err := Process(ctx, cmdCtx, tt.query)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if handled != tt.wantHandled {
				t.Errorf("Process(%q) handled = %v, want %v", tt.query, handled, tt.wantHandled)
			}
		})
	}
}

func TestProcessCommandsWithNativeCommandsContext(t *testing.T) {
	ctx := ContextWithNativeCommands(context.Background(), true)
	cmdCtx := CommandContext{
		SessionKey: "test:channel",
	}

	_, handled, err := Process(ctx, cmdCtx, "/new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Error("Process() should not handle any text commands when context indicates native commands are supported")
	}
}

