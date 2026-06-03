package providers

import (
	"context"
	"fmt"

	"google.golang.org/adk/model"
	"botson/providers/gemini"
	"botson/providers/openrouter"
)

// GetModel retrieves an implementation of model.LLM.
func GetModel(ctx context.Context, provider string, modelNameGetter func() string, apiKeyGetter func() string) (model.LLM, error) {
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

	return inner, nil
}
