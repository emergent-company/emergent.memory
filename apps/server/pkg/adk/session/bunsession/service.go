package bunsession

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

type bunService struct {
	db bun.IDB
}

// NewService creates a new bun-backed ADK session service.
func NewService(db bun.IDB) session.Service {
	return &bunService{db: db}
}

// Create handles creating a new session.
func (s *bunService) Create(ctx context.Context, req *session.CreateRequest) (*session.CreateResponse, error) {
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	state := req.State
	if state == nil {
		state = make(map[string]any)
	}

	record := &ADKSession{
		ID:         sessionID,
		AppName:    req.AppName,
		UserID:     req.UserID,
		State:      state,
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}

	_, err := s.db.NewInsert().Model(record).Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Also fetch app and user state
	appState, err := s.fetchState(ctx, "app", req.AppName, "", "")
	if err != nil {
		return nil, err
	}
	userState, err := s.fetchState(ctx, "user", req.AppName, req.UserID, "")
	if err != nil {
		return nil, err
	}

	localSess := newLocalSession(req.AppName, req.UserID, sessionID, state, appState, userState, record.UpdateTime, nil)

	return &session.CreateResponse{Session: localSess}, nil
}

// Get retrieves an existing session with events.
func (s *bunService) Get(ctx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
	record := new(ADKSession)
	err := s.db.NewSelect().Model(record).
		Where("id = ? AND app_name = ? AND user_id = ?", req.SessionID, req.AppName, req.UserID).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	appState, err := s.fetchState(ctx, "app", req.AppName, "", "")
	if err != nil {
		return nil, err
	}
	userState, err := s.fetchState(ctx, "user", req.AppName, req.UserID, "")
	if err != nil {
		return nil, err
	}

	// Fetch events
	var events []*ADKEvent
	q := s.db.NewSelect().Model(&events).
		Where("session_id = ? AND app_name = ? AND user_id = ?", req.SessionID, req.AppName, req.UserID).
		Order("timestamp ASC")

	if !req.After.IsZero() {
		q = q.Where("timestamp >= ?", req.After)
	}

	err = q.Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}

	// If NumRecentEvents is > 0, we can slice it (though better in DB, but the interface requires returning recent ones)
	if req.NumRecentEvents > 0 && len(events) > req.NumRecentEvents {
		events = events[len(events)-req.NumRecentEvents:]
	}

	adkEvents := make([]*session.Event, len(events))
	for i, e := range events {
		adkEv, err := mapToADKEvent(e)
		if err != nil {
			return nil, fmt.Errorf("failed to map event: %w", err)
		}
		adkEvents[i] = adkEv
	}

	localSess := newLocalSession(req.AppName, req.UserID, req.SessionID, record.State, appState, userState, record.UpdateTime, adkEvents)

	return &session.GetResponse{Session: localSess}, nil
}

func (s *bunService) List(ctx context.Context, req *session.ListRequest) (*session.ListResponse, error) {
	var records []*ADKSession
	err := s.db.NewSelect().Model(&records).
		Where("app_name = ? AND user_id = ?", req.AppName, req.UserID).
		Order("update_time DESC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	sessions := make([]session.Session, len(records))
	for i, r := range records {
		sessions[i] = newLocalSession(r.AppName, r.UserID, r.ID, r.State, nil, nil, r.UpdateTime, nil)
	}

	return &session.ListResponse{Sessions: sessions}, nil
}

func (s *bunService) Delete(ctx context.Context, req *session.DeleteRequest) error {
	_, err := s.db.NewDelete().Model((*ADKSession)(nil)).
		Where("id = ? AND app_name = ? AND user_id = ?", req.SessionID, req.AppName, req.UserID).
		Exec(ctx)
	return err
}

func (s *bunService) AppendEvent(ctx context.Context, curSession session.Session, event *session.Event) error {
	sess, ok := curSession.(*localSession)
	if !ok {
		return fmt.Errorf("unexpected session type %T", curSession)
	}

	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Update states based on event.Actions.StateDelta
		if len(event.Actions.StateDelta) > 0 {
			err := s.applyStateDeltas(ctx, tx, sess, event.Actions.StateDelta)
			if err != nil {
				return err
			}
		}

		// Insert event
		record, err := mapToDBEvent(sess, event)
		if err != nil {
			return err
		}

		_, err = tx.NewInsert().Model(record).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to insert event: %w", err)
		}

		// Update session time
		now := time.Now()
		_, err = tx.NewUpdate().Model((*ADKSession)(nil)).
			Set("update_time = ?", now).
			Where("id = ? AND app_name = ? AND user_id = ?", sess.sessionID, sess.appName, sess.userID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update session time: %w", err)
		}

		// Also update the in-memory session object for immediate consistency
		sess.events = append(sess.events, event)
		sess.updatedAt = now

		return nil
	})

	if err != nil {
		return err
	}

	// Remove temporary keys from event.Actions.StateDelta after appending
	for k := range event.Actions.StateDelta {
		if isTempKey(k) {
			delete(event.Actions.StateDelta, k)
		}
	}

	return nil
}

func (s *bunService) fetchState(ctx context.Context, scope, appName, userID, sessionID string) (map[string]any, error) {
	record := new(ADKState)

	q := s.db.NewSelect().Model(record).
		Where("scope = ? AND app_name = ?", scope, appName)

	if userID != "" {
		q = q.Where("user_id = ?", userID)
	} else {
		q = q.Where("user_id IS NULL OR user_id = ''")
	}

	if sessionID != "" {
		q = q.Where("session_id = ?", sessionID)
	} else {
		q = q.Where("session_id IS NULL OR session_id = ''")
	}

	err := q.Scan(ctx)
	if err != nil {
		// Not found is fine, just return empty state
		return make(map[string]any), nil
	}

	if record.State == nil {
		return make(map[string]any), nil
	}

	return record.State, nil
}

func (s *bunService) applyStateDeltas(ctx context.Context, tx bun.Tx, sess *localSession, delta map[string]any) error {
	appDelta, userDelta, tempDelta, sessionDelta := extractStateDeltas(delta)

	if len(appDelta) > 0 {
		err := s.updateState(ctx, tx, "app", sess.appName, "", "", appDelta)
		if err != nil {
			return err
		}
		// update local
		for k, v := range appDelta {
			sess.appState[k] = v
		}
	}

	if len(userDelta) > 0 {
		err := s.updateState(ctx, tx, "user", sess.appName, sess.userID, "", userDelta)
		if err != nil {
			return err
		}
		for k, v := range userDelta {
			sess.userState[k] = v
		}
	}

	if len(sessionDelta) > 0 {
		// Update adk_sessions
		// we fetch current, merge, save
		record := new(ADKSession)
		err := tx.NewSelect().Model(record).
			Where("id = ? AND app_name = ? AND user_id = ?", sess.sessionID, sess.appName, sess.userID).
			For("UPDATE").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch session for update: %w", err)
		}
		if record.State == nil {
			record.State = make(map[string]any)
		}
		for k, v := range sessionDelta {
			if v == nil {
				delete(record.State, k)
			} else {
				record.State[k] = v
			}
		}
		_, err = tx.NewUpdate().Model(record).
			Set("state = ?", record.State).
			Where("id = ? AND app_name = ? AND user_id = ?", sess.sessionID, sess.appName, sess.userID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update session state: %w", err)
		}
		for k, v := range sessionDelta {
			sess.state[k] = v
		}
	}

	// temp keys only exist in the in-memory object during the invocation
	for k, v := range tempDelta {
		if v == nil {
			delete(sess.state, k)
		} else {
			sess.state[k] = v
		}
	}

	return nil
}

func (s *bunService) updateState(ctx context.Context, tx bun.Tx, scope, appName, userID, sessionID string, delta map[string]any) error {
	record := new(ADKState)
	q := tx.NewSelect().Model(record).
		Where("scope = ? AND app_name = ?", scope, appName).
		For("UPDATE")

	if userID != "" {
		q = q.Where("user_id = ?", userID)
	} else {
		q = q.Where("user_id IS NULL OR user_id = ''")
	}

	if sessionID != "" {
		q = q.Where("session_id = ?", sessionID)
	} else {
		q = q.Where("session_id IS NULL OR session_id = ''")
	}

	exists := true
	if err := q.Scan(ctx); err != nil {
		exists = false
		record = &ADKState{
			Scope:     scope,
			AppName:   appName,
			UserID:    userID,
			SessionID: sessionID,
			State:     make(map[string]any),
		}
	}

	if record.State == nil {
		record.State = make(map[string]any)
	}

	for k, v := range delta {
		if v == nil {
			delete(record.State, k)
		} else {
			record.State[k] = v
		}
	}
	record.UpdateTime = time.Now()

	var err error
	if exists {
		_, err = tx.NewUpdate().Model(record).
			Set("state = ?", record.State).
			Set("update_time = ?", record.UpdateTime).
			WherePK().
			Exec(ctx)
	} else {
		_, err = tx.NewInsert().Model(record).Exec(ctx)
	}

	return err
}

func mapToADKEvent(e *ADKEvent) (*session.Event, error) {
	ev := &session.Event{
		ID:           e.ID,
		Timestamp:    e.Timestamp,
		InvocationID: e.InvocationID,
		Author:       e.Author,
	}

	if e.Branch != nil {
		ev.Branch = *e.Branch
	}

	if len(e.Actions) > 0 {
		if err := unmarshalFromMap(e.Actions, &ev.Actions); err != nil {
			return nil, fmt.Errorf("unmarshal actions: %w", err)
		}
	}
	if len(e.LongRunningToolIDsJSON) > 0 {
		if err := unmarshalFromMap(e.LongRunningToolIDsJSON, &ev.LongRunningToolIDs); err != nil {
			return nil, fmt.Errorf("unmarshal long_running_tool_ids: %w", err)
		}
	}

	if len(e.Content) > 0 {
		var c genai.Content
		if err := unmarshalFromMap(e.Content, &c); err != nil {
			return nil, fmt.Errorf("unmarshal content: %w", err)
		}
		ev.Content = &c
	}
	if len(e.GroundingMetadata) > 0 {
		var gm genai.GroundingMetadata
		if err := unmarshalFromMap(e.GroundingMetadata, &gm); err != nil {
			return nil, fmt.Errorf("unmarshal grounding_metadata: %w", err)
		}
		ev.GroundingMetadata = &gm
	}
	if len(e.CustomMetadata) > 0 {
		if err := unmarshalFromMap(e.CustomMetadata, &ev.CustomMetadata); err != nil {
			return nil, fmt.Errorf("unmarshal custom_metadata: %w", err)
		}
	}
	if len(e.UsageMetadata) > 0 {
		var um genai.GenerateContentResponseUsageMetadata
		if err := unmarshalFromMap(e.UsageMetadata, &um); err != nil {
			return nil, fmt.Errorf("unmarshal usage_metadata: %w", err)
		}
		ev.UsageMetadata = &um
	}
	if len(e.CitationMetadata) > 0 {
		var cm genai.CitationMetadata
		if err := unmarshalFromMap(e.CitationMetadata, &cm); err != nil {
			return nil, fmt.Errorf("unmarshal citation_metadata: %w", err)
		}
		ev.CitationMetadata = &cm
	}

	if e.Partial != nil {
		ev.Partial = *e.Partial
	}
	if e.TurnComplete != nil {
		ev.TurnComplete = *e.TurnComplete
	}
	if e.ErrorCode != nil {
		ev.ErrorCode = *e.ErrorCode
	}
	if e.ErrorMessage != nil {
		ev.ErrorMessage = *e.ErrorMessage
	}
	if e.Interrupted != nil {
		ev.Interrupted = *e.Interrupted
	}

	return ev, nil
}

func mapToDBEvent(sess *localSession, e *session.Event) (*ADKEvent, error) {
	dbEv := &ADKEvent{
		ID:           e.ID,
		AppName:      sess.appName,
		UserID:       sess.userID,
		SessionID:    sess.sessionID,
		InvocationID: e.InvocationID,
		Author:       e.Author,
		Timestamp:    e.Timestamp,
	}

	if e.Branch != "" {
		dbEv.Branch = &e.Branch
	}

	var err error

	// Marshal typed structs → JSON bytes → map[string]any so that Bun stores
	// them as proper JSONB objects (json.RawMessage is serialised as a string by Bun).
	dbEv.Actions, err = marshalToMap(e.Actions)
	if err != nil {
		return nil, fmt.Errorf("marshal actions: %w", err)
	}

	if len(e.LongRunningToolIDs) > 0 {
		dbEv.LongRunningToolIDsJSON, err = marshalToMap(e.LongRunningToolIDs)
		if err != nil {
			return nil, fmt.Errorf("marshal long_running_tool_ids: %w", err)
		}
	}

	if e.Content != nil {
		dbEv.Content, err = marshalToMap(e.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal content: %w", err)
		}
	}
	if e.GroundingMetadata != nil {
		dbEv.GroundingMetadata, err = marshalToMap(e.GroundingMetadata)
		if err != nil {
			return nil, fmt.Errorf("marshal grounding_metadata: %w", err)
		}
	}
	if len(e.CustomMetadata) > 0 {
		dbEv.CustomMetadata, err = marshalToMap(e.CustomMetadata)
		if err != nil {
			return nil, fmt.Errorf("marshal custom_metadata: %w", err)
		}
	}
	if e.UsageMetadata != nil {
		dbEv.UsageMetadata, err = marshalToMap(e.UsageMetadata)
		if err != nil {
			return nil, fmt.Errorf("marshal usage_metadata: %w", err)
		}
	}
	if e.CitationMetadata != nil {
		dbEv.CitationMetadata, err = marshalToMap(e.CitationMetadata)
		if err != nil {
			return nil, fmt.Errorf("marshal citation_metadata: %w", err)
		}
	}

	dbEv.Partial = &e.Partial
	dbEv.TurnComplete = &e.TurnComplete
	dbEv.Interrupted = &e.Interrupted

	if e.ErrorCode != "" {
		dbEv.ErrorCode = &e.ErrorCode
	}
	if e.ErrorMessage != "" {
		dbEv.ErrorMessage = &e.ErrorMessage
	}

	return dbEv, nil
}

// unmarshalFromMap converts a map[string]any (as read from a Bun JSONB field) back
// into a typed struct via JSON round-trip.
func unmarshalFromMap(m map[string]any, dst any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// marshalToMap converts any value to a map[string]any via JSON round-trip.
// This is the correct way to store typed structs in Bun JSONB fields — using
// json.RawMessage directly causes Bun to store a quoted base64 string instead
// of a JSON object.
func marshalToMap(v any) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		// Fallback: wrap non-object types (arrays, scalars) so they fit in map[string]any.
		return map[string]any{"_value": json.RawMessage(b)}, nil
	}
	return m, nil
}
