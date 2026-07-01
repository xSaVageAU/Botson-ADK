package sqlitesession_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	sqlitesession "botson/internal/session"
)

func TestSQLiteSessionService(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := sqlitesession.NewSQLiteService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to initialize session service: %v", err)
	}
	defer func() {
		if closer, ok := svc.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()
	ctx := context.Background()

	appName := "test-app"
	userA := "user-a"
	userB := "user-b"

	// 1. Test Create Session
	respA1, err := svc.Create(ctx, &session.CreateRequest{
		AppName: appName,
		UserID:  userA,
		State: map[string]any{
			"init_key": "init_val",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create session A1: %v", err)
	}
	sessA1 := respA1.Session
	if sessA1.ID() == "" {
		t.Errorf("Expected auto-generated session ID, got empty")
	}
	if sessA1.AppName() != appName || sessA1.UserID() != userA {
		t.Errorf("Mismatch in session metadata")
	}

	// Verify initial state
	val, err := sessA1.State().Get("init_key")
	if err != nil {
		t.Fatalf("Failed to get initial state: %v", err)
	}
	if val.(string) != "init_val" {
		t.Errorf("Expected 'init_val', got '%v'", val)
	}

	// 2. Test Key Scoping (app:, user:, session-local)
	// Create another session for User A
	respA2, err := svc.Create(ctx, &session.CreateRequest{
		AppName: appName,
		UserID:  userA,
	})
	if err != nil {
		t.Fatalf("Failed to create session A2: %v", err)
	}
	sessA2 := respA2.Session

	// Create a session for User B
	respB1, err := svc.Create(ctx, &session.CreateRequest{
		AppName: appName,
		UserID:  userB,
	})
	if err != nil {
		t.Fatalf("Failed to create session B1: %v", err)
	}
	sessB1 := respB1.Session

	// Set scoped variables in A1
	if err := sessA1.State().Set("local_var", "local-a1"); err != nil {
		t.Fatalf("Failed to set local_var: %v", err)
	}
	if err := sessA1.State().Set("user:nickname", "Savage"); err != nil {
		t.Fatalf("Failed to set user:nickname: %v", err)
	}
	if err := sessA1.State().Set("app:maintenance", true); err != nil {
		t.Fatalf("Failed to set app:maintenance: %v", err)
	}

	// Verify A1 can read all its variables
	if v, err := sessA1.State().Get("local_var"); err != nil || v.(string) != "local-a1" {
		t.Errorf("Failed to read local_var in A1: v=%v, err=%v", v, err)
	}
	if v, err := sessA1.State().Get("user:nickname"); err != nil || v.(string) != "Savage" {
		t.Errorf("Failed to read user:nickname in A1: v=%v, err=%v", v, err)
	}
	if v, err := sessA1.State().Get("app:maintenance"); err != nil || v.(bool) != true {
		t.Errorf("Failed to read app:maintenance in A1: v=%v, err=%v", v, err)
	}

	// Verify A2 (same user) can read user: and app: but NOT local_var
	if v, err := sessA2.State().Get("user:nickname"); err != nil || v.(string) != "Savage" {
		t.Errorf("A2 should access user:nickname: v=%v, err=%v", v, err)
	}
	if v, err := sessA2.State().Get("app:maintenance"); err != nil || v.(bool) != true {
		t.Errorf("A2 should access app:maintenance: v=%v, err=%v", v, err)
	}
	if _, err := sessA2.State().Get("local_var"); err == nil {
		t.Errorf("A2 should NOT have access to A1's local_var")
	}

	// Verify B1 (different user) can read app: but NOT user:nickname or local_var
	if v, err := sessB1.State().Get("app:maintenance"); err != nil || v.(bool) != true {
		t.Errorf("B1 should access app:maintenance: v=%v, err=%v", v, err)
	}
	if _, err := sessB1.State().Get("user:nickname"); err == nil {
		t.Errorf("B1 should NOT have access to User A's nickname")
	}
	if _, err := sessB1.State().Get("local_var"); err == nil {
		t.Errorf("B1 should NOT have access to A1's local_var")
	}

	// 3. Test AppendEvent & Get with Chronology
	ev1 := &session.Event{
		Timestamp:    time.Now().Add(-10 * time.Minute),
		InvocationID: "inv-1",
		Branch:       "main",
		Author:       "user",
	}
	ev1.Content = &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			{Text: "Hello Botson!"},
		},
	}

	ev2 := &session.Event{
		Timestamp:    time.Now().Add(-5 * time.Minute),
		InvocationID: "inv-1",
		Branch:       "main",
		Author:       "botson",
	}
	ev2.Content = &genai.Content{
		Role: "model",
		Parts: []*genai.Part{
			{Text: "Hello there! How can I help you today?"},
		},
	}

	if err := svc.AppendEvent(ctx, sessA1, ev1); err != nil {
		t.Fatalf("Failed to append event 1: %v", err)
	}
	if err := svc.AppendEvent(ctx, sessA1, ev2); err != nil {
		t.Fatalf("Failed to append event 2: %v", err)
	}

	// Retrieve session and verify history
	getResp, err := svc.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userA,
		SessionID: sessA1.ID(),
	})
	if err != nil {
		t.Fatalf("Failed to retrieve session A1: %v", err)
	}
	retrievedSess := getResp.Session
	events := retrievedSess.Events()
	if events.Len() != 2 {
		t.Errorf("Expected 2 events, got %d", events.Len())
	}

	// Verify fields and chronological ordering (ev1 then ev2)
	retrievedEv1 := events.At(0)
	retrievedEv2 := events.At(1)
	if retrievedEv1.Author != "user" || retrievedEv1.Content.Parts[0].Text != "Hello Botson!" {
		t.Errorf("Event 1 fields mismatched")
	}
	if retrievedEv2.Author != "botson" || retrievedEv2.Content.Parts[0].Text != "Hello there! How can I help you today?" {
		t.Errorf("Event 2 fields mismatched")
	}

	// Test NumRecentEvents filtering (limit 1)
	getFilterResp, err := svc.Get(ctx, &session.GetRequest{
		AppName:         appName,
		UserID:          userA,
		SessionID:       sessA1.ID(),
		NumRecentEvents: 1,
	})
	if err != nil {
		t.Fatalf("Failed to retrieve session with limit: %v", err)
	}
	if getFilterResp.Session.Events().Len() != 1 {
		t.Errorf("Expected 1 event, got %d", getFilterResp.Session.Events().Len())
	}
	if getFilterResp.Session.Events().At(0).Content.Parts[0].Text != ev2.Content.Parts[0].Text {
		t.Errorf("Expected the most recent event (ev2)")
	}

	// 4. Test List sessions
	listResp, err := svc.List(ctx, &session.ListRequest{
		AppName: appName,
		UserID:  userA,
	})
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}
	if len(listResp.Sessions) != 2 {
		t.Errorf("Expected 2 sessions for User A, got %d", len(listResp.Sessions))
	}

	// 5. Test Delete session
	err = svc.Delete(ctx, &session.DeleteRequest{
		AppName:   appName,
		UserID:    userA,
		SessionID: sessA1.ID(),
	})
	if err != nil {
		t.Fatalf("Failed to delete session A1: %v", err)
	}

	// Verify session A1 is deleted
	_, err = svc.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userA,
		SessionID: sessA1.ID(),
	})
	if err == nil {
		t.Errorf("Expected error getting deleted session, but it succeeded")
	}
}
