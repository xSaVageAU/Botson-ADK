package main

import (
	"context"
	"fmt"
	"iter"

	"google.golang.org/adk/v2/model"

	"botson/internal/config"
	"botson/providers"
	"botson/providers/openrouter"
)

// DynamicProviderModel is an LLM adapter that switches between OpenRouter and
// Gemini at runtime based on the current configuration.
type DynamicProviderModel struct {
	ctx             context.Context
	mgr             *config.Manager
	openrouterModel model.LLM
	geminiModel     model.LLM
}

// NewDynamicProviderModel constructs a DynamicProviderModel for the given
// configuration manager.
func NewDynamicProviderModel(ctx context.Context, mgr *config.Manager, envTypeGetter func() string) (*DynamicProviderModel, error) {
	orModel, err := providers.GetModel(ctx, "openrouter", func() string {
		pCfg, _ := mgr.GetProvider("openrouter")
		if pCfg != nil {
			return pCfg.Model
		}
		return ""
	}, func() string {
		pCfg, _ := mgr.GetProvider("openrouter")
		if pCfg != nil {
			return pCfg.APIKey
		}
		return ""
	}, envTypeGetter)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenRouter model: %w", err)
	}
	// Wire usage callback so model info handler can track context consumption.
	if orm, ok := orModel.(*openrouter.OpenRouterModel); ok {
		orm.OnUsage = UpdateUsage
	}

	gemModel, err := providers.GetModel(ctx, "gemini", func() string {
		pCfg, _ := mgr.GetProvider("gemini")
		if pCfg != nil {
			return pCfg.Model
		}
		return ""
	}, func() string {
		pCfg, _ := mgr.GetProvider("gemini")
		if pCfg != nil {
			return pCfg.APIKey
		}
		return ""
	}, envTypeGetter)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini model: %w", err)
	}

	return &DynamicProviderModel{
		ctx:             ctx,
		mgr:             mgr,
		openrouterModel: orModel,
		geminiModel:     gemModel,
	}, nil
}

// Name returns the underlying model name for the currently active provider.
func (dm *DynamicProviderModel) Name() string {
	provider := dm.mgr.Get().Provider
	if provider == "gemini" {
		return dm.geminiModel.Name()
	}
	return dm.openrouterModel.Name()
}

// GenerateContent dispatches the request to the currently active provider.
func (dm *DynamicProviderModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	provider := dm.mgr.Get().Provider
	if provider == "gemini" {
		return dm.geminiModel.GenerateContent(ctx, req, stream)
	}
	return dm.openrouterModel.GenerateContent(ctx, req, stream)
}
