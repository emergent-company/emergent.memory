package account

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AccountInfo describes one locally stored account session. It is the shape the
// `auth list` command renders (text and JSON).
type AccountInfo struct {
	ServerURL string
	Email     string
	Issuer    string
	ExpiresAt time.Time
	SignedIn  bool
	Active    bool
}

// Accounts enumerates every locally stored session, sorted by server URL. The
// accounts directory is read directly so any file that parses as a Session is
// included. Unreadable, non-JSON, or corrupt entries are skipped rather than
// failing the whole listing; a missing directory yields an empty, non-nil
// slice.
func (m *Manager) Accounts() ([]AccountInfo, error) {
	infos := []AccountInfo{}
	entries, err := os.ReadDir(m.accountsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return infos, nil
		}
		return nil, fmt.Errorf("account: list sessions: %w", err)
	}
	active, _ := m.Active()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(m.accountsDir(), entry.Name()))
		if err != nil {
			continue
		}
		sess := &Session{}
		if err := json.Unmarshal(b, sess); err != nil {
			continue
		}
		// A file without a server URL or any credential is not a session.
		if sess.ServerURL == "" || (sess.AccessToken == "" && sess.RefreshToken == "") {
			continue
		}
		infos = append(infos, AccountInfo{
			ServerURL: sess.ServerURL,
			Email:     sess.UserEmail,
			Issuer:    sess.IssuerURL,
			ExpiresAt: sess.ExpiresAt,
			SignedIn:  true,
			Active:    active != "" && sess.ServerURL == active,
		})
	}

	sort.Slice(infos, func(i, j int) bool { return infos[i].ServerURL < infos[j].ServerURL })
	return infos, nil
}
