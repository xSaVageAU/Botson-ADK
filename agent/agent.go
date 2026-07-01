package agent

import (
	"context"
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
)

// CreateAgent creates a new LLM agent using the provided model implementation and tools.
func CreateAgent(ctx context.Context, name string, m model.LLM, instruction string, tools []tool.Tool) (agent.Agent, error) {
	a, err := llmagent.New(llmagent.Config{
		Name:        name,
		Model:       m,
		Instruction: instruction,
		Description: "Botson AI Agent",
		Tools:       tools,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}
	return a, nil
}
