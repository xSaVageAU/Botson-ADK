package openrouter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"reflect"
	"strings"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
	"botson/internal/prompt"
)

type OpenRouterModel struct {
	modelNameGetter func() string
	apiKeyGetter    func() string
	envTypeGetter   func() string
}

// NewModel creates a new OpenRouter implementation of model.LLM using dynamic configuration getters.
func NewModel(ctx context.Context, modelNameGetter func() string, apiKeyGetter func() string, envTypeGetter func() string) (model.LLM, error) {
	return &OpenRouterModel{
		modelNameGetter: modelNameGetter,
		apiKeyGetter:    apiKeyGetter,
		envTypeGetter:   envTypeGetter,
	}, nil
}

func (m *OpenRouterModel) Name() string {
	return m.modelNameGetter()
}

type openRouterToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openRouterToolCall struct {
	ID       string                     `json:"id"`
	Type     string                     `json:"type"`
	Function openRouterToolCallFunction `json:"function"`
}

type openRouterMessage struct {
	Role       string               `json:"role"`
	Content    string               `json:"content,omitempty"`
	Name       string               `json:"name,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
	ToolCalls  []openRouterToolCall `json:"tool_calls,omitempty"`
}

type openRouterToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"`
}

type openRouterTool struct {
	Type     string                 `json:"type"`
	Function openRouterToolFunction `json:"function"`
}

type openRouterRequest struct {
	Model    string              `json:"model"`
	Messages []openRouterMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Tools    []openRouterTool    `json:"tools,omitempty"`
}

type openRouterDeltaToolCall struct {
	Index    *int                       `json:"index,omitempty"`
	ID       string                     `json:"id,omitempty"`
	Type     string                     `json:"type,omitempty"`
	Function openRouterToolCallFunction `json:"function,omitempty"`
}

type openRouterDeltaMessage struct {
	Role      string                    `json:"role"`
	Content   string                    `json:"content"`
	ToolCalls []openRouterDeltaToolCall `json:"tool_calls,omitempty"`
}

type openRouterResponseChoice struct {
	Message openRouterMessage      `json:"message"`
	Delta   openRouterDeltaMessage `json:"delta"`
}

type openRouterResponse struct {
	Choices []openRouterResponseChoice `json:"choices"`
}

func (m *OpenRouterModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		apiKey := m.apiKeyGetter()
		modelName := m.modelNameGetter()

		if apiKey == "" {
			yield(nil, fmt.Errorf("openrouter API key is required"))
			return
		}
		if modelName == "" {
			modelName = "openrouter/owl-alpha"
		}

		// Extract tool declarations
		var tools []openRouterTool
		for _, t := range req.Tools {
			var decl *genai.FunctionDeclaration
			val := reflect.ValueOf(t)
			declMethod := val.MethodByName("Declaration")
			if declMethod.IsValid() {
				results := declMethod.Call(nil)
				if len(results) > 0 {
					if d, ok := results[0].Interface().(*genai.FunctionDeclaration); ok {
						decl = d
					}
				}
			}

			if decl != nil {
				var params any
				if decl.ParametersJsonSchema != nil {
					params = decl.ParametersJsonSchema
				} else if decl.Parameters != nil {
					params = decl.Parameters
				}

				tools = append(tools, openRouterTool{
					Type: "function",
					Function: openRouterToolFunction{
						Name:        decl.Name,
						Description: decl.Description,
						Parameters:  params,
					},
				})
			}
		}

		var messages []openRouterMessage

		// 1. System instructions if provided in Config
		if req.Config != nil && req.Config.SystemInstruction != nil {
			var sysText strings.Builder
			for _, part := range req.Config.SystemInstruction.Parts {
				if part.Text != "" {
					sysText.WriteString(part.Text)
				}
			}
			if sysText.Len() > 0 {
				resolved := prompt.ResolvePlaceholders(sysText.String(), m.envTypeGetter())
				messages = append(messages, openRouterMessage{
					Role:    "system",
					Content: resolved,
				})
			}
		}

		// 2. Map conversation contents
		for _, content := range req.Contents {
			role := "user"
			if content.Role == "model" {
				role = "assistant"
			} else if content.Role == "function" {
				role = "tool"
			}

			var textBuilder strings.Builder
			var toolCalls []openRouterToolCall
			hasFunctionResponse := false

			for _, part := range content.Parts {
				if part.Text != "" {
					textBuilder.WriteString(part.Text)
				}
				if part.FunctionCall != nil {
					argsBytes, err := json.Marshal(part.FunctionCall.Args)
					argsStr := "{}"
					if err == nil {
						argsStr = string(argsBytes)
					}
					callID := part.FunctionCall.ID
					if callID == "" {
						callID = "call_" + part.FunctionCall.Name
					}
					toolCalls = append(toolCalls, openRouterToolCall{
						ID:   callID,
						Type: "function",
						Function: openRouterToolCallFunction{
							Name:      part.FunctionCall.Name,
							Arguments: argsStr,
						},
					})
				}
				if part.FunctionResponse != nil {
					hasFunctionResponse = true
					respBytes, err := json.Marshal(part.FunctionResponse.Response)
					respStr := "{}"
					if err == nil {
						respStr = string(respBytes)
					}
					callID := part.FunctionResponse.ID
					if callID == "" {
						callID = "call_" + part.FunctionResponse.Name
					}
					messages = append(messages, openRouterMessage{
						Role:       "tool",
						Content:    respStr,
						Name:       part.FunctionResponse.Name,
						ToolCallID: callID,
					})
				}
			}

			if hasFunctionResponse {
				continue
			}

			msg := openRouterMessage{
				Role:    role,
				Content: textBuilder.String(),
			}
			if len(toolCalls) > 0 {
				msg.ToolCalls = toolCalls
			}
			messages = append(messages, msg)
		}

		// 3. Make OpenRouter payload
		payload := openRouterRequest{
			Model:    modelName,
			Messages: messages,
			Stream:   stream,
			Tools:    tools,
		}

		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			yield(nil, fmt.Errorf("failed to marshal openrouter request: %w", err))
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(jsonBytes))
		if err != nil {
			yield(nil, fmt.Errorf("failed to create http request: %w", err))
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		httpReq.Header.Set("HTTP-Referer", "https://github.com/google/adk-go")
		httpReq.Header.Set("X-Title", "Botson Agent")

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("http request failed: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			yield(nil, fmt.Errorf("openrouter returned status %d: %s", resp.StatusCode, string(bodyBytes)))
			return
		}

		if !stream {
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				yield(nil, fmt.Errorf("failed to read response: %w", err))
				return
			}
			var openResp openRouterResponse
			if err := json.Unmarshal(bodyBytes, &openResp); err != nil {
				yield(nil, fmt.Errorf("failed to unmarshal response: %w", err))
				return
			}
			if len(openResp.Choices) == 0 {
				yield(nil, fmt.Errorf("no choices returned from model"))
				return
			}

			choice := openResp.Choices[0]
			var parts []*genai.Part
			if choice.Message.Content != "" {
				parts = append(parts, &genai.Part{Text: choice.Message.Content})
			}
			for _, tc := range choice.Message.ToolCalls {
				var argsMap map[string]any
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &argsMap); err != nil {
					argsMap = make(map[string]any)
				}
				parts = append(parts, &genai.Part{
					FunctionCall: &genai.FunctionCall{
						ID:   tc.ID,
						Name: tc.Function.Name,
						Args: argsMap,
					},
				})
			}

			respObj := &model.LLMResponse{
				Content: &genai.Content{
					Role:  "model",
					Parts: parts,
				},
				Partial:      false,
				TurnComplete: true,
			}
			yield(respObj, nil)
			return
		}

		// Streaming mode
		var accumulatedText strings.Builder
		
		type accumulatedToolCall struct {
			id        string
			name      string
			argsParts []string
		}
		var streamToolCalls = make(map[int]*accumulatedToolCall)

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				yield(nil, fmt.Errorf("failed to read stream line: %w", err))
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			dataStr := strings.TrimPrefix(line, "data: ")
			if dataStr == "[DONE]" {
				break
			}

			var chunk openRouterResponse
			if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
				continue
			}

			if len(chunk.Choices) > 0 {
				choice := chunk.Choices[0]
				content := choice.Delta.Content
				if content != "" {
					accumulatedText.WriteString(content)
					respObj := &model.LLMResponse{
						Content: &genai.Content{
							Role: "model",
							Parts: []*genai.Part{
								{Text: content},
							},
						},
						Partial:      true,
						TurnComplete: false,
					}
					if !yield(respObj, nil) {
						return // yield requested stop
					}
				}
				
				for _, tc := range choice.Delta.ToolCalls {
					idx := 0
					if tc.Index != nil {
						idx = *tc.Index
					}
					acc, exists := streamToolCalls[idx]
					if !exists {
						acc = &accumulatedToolCall{}
						streamToolCalls[idx] = acc
					}
					if tc.ID != "" {
						acc.id = tc.ID
					}
					if tc.Function.Name != "" {
						acc.name = tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						acc.argsParts = append(acc.argsParts, tc.Function.Arguments)
					}
				}
			}
		}

		// Yield final complete chunk with full accumulated text and tool calls
		var parts []*genai.Part
		if accumulatedText.Len() > 0 {
			parts = append(parts, &genai.Part{Text: accumulatedText.String()})
		}
		
		var maxIdx = -1
		for idx := range streamToolCalls {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
		for i := 0; i <= maxIdx; i++ {
			acc, exists := streamToolCalls[i]
			if !exists {
				continue
			}
			argsStr := strings.Join(acc.argsParts, "")
			var argsMap map[string]any
			if err := json.Unmarshal([]byte(argsStr), &argsMap); err != nil {
				argsMap = make(map[string]any)
			}
			parts = append(parts, &genai.Part{
				FunctionCall: &genai.FunctionCall{
					ID:   acc.id,
					Name: acc.name,
					Args: argsMap,
				},
			})
		}

		finalResp := &model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: parts,
			},
			Partial:      false,
			TurnComplete: true,
		}
		yield(finalResp, nil)
	}
}
