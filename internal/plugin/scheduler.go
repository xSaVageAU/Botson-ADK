package plugin

import (
	"fmt"
	"sync"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

type runnableTool interface {
	Run(ctx agent.Context, args any) (map[string]any, error)
}

type sessionQueue struct {
	mu          sync.Mutex
	nextIndex   int
	lastEventID string
	notifyChs   map[int]chan struct{}
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
		BeforeToolCallback: func(ctx agent.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
			rt, ok := t.(runnableTool)
			if !ok {
				return nil, fmt.Errorf("tool %q is not runnable", t.Name())
			}

			sess := ctx.Session()
			if sess == nil {
				// Fallback if session is missing
				return rt.Run(ctx, args)
			}

			// 1. Find the last event containing function calls to resolve ordering
			events := sess.Events()
			var lastLLMEvent *session.Event
			var functionCalls []*genai.FunctionCall

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
				// Fallback if we cannot find the originating event
				return rt.Run(ctx, args)
			}

			// 2. Find the index of the current tool call
			currCallID := ctx.FunctionCallID()
			callIndex := -1
			for idx, call := range functionCalls {
				if call.ID == currCallID {
					callIndex = idx
					break
				}
			}

			if callIndex == -1 {
				// Fallback if tool call ID is not found in the event
				return rt.Run(ctx, args)
			}

			// 3. Acquire queue and block until our turn (callIndex) arrives
			q := getSessionQueue(sess.ID())

			q.mu.Lock()
			// Reset queue index if we transitioned to a new event/batch
			if q.lastEventID != lastLLMEvent.ID {
				q.lastEventID = lastLLMEvent.ID
				q.nextIndex = 0
				q.notifyChs = make(map[int]chan struct{})
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

			// 4. Run the tool synchronously
			result, err := rt.Run(ctx, args)

			// 5. Advance queue and wake up the next tool call
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
