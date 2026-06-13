package sqlitesession

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/session"
)

// SQLiteService implements session.Service.
type SQLiteService struct {
	db *sql.DB
}

// NewSQLiteService creates a new SQLite-backed session.Service.
func NewSQLiteService(db *sql.DB) session.Service {
	return &SQLiteService{
		db: db,
	}
}

// Create creates a new session in the SQLite database.
func (s *SQLiteService) Create(ctx context.Context, req *session.CreateRequest) (*session.CreateResponse, error) {
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}

	now := time.Now()

	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO sessions (app_name, user_id, session_id, last_update_time)
		VALUES (?, ?, ?, ?)`,
		req.AppName, req.UserID, sessionID, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create session record: %w", err)
	}

	state := &sqliteState{
		db:        s.db,
		appName:   req.AppName,
		userID:    req.UserID,
		sessionID: sessionID,
	}

	if req.State != nil {
		for k, v := range req.State {
			if err := state.Set(k, v); err != nil {
				return nil, fmt.Errorf("failed to set initial session state for key %s: %w", k, err)
			}
		}
	}

	sess := &SQLiteSession{
		appName:        req.AppName,
		userID:         req.UserID,
		sessionID:      sessionID,
		state:          state,
		events:         &sqliteEvents{events: []*session.Event{}},
		lastUpdateTime: now,
	}

	return &session.CreateResponse{
		Session: sess,
	}, nil
}

// Get retrieves a session from the SQLite database.
func (s *SQLiteService) Get(ctx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
	var lastUpdate time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT last_update_time FROM sessions
		WHERE app_name = ? AND user_id = ? AND session_id = ?`,
		req.AppName, req.UserID, req.SessionID).Scan(&lastUpdate)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("session %s not found: %w", req.SessionID, sql.ErrNoRows)
		}
		return nil, fmt.Errorf("failed to query session %s: %w", req.SessionID, err)
	}

	// Build query for events
	var queryStr strings.Builder
	var args []any
	queryStr.WriteString(`
		SELECT event_id, timestamp, invocation_id, branch, author, event_data_json
		FROM session_events
		WHERE app_name = ? AND user_id = ? AND session_id = ?`)
	args = append(args, req.AppName, req.UserID, req.SessionID)

	if !req.After.IsZero() {
		queryStr.WriteString(" AND timestamp >= ?")
		args = append(args, req.After)
	}

	var finalQuery string
	if req.NumRecentEvents > 0 {
		// Get the most recent N events in descending order, then re-sort them in ascending order
		finalQuery = fmt.Sprintf("SELECT * FROM (%s ORDER BY timestamp DESC LIMIT %d) AS sub ORDER BY timestamp ASC", queryStr.String(), req.NumRecentEvents)
	} else {
		queryStr.WriteString(" ORDER BY timestamp ASC")
		finalQuery = queryStr.String()
	}

	rows, err := s.db.QueryContext(ctx, finalQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query session events: %w", err)
	}
	defer rows.Close()

	var events []*session.Event
	for rows.Next() {
		var eventID string
		var timestamp time.Time
		var invocationID string
		var branch string
		var author string
		var dataJSON string

		if err := rows.Scan(&eventID, &timestamp, &invocationID, &branch, &author, &dataJSON); err != nil {
			return nil, fmt.Errorf("failed to scan event row: %w", err)
		}

		var ev session.Event
		if err := json.Unmarshal([]byte(dataJSON), &ev); err != nil {
			return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
		}

		ev.ID = eventID
		ev.Timestamp = timestamp
		ev.InvocationID = invocationID
		ev.Branch = branch
		ev.Author = author

		events = append(events, &ev)
	}

	state := &sqliteState{
		db:        s.db,
		appName:   req.AppName,
		userID:    req.UserID,
		sessionID: req.SessionID,
	}

	sess := &SQLiteSession{
		appName:        req.AppName,
		userID:         req.UserID,
		sessionID:      req.SessionID,
		state:          state,
		events:         &sqliteEvents{events: events},
		lastUpdateTime: lastUpdate,
	}

	return &session.GetResponse{
		Session: sess,
	}, nil
}

// List lists all sessions in the SQLite database for the given app and user.
func (s *SQLiteService) List(ctx context.Context, req *session.ListRequest) (*session.ListResponse, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT session_id, last_update_time FROM sessions
		WHERE app_name = ? AND user_id = ?
		ORDER BY last_update_time DESC`,
		req.AppName, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to query session list: %w", err)
	}
	defer rows.Close()

	var sessionsList []session.Session
	for rows.Next() {
		var sessionID string
		var lastUpdate time.Time
		if err := rows.Scan(&sessionID, &lastUpdate); err != nil {
			return nil, fmt.Errorf("failed to scan session list row: %w", err)
		}

		evRows, err := s.db.QueryContext(ctx, `
			SELECT event_id, timestamp, invocation_id, branch, author, event_data_json
			FROM session_events
			WHERE app_name = ? AND user_id = ? AND session_id = ?
			ORDER BY timestamp ASC`,
			req.AppName, req.UserID, sessionID)
		if err != nil {
			return nil, fmt.Errorf("failed to list session events for %s: %w", sessionID, err)
		}

		var events []*session.Event
		for evRows.Next() {
			var eventID string
			var timestamp time.Time
			var invocationID string
			var branch string
			var author string
			var dataJSON string

			if err := evRows.Scan(&eventID, &timestamp, &invocationID, &branch, &author, &dataJSON); err != nil {
				evRows.Close()
				return nil, fmt.Errorf("failed to scan event row in list: %w", err)
			}

			var ev session.Event
			if err := json.Unmarshal([]byte(dataJSON), &ev); err != nil {
				evRows.Close()
				return nil, fmt.Errorf("failed to unmarshal event %s in list: %w", eventID, err)
			}
			ev.ID = eventID
			ev.Timestamp = timestamp
			ev.InvocationID = invocationID
			ev.Branch = branch
			ev.Author = author
			events = append(events, &ev)
		}
		evRows.Close()

		state := &sqliteState{
			db:        s.db,
			appName:   req.AppName,
			userID:    req.UserID,
			sessionID: sessionID,
		}

		sessionsList = append(sessionsList, &SQLiteSession{
			appName:        req.AppName,
			userID:         req.UserID,
			sessionID:      sessionID,
			state:          state,
			events:         &sqliteEvents{events: events},
			lastUpdateTime: lastUpdate,
		})
	}

	return &session.ListResponse{
		Sessions: sessionsList,
	}, nil
}

// Delete removes a session and its associated states/events from the database.
func (s *SQLiteService) Delete(ctx context.Context, req *session.DeleteRequest) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin delete transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE app_name = ? AND user_id = ? AND session_id = ?`,
		req.AppName, req.UserID, req.SessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session metadata: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		DELETE FROM session_state
		WHERE app_name = ? AND user_id = ? AND session_id = ?`,
		req.AppName, req.UserID, req.SessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session states: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		DELETE FROM session_events
		WHERE app_name = ? AND user_id = ? AND session_id = ?`,
		req.AppName, req.UserID, req.SessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session events: %w", err)
	}

	return tx.Commit()
}

// AppendEvent stores an event and updates states/session times.
func (s *SQLiteService) AppendEvent(ctx context.Context, sess session.Session, ev *session.Event) error {
	if ev.ID == "" {
		ev.ID = fmt.Sprintf("ev_%d", time.Now().UnixNano())
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}

	dataJSON, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin append transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO session_events (event_id, app_name, user_id, session_id, timestamp, invocation_id, branch, author, event_data_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, sess.AppName(), sess.UserID(), sess.ID(), ev.Timestamp, ev.InvocationID, ev.Branch, ev.Author, string(dataJSON))
	if err != nil {
		return fmt.Errorf("failed to save event: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE sessions SET last_update_time = ?
		WHERE app_name = ? AND user_id = ? AND session_id = ?`,
		ev.Timestamp, sess.AppName(), sess.UserID(), sess.ID())
	if err != nil {
		return fmt.Errorf("failed to update session last_update_time: %w", err)
	}

	if ev.Actions.StateDelta != nil {
		stateObj := sess.State()
		for k, v := range ev.Actions.StateDelta {
			valBytes, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("failed to marshal state value for key %s: %w", k, err)
			}

			targetUserID := sess.UserID()
			targetSessionID := sess.ID()
			if strings.HasPrefix(k, "app:") {
				targetUserID = ""
				targetSessionID = ""
			} else if strings.HasPrefix(k, "user:") {
				targetSessionID = ""
			}

			_, err = tx.ExecContext(ctx, `
				INSERT OR REPLACE INTO session_state (app_name, user_id, session_id, key, value_json)
				VALUES (?, ?, ?, ?, ?)`,
				sess.AppName(), targetUserID, targetSessionID, k, string(valBytes))
			if err != nil {
				return fmt.Errorf("failed to save state delta key %s: %w", k, err)
			}

			_ = stateObj.Set(k, v)
		}
	}

	if ev.Actions.StateDelta != nil {
		for k := range ev.Actions.StateDelta {
			if strings.HasPrefix(k, "temp:") {
				delete(ev.Actions.StateDelta, k)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit append event transaction: %w", err)
	}

	if sqliteSess, ok := sess.(*SQLiteSession); ok {
		sqliteSess.mu.Lock()
		sqliteSess.events.events = append(sqliteSess.events.events, ev)
		sqliteSess.lastUpdateTime = ev.Timestamp
		sqliteSess.mu.Unlock()
	}

	return nil
}

// SQLiteSession implements session.Session.
type SQLiteSession struct {
	appName        string
	userID         string
	sessionID      string
	state          *sqliteState
	events         *sqliteEvents
	lastUpdateTime time.Time
	mu             sync.RWMutex
}

// ID returns the session ID.
func (s *SQLiteSession) ID() string {
	return s.sessionID
}

// AppName returns the app name.
func (s *SQLiteSession) AppName() string {
	return s.appName
}

// UserID returns the user ID.
func (s *SQLiteSession) UserID() string {
	return s.userID
}

// State returns the SQLite key-value state store.
func (s *SQLiteSession) State() session.State {
	return s.state
}

// Events returns the sequence of events.
func (s *SQLiteSession) Events() session.Events {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.events
}

// LastUpdateTime returns the last update timestamp.
func (s *SQLiteSession) LastUpdateTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastUpdateTime
}

type sqliteEvents struct {
	events []*session.Event
}

func (e *sqliteEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, ev := range e.events {
			if !yield(ev) {
				return
			}
		}
	}
}

func (e *sqliteEvents) Len() int {
	return len(e.events)
}

func (e *sqliteEvents) At(i int) *session.Event {
	if i < 0 || i >= len(e.events) {
		return nil
	}
	return e.events[i]
}

type sqliteState struct {
	db        *sql.DB
	appName   string
	userID    string
	sessionID string
}

func (s *sqliteState) Get(key string) (any, error) {
	targetUserID := s.userID
	targetSessionID := s.sessionID
	if strings.HasPrefix(key, "app:") {
		targetUserID = ""
		targetSessionID = ""
	} else if strings.HasPrefix(key, "user:") {
		targetSessionID = ""
	}

	var valJSON string
	err := s.db.QueryRow(`
		SELECT value_json FROM session_state
		WHERE app_name = ? AND user_id = ? AND session_id = ? AND key = ?`,
		s.appName, targetUserID, targetSessionID, key).Scan(&valJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, session.ErrStateKeyNotExist
		}
		return nil, err
	}

	var val any
	if err := json.Unmarshal([]byte(valJSON), &val); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state value: %w", err)
	}

	return val, nil
}

func (s *sqliteState) Set(key string, val any) error {
	valBytes, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("failed to marshal state value: %w", err)
	}

	targetUserID := s.userID
	targetSessionID := s.sessionID
	if strings.HasPrefix(key, "app:") {
		targetUserID = ""
		targetSessionID = ""
	} else if strings.HasPrefix(key, "user:") {
		targetSessionID = ""
	}

	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO session_state (app_name, user_id, session_id, key, value_json)
		VALUES (?, ?, ?, ?, ?)`,
		s.appName, targetUserID, targetSessionID, key, string(valBytes))
	if err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	return nil
}

func (s *sqliteState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		rows, err := s.db.Query(`
			SELECT key, value_json FROM session_state
			WHERE (app_name = ? AND user_id = '' AND session_id = '')
			   OR (app_name = ? AND user_id = ? AND session_id = '')
			   OR (app_name = ? AND user_id = ? AND session_id = ?)`,
			s.appName, s.appName, s.userID, s.appName, s.userID, s.sessionID)
		if err != nil {
			return
		}
		defer rows.Close()

		for rows.Next() {
			var key string
			var valJSON string
			if err := rows.Scan(&key, &valJSON); err != nil {
				return
			}

			var val any
			if err := json.Unmarshal([]byte(valJSON), &val); err != nil {
				continue
			}

			if !yield(key, val) {
				return
			}
		}
	}
}
