package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"google.golang.org/adk/v2/session"
)

// getSessionPreview returns the first user text from a session (up to 45 chars)
// or "New Conversation" if the session is empty.
func getSessionPreview(s session.Session) string {
	for ev := range s.Events().All() {
		if ev.Content != nil && ev.Author == "user" {
			for _, part := range ev.Content.Parts {
				if part.Text != "" {
					text := strings.TrimSpace(part.Text)
					if len(text) > 45 {
						return text[:45] + "..."
					}
					return text
				}
			}
		}
	}
	return "New Conversation"
}

// handleListSessions returns a JSON array of all sessions sorted newest-first.
func handleListSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	res, err := sessService.List(r.Context(), &session.ListRequest{
		AppName: agentName,
		UserID:  "",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list sessions: %v", err), http.StatusInternalServerError)
		return
	}

	// Sort sessions: newest to oldest
	slices.SortFunc(res.Sessions, func(a, b session.Session) int {
		ta := a.LastUpdateTime()
		tb := b.LastUpdateTime()
		if ta.After(tb) {
			return -1
		}
		if ta.Before(tb) {
			return 1
		}
		return 0
	})

	type SessionInfo struct {
		ID         string    `json:"id"`
		UserID     string    `json:"user_id"`
		Preview    string    `json:"preview"`
		LastUpdate time.Time `json:"last_update"`
	}

	list := []SessionInfo{}
	for _, s := range res.Sessions {
		preview := "New Conversation"
		fullSess, err := sessService.Get(r.Context(), &session.GetRequest{
			AppName:   agentName,
			UserID:    s.UserID(),
			SessionID: s.ID(),
		})
		if err == nil && fullSess != nil {
			preview = getSessionPreview(fullSess.Session)
		}

		list = append(list, SessionInfo{
			ID:         s.ID(),
			UserID:     s.UserID(),
			Preview:    preview,
			LastUpdate: s.LastUpdateTime(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// handleCreateSession creates a new session for user "user" and returns its ID.
func handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	res, err := sessService.Create(r.Context(), &session.CreateRequest{
		AppName: agentName,
		UserID:  "user",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": res.Session.ID(),
	})
}

// handleGetSession returns the messages for a single session identified by
// the "id" and optional "user_id" query parameters.
func handleGetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("id")
	if sessionID == "" {
		http.Error(w, "Missing session id", http.StatusBadRequest)
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = "user"
	}

	res, err := sessService.Get(r.Context(), &session.GetRequest{
		AppName:   agentName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get session: %v", err), http.StatusInternalServerError)
		return
	}

	type Message struct {
		Role     string `json:"role"`
		Content  string `json:"content"`
		ToolName string `json:"tool_name,omitempty"`
	}

	messages := []Message{}
	for ev := range res.Session.Events().All() {
		if ev.Content != nil {
			for _, part := range ev.Content.Parts {
				if part.Text != "" {
					role := ev.Content.Role
					if role == "" {
						if ev.Author == "user" {
							role = "user"
						} else {
							role = "model"
						}
					}
					if role == "model" {
						role = "agent"
					}
					messages = append(messages, Message{
						Role:    role,
						Content: part.Text,
					})
				}
				if part.FunctionCall != nil {
					argsBytes, _ := json.Marshal(part.FunctionCall.Args)
					argsStr := string(argsBytes)
					if argsStr == "{}" {
						argsStr = ""
					}
					messages = append(messages, Message{
						Role:     "tool_call",
						Content:  argsStr,
						ToolName: part.FunctionCall.Name,
					})
				}
				if part.FunctionResponse != nil {
					var respStr string
					if part.FunctionResponse.Response != nil {
						if rVal, exists := part.FunctionResponse.Response["result"]; exists {
							respStr = fmt.Sprintf("%v", rVal)
						} else if rVal, exists := part.FunctionResponse.Response["output"]; exists {
							respStr = fmt.Sprintf("%v", rVal)
						}
					}
					if respStr == "" {
						respBytes, _ := json.Marshal(part.FunctionResponse.Response)
						respStr = string(respBytes)
					}
					messages = append(messages, Message{
						Role:     "tool_response",
						Content:  respStr,
						ToolName: part.FunctionResponse.Name,
					})
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":       res.Session.ID(),
		"messages": messages,
	})
}

// handleDeleteSession deletes a session identified by the "id" and optional
// "user_id" query parameters.
func handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("id")
	if sessionID == "" {
		http.Error(w, "Missing session id", http.StatusBadRequest)
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = "user"
	}

	err := sessService.Delete(r.Context(), &session.DeleteRequest{
		AppName:   agentName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success"})
}
