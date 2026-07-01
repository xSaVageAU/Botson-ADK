package gemini

import (
	"context"
	"fmt"
	"iter"
	"sync"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
)

type GeminiModelWrapper struct {
	mu              sync.RWMutex
	ctx             context.Context
	modelNameGetter func() string
	apiKeyGetter    func() string
	cachedModel     model.LLM
	cachedModelName string
	cachedAPIKey    string
}

// NewModel creates a new Gemini implementation wrapper supporting dynamic configuration hot-reloading.
func NewModel(ctx context.Context, modelNameGetter func() string, apiKeyGetter func() string) (model.LLM, error) {
	return &GeminiModelWrapper{
		ctx:             ctx,
		modelNameGetter: modelNameGetter,
		apiKeyGetter:    apiKeyGetter,
	}, nil
}

func (m *GeminiModelWrapper) getInnerModel() (model.LLM, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	modelName := m.modelNameGetter()
	apiKey := m.apiKeyGetter()

	if modelName == "" {
		modelName = "gemini-2.5-flash"
	}

	if m.cachedModel != nil && m.cachedModelName == modelName && m.cachedAPIKey == apiKey {
		return m.cachedModel, nil
	}

	cfg := &genai.ClientConfig{}
	if apiKey != "" {
		cfg.APIKey = apiKey
	}

	inner, err := gemini.NewModel(m.ctx, modelName, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini model: %w", err)
	}

	m.cachedModel = inner
	m.cachedModelName = modelName
	m.cachedAPIKey = apiKey
	return inner, nil
}

func (m *GeminiModelWrapper) Name() string {
	inner, err := m.getInnerModel()
	if err != nil {
		return m.modelNameGetter()
	}
	return inner.Name()
}

func (m *GeminiModelWrapper) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	inner, err := m.getInnerModel()
	if err != nil {
		return func(yield func(*model.LLMResponse, error) bool) {
			yield(nil, err)
		}
	}
	return inner.GenerateContent(ctx, req, stream)
}
