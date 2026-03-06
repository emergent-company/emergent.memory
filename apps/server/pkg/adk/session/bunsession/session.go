package bunsession

import (
	"iter"
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/session"
)

type localSession struct {
	appName   string
	userID    string
	sessionID string

	// guards all mutable fields
	mu        sync.RWMutex
	events    []*session.Event
	state     map[string]any
	appState  map[string]any
	userState map[string]any
	updatedAt time.Time
}

func newLocalSession(appName, userID, sessionID string, state, appState, userState map[string]any, updatedAt time.Time, events []*session.Event) *localSession {
	if state == nil {
		state = make(map[string]any)
	}
	if appState == nil {
		appState = make(map[string]any)
	}
	if userState == nil {
		userState = make(map[string]any)
	}

	// Pre-merge for access in State()
	merged := make(map[string]any)
	for k, v := range appState {
		merged[k] = v
	}
	for k, v := range userState {
		merged[k] = v
	}
	for k, v := range state {
		merged[k] = v
	}

	return &localSession{
		appName:   appName,
		userID:    userID,
		sessionID: sessionID,
		state:     merged,
		appState:  appState,
		userState: userState,
		updatedAt: updatedAt,
		events:    events,
	}
}

func (s *localSession) ID() string {
	return s.sessionID
}

func (s *localSession) AppName() string {
	return s.appName
}

func (s *localSession) UserID() string {
	return s.userID
}

func (s *localSession) State() session.State {
	return &localState{
		mu:    &s.mu,
		state: s.state,
	}
}

func (s *localSession) Events() session.Events {
	return localEvents(s.events)
}

func (s *localSession) LastUpdateTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.updatedAt
}

type localEvents []*session.Event

func (e localEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, event := range e {
			if !yield(event) {
				return
			}
		}
	}
}

func (e localEvents) Len() int {
	return len(e)
}

func (e localEvents) At(i int) *session.Event {
	if i >= 0 && i < len(e) {
		return e[i]
	}
	return nil
}

type localState struct {
	mu    *sync.RWMutex
	state map[string]any
}

func (s *localState) Get(key string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	val, ok := s.state[key]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}

	return val, nil
}

func (s *localState) All() iter.Seq2[string, any] {
	return func(yield func(key string, val any) bool) {
		s.mu.RLock()

		for k, v := range s.state {
			s.mu.RUnlock()
			if !yield(k, v) {
				return
			}
			s.mu.RLock()
		}

		s.mu.RUnlock()
	}
}

func (s *localState) Set(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state[key] = value
	return nil
}

func isTempKey(key string) bool {
	return strings.HasPrefix(key, session.KeyPrefixTemp)
}

func extractStateDeltas(delta map[string]any) (app, user, temp, sess map[string]any) {
	app = make(map[string]any)
	user = make(map[string]any)
	temp = make(map[string]any)
	sess = make(map[string]any)

	for k, v := range delta {
		switch {
		case strings.HasPrefix(k, session.KeyPrefixApp):
			app[k] = v
		case strings.HasPrefix(k, session.KeyPrefixUser):
			user[k] = v
		case strings.HasPrefix(k, session.KeyPrefixTemp):
			temp[k] = v
		default:
			sess[k] = v
		}
	}
	return
}
