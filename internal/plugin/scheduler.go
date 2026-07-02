package plugin

import (
	"fmt"
	"sync"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

type runnableTool interface {
	Run(ctx agent.Context, args any) (map[string]any, error)
}

type sessionQueue struct {
	mu            sync.Mutex
	nextIndex     int
	lastEventID   string
	functionCalls []*genai.FunctionCall
	notifyChs     map[int]chan struct{}
}

var (
	queuesMu sync.Mutex
	queues   = make(map[string]*sessionQueue)
)

func getSessionQueue(sessionID string) *sessionQueue {
	queuesMu.Lock()
	defer queuesMu.Unlock()
	q, exists := queues[sessionID]
	if !exists {
		q = &sessionQueue{
			notifyChs: make(map[int]chan struct{}),
		}
		queues[sessionID] = q
	}
	return q
}

func getFunctionCalls(content *genai.Content) []*genai.FunctionCall {
	if content == nil {
		return nil
	}
	var calls []*genai.FunctionCall
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			calls = append(calls, part.FunctionCall)
		}
	}
	return calls
}

// NewSequentialToolPlugin creates an ADK plugin that serializes execution of all tools
// across concurrent goroutines in the exact order they were returned by the LLM.
func NewSequentialToolPlugin() (*plugin.Plugin, error) {
	return plugin.New(plugin.Config{
		Name: "SequentialToolScheduler",

		// 1. Intercept the model response synchronously *before* tool dispatch to cache the function calls list
		AfterModelCallback: func(ctx agent.Context, resp *model.LLMResponse, err error) (*model.LLMResponse, error) {
			if err != nil || resp == nil || resp.Content == nil {
				return resp, err
			}
			calls := getFunctionCalls(resp.Content)
			if len(calls) > 0 {
				q := getSessionQueue(ctx.SessionID())
				q.mu.Lock()
				q.lastEventID = ctx.InvocationID()
				q.functionCalls = calls
				q.nextIndex = 0
				q.notifyChs = make(map[int]chan struct{})
				q.mu.Unlock()
			}
			return resp, err
		},

		// 2. Intercept concurrent tool runs and enforce strict index order using wait channels
		BeforeToolCallback: func(ctx agent.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
			rt, ok := t.(runnableTool)
			if !ok {
				return nil, fmt.Errorf("tool %q is not runnable", t.Name())
			}

			sess := ctx.Session()
			if sess == nil {
				return rt.Run(ctx, args)
			}

			q := getSessionQueue(ctx.SessionID())
			q.mu.Lock()

			var lastLLMID string
			var functionCalls []*genai.FunctionCall

			// First try to read from the cached turn batch
			if q.lastEventID == ctx.InvocationID() && len(q.functionCalls) > 0 {
				lastLLMID = q.lastEventID
				functionCalls = q.functionCalls
			} else {
				// Fallback to database lookup if not found in memory cache
				q.mu.Unlock()
				events := sess.Events()
				var lastLLMEvent *session.Event
				for i := events.Len() - 1; i >= 0; i-- {
					ev := events.At(i)
					if ev.LLMResponse.Content != nil {
						calls := getFunctionCalls(ev.LLMResponse.Content)
						if len(calls) > 0 {
							lastLLMEvent = ev
							functionCalls = calls
							break
						}
					}
				}
				if lastLLMEvent == nil {
					return rt.Run(ctx, args)
				}
				lastLLMID = lastLLMEvent.ID
				q.mu.Lock()

				// Reset queue index if we transitioned to a new event/batch in the fallback path
				if q.lastEventID != lastLLMID {
					q.lastEventID = lastLLMID
					q.nextIndex = 0
					q.notifyChs = make(map[int]chan struct{})
				}
			}

			// Find the index of the current tool call
			currCallID := ctx.FunctionCallID()
			callIndex := -1
			for idx, call := range functionCalls {
				if call.ID == currCallID {
					callIndex = idx
					break
				}
			}

			if callIndex == -1 {
				q.mu.Unlock()
				return rt.Run(ctx, args)
			}

			// If it's not our turn, we wait on the channel for our index
			var waitCh chan struct{}
			if q.nextIndex < callIndex {
				var exists bool
				waitCh, exists = q.notifyChs[callIndex]
				if !exists {
					waitCh = make(chan struct{})
					q.notifyChs[callIndex] = waitCh
				}
			}
			q.mu.Unlock()

			if waitCh != nil {
				select {
				case <-waitCh:
					// Woken up, our turn has arrived!
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}

			// Run the tool synchronously
			result, err := rt.Run(ctx, args)

			// Advance queue and wake up the next tool call in line
			q.mu.Lock()
			q.nextIndex++
			nextIdx := q.nextIndex
			if nextCh, exists := q.notifyChs[nextIdx]; exists {
				select {
				case <-nextCh: // already closed
				default:
					close(nextCh)
				}
			} else {
				// Create it closed so if the next tool starts later, it doesn't block
				ch := make(chan struct{})
				close(ch)
				q.notifyChs[nextIdx] = ch
			}
			q.mu.Unlock()

			return result, err
		},
	})
}
