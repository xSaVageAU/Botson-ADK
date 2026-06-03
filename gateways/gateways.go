package gateways

import (
	"context"
	"log"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
)

type GatewayManager struct {
	agent          agent.Agent
	sessionService session.Service
	runner         *runner.Runner
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

// Start spawns the background listening loops for mock Telegram and Discord gateways.
func (gm *GatewayManager) Start(ctx context.Context) {
	log.Println("Starting background messaging gateways...")

	// Spawn mock Telegram listener
	go func() {
		log.Println("[Gateway] Telegram gateway active (mock polling started).")
		for {
			select {
			case <-gm.stopChan:
				log.Println("[Gateway] Telegram gateway listener stopped.")
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Spawn mock Discord listener
	go func() {
		log.Println("[Gateway] Discord gateway active (mock WebSocket connected).")
		for {
			select {
			case <-gm.stopChan:
				log.Println("[Gateway] Discord gateway listener stopped.")
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop terminates all background gateway polling loops.
func (gm *GatewayManager) Stop() {
	close(gm.stopChan)
}
