package plugin

import (
	"fmt"
	"sync"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/tool"
)

type runnableTool interface {
	Run(ctx agent.Context, args any) (map[string]any, error)
}

// NewSequentialToolPlugin creates an ADK plugin that serializes execution of all tools
// across concurrent goroutines spawned during a model invocation turn.
func NewSequentialToolPlugin() (*plugin.Plugin, error) {
	var toolMu sync.Mutex

	return plugin.New(plugin.Config{
		Name: "SequentialToolScheduler",
		BeforeToolCallback: func(ctx agent.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
			rt, ok := t.(runnableTool)
			if !ok {
				return nil, fmt.Errorf("tool %q is not runnable (cannot assert runnableTool)", t.Name())
			}

			// Acquire the sequential execution lock
			toolMu.Lock()
			defer toolMu.Unlock()

			// Manually run the tool synchronously inside the lock
			return rt.Run(ctx, args)
		},
	})
}
