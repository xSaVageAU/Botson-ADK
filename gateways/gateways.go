package gateways

import (
	"context"
	"log"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"botson/internal/commands"
)

// Gateway defines the lifecycle of a message gateway (e.g. Discord, Telegram).
type Gateway interface {
	Name() string
	Start(ctx context.Context, runFn func(ctx context.Context, sessionID string, query string) (string, error)) error
	Stop() error
}

type GatewayManager struct {
	agent          agent.Agent
	sessionService session.Service
	runner         *runner.Runner
	gateways       []Gateway
	stopChan       chan struct{}
}

// NewGatewayManager initializes the gateway runner with the active agent and session service.
func NewGatewayManager(ag agent.Agent, sessSvc session.Service) (*GatewayManager, error) {
	r, err := runner.New(runner.Config{
		AppName:           ag.Name(),
		Agent:             ag,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, err
	}
	return &GatewayManager{
		agent:          ag,
		sessionService: sessSvc,
		runner:         r,
		stopChan:       make(chan struct{}),
	}, nil
}

// Register registers a gateway implementation.
func (gm *GatewayManager) Register(g Gateway) {
	gm.gateways = append(gm.gateways, g)
}

// Start spawns the background listening loops for all registered gateways.
func (gm *GatewayManager) Start(ctx context.Context) {
	log.Println("Starting background messaging gateways...")

	runFn := func(ctx context.Context, sessionKey string, query string) (string, error) {
		// 1. Process commands first
		cmdCtx := commands.CommandContext{
			SessionKey: sessionKey,
		}
		if resp, handled, err := commands.Process(ctx, cmdCtx, query); handled {
			return resp, err
		}

		// 2. Resolve active session ID
		activeSessionID := commands.GetActiveSession(sessionKey)

		// 3. Run LLM query
		events := gm.runner.Run(ctx, gm.agent.Name(), activeSessionID, &genai.Content{
			Role: "user",
			Parts: []*genai.Part{
				{Text: query},
			},
		}, agent.RunConfig{
			StreamingMode: agent.StreamingModeNone,
		})

		var result strings.Builder
		for ev, err := range events {
			if err != nil {
				return "", err
			}
			if ev.Content != nil {
				for _, part := range ev.Content.Parts {
					if part.Text != "" {
						result.WriteString(part.Text)
					}
				}
			}
		}
		return result.String(), nil
	}

	for _, g := range gm.gateways {
		g := g
		go func() {
			log.Printf("Starting gateway: %s", g.Name())
			if err := g.Start(ctx, runFn); err != nil {
				log.Printf("Error running gateway %s: %v", g.Name(), err)
			}
		}()
	}
}

// Stop terminates all background gateways.
func (gm *GatewayManager) Stop() {
	close(gm.stopChan)
	for _, g := range gm.gateways {
		log.Printf("Stopping gateway: %s", g.Name())
		if err := g.Stop(); err != nil {
			log.Printf("Error stopping gateway %s: %v", g.Name(), err)
		}
	}
}
