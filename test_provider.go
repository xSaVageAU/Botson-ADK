package main

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
	botsonAgent "botson/agent"
	"botson/providers"
	"botson/internal/config"
	"botson/internal/configtool"
	"botson/tools/time"
)

func main() {
	ctx := context.Background()

	// Initialize configuration
	mgr, err := config.NewManager("config.json")
	if err != nil {
		log.Fatalf("failed to create config manager: %v", err)
	}
	cfg := mgr.Get()

	sessSvc := session.InMemoryService()

	modelGetter := func() string {
		pCfg, _ := mgr.GetProvider(mgr.Get().Provider)
		if pCfg != nil {
			return pCfg.Model
		}
		return ""
	}
	apiKeyGetter := func() string {
		pCfg, _ := mgr.GetProvider(mgr.Get().Provider)
		if pCfg != nil {
			return pCfg.APIKey
		}
		return ""
	}

	// Create model
	m, err := providers.GetModel(ctx, cfg.Provider, modelGetter, apiKeyGetter)
	if err != nil {
		log.Fatalf("failed to create model: %v", err)
	}

	// Create tools
	readTool, _ := configtool.MakeReadConfigTool(mgr)
	writeTool, _ := configtool.MakeUpdateConfigTool(mgr)
	timeTool, _ := time.MakeGetTimeTool()
	toolsList := []tool.Tool{readTool, writeTool, timeTool}

	// Create agent
	ag, err := botsonAgent.CreateAgent(ctx, "botson", m, cfg.Instruction, toolsList)
	if err != nil {
		log.Fatalf("failed to create agent: %v", err)
	}

	// Create runner
	r, err := runner.New(runner.Config{
		AppName:           "botson",
		Agent:             ag,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		log.Fatalf("failed to create runner: %v", err)
	}

	// Run conversation turn
	fmt.Println("Running agent conversation turn...")
	eventsChan := r.Run(ctx, "botson", "test-session", &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			{Text: "what is your current model?"},
		},
	}, agent.RunConfig{
		StreamingMode: agent.StreamingModeNone,
	})

	for ev := range eventsChan {
		_ = ev // read events to drain channel and let runner finish
	}
}
