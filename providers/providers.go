package providers

import (
	"context"
	"fmt"
	"iter"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
	"botson/internal/commands"
	"botson/providers/gemini"
	"botson/providers/openrouter"
)

// sessionResetWrapper wraps a model.LLM and intercepts "/new" to clear the session service.
type sessionResetWrapper struct {
	model.LLM
	sessionService session.Service
}

func (w *sessionResetWrapper) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	if !commands.HasNativeCommands(ctx) && hasNewCommand(req.Contents) {
		return func(yield func(*model.LLMResponse, error) bool) {
			if w.sessionService != nil {
				// List and delete all sessions to reset context
				res, err := w.sessionService.List(ctx, &session.ListRequest{})
				if err == nil && res != nil {
					for _, s := range res.Sessions {
						_ = w.sessionService.Delete(ctx, &session.DeleteRequest{
							AppName:   s.AppName(),
							UserID:    s.UserID(),
							SessionID: s.ID(),
						})
					}
				}
			}
			yield(&model.LLMResponse{
				Content: &genai.Content{
					Role: "model",
					Parts: []*genai.Part{
						{Text: "\n🔄 Session reset! All in-memory chat context has been cleared.\n"},
					},
				},
				Partial:      false,
				TurnComplete: true,
			}, nil)
		}
	}
	return w.LLM.GenerateContent(ctx, req, stream)
}

func hasNewCommand(contents []*genai.Content) bool {
	if len(contents) == 0 {
		return false
	}
	last := contents[len(contents)-1]
	for _, part := range last.Parts {
		// A command must start exactly with "/" (no leading whitespace)
		if strings.HasPrefix(part.Text, "/") && strings.TrimSpace(part.Text) == "/new" {
			return true
		}
	}
	return false
}

// GetModel retrieves an implementation of model.LLM wrapped with session reset capabilities.
func GetModel(ctx context.Context, provider string, modelNameGetter func() string, apiKeyGetter func() string, sessionService session.Service) (model.LLM, error) {
	var inner model.LLM
	var err error

	switch provider {
	case "openrouter":
		inner, err = openrouter.NewModel(ctx, modelNameGetter, apiKeyGetter)
	case "gemini":
		inner, err = gemini.NewModel(ctx, modelNameGetter, apiKeyGetter)
	default:
		return nil, fmt.Errorf("unknown LLM provider: %s", provider)
	}

	if err != nil {
		return nil, err
	}

	return &sessionResetWrapper{
		LLM:            inner,
		sessionService: sessionService,
	}, nil
}
