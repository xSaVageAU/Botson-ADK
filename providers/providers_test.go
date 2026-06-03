package providers

import (
	"context"
	"iter"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
	"botson/internal/commands"
)

func TestHasNewCommand(t *testing.T) {
	tests := []struct {
		name     string
		contents []*genai.Content
		want     bool
	}{
		{
			name:     "No content",
			contents: nil,
			want:     false,
		},
		{
			name: "Valid new command",
			contents: []*genai.Content{
				{
					Parts: []*genai.Part{
						{Text: "/new"},
					},
				},
			},
			want: true,
		},
		{
			name: "New command with leading whitespace",
			contents: []*genai.Content{
				{
					Parts: []*genai.Part{
						{Text: " /new"},
					},
				},
			},
			want: false,
		},
		{
			name: "Normal user text query",
			contents: []*genai.Content{
				{
					Parts: []*genai.Part{
						{Text: "hello world"},
					},
				},
			},
			want: false,
		},
		{
			name: "New command with trailing whitespace",
			contents: []*genai.Content{
				{
					Parts: []*genai.Part{
						{Text: "/new \n"},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasNewCommand(tt.contents)
			if got != tt.want {
				t.Errorf("hasNewCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

type mockLLM struct {
	model.LLM
	called bool
}

func (m *mockLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	m.called = true
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{}, nil)
	}
}

func TestSessionResetWrapper(t *testing.T) {
	inner := &mockLLM{}
	wrapper := &sessionResetWrapper{
		LLM: inner,
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Parts: []*genai.Part{
					{Text: "/new"},
				},
			},
		},
	}

	// 1. Without native commands context: should NOT call inner LLM (intercepted)
	inner.called = false
	seq := wrapper.GenerateContent(context.Background(), req, false)
	for range seq {
	}
	if inner.called {
		t.Error("Wrapper should have intercepted /new without native commands context")
	}

	// 2. With native commands context: should call inner LLM (not intercepted)
	inner.called = false
	ctx := commands.ContextWithNativeCommands(context.Background(), true)
	seq2 := wrapper.GenerateContent(ctx, req, false)
	for range seq2 {
	}
	if !inner.called {
		t.Error("Wrapper should have forwarded /new to inner LLM with native commands context")
	}
}

