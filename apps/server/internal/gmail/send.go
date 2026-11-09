package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"strings"
	"time"

	gapi "google.golang.org/api/gmail/v1"
)

const (
	// ComposeScope allows creating and sending drafts (and sending messages).
	ComposeScope = "https://www.googleapis.com/auth/gmail.compose"
	// SendScope is the narrower send-only scope. It is requested alongside
	// readonly+compose only if Google rejects compose-only sends.
	SendScope = "https://www.googleapis.com/auth/gmail.send"
)

// WriteScopes returns the scopes a full read+send Gmail connection requests.
func WriteScopes() []string { return []string{ReadonlyScope, ComposeScope} }

// Profile is the authenticated mailbox identity.
type Profile struct {
	EmailAddress  string
	MessagesTotal int64
	ThreadsTotal  int64
	HistoryID     uint64
}

// OutgoingMessage is the provider-neutral send/draft input.
type OutgoingMessage struct {
	To       string
	Cc       string
	Bcc      string
	Subject  string
	Text     string
	HTML     string
	ThreadID string
}

// SentMessage is the result of a send.
type SentMessage struct {
	ID       string
	ThreadID string
	Labels   []string
}

// DraftRef is one draft listing entry.
type DraftRef struct {
	ID        string
	MessageID string
	ThreadID  string
}

// Profile returns the mailbox identity and current history id.
func (c *Client) Profile(ctx context.Context) (Profile, error) {
	var p *gapi.Profile
	err := c.call(ctx, func(callCtx context.Context) error {
		var callErr error
		p, callErr = c.svc.Users.GetProfile("me").Context(callCtx).Do()
		return callErr
	})
	if err != nil {
		return Profile{}, fmt.Errorf("gmail: get profile: %w", err)
	}
	return Profile{EmailAddress: p.EmailAddress, MessagesTotal: p.MessagesTotal, ThreadsTotal: p.ThreadsTotal, HistoryID: p.HistoryId}, nil
}

// SendMessage sends a message immediately.
func (c *Client) SendMessage(ctx context.Context, msg OutgoingMessage) (*SentMessage, error) {
	raw, err := buildRawMessage(msg)
	if err != nil {
		return nil, err
	}
	var sent *gapi.Message
	err = c.call(ctx, func(callCtx context.Context) error {
		var callErr error
		sent, callErr = c.svc.Users.Messages.Send("me", &gapi.Message{Raw: raw, ThreadId: msg.ThreadID}).Context(callCtx).Do()
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("gmail: send message: %w", err)
	}
	return &SentMessage{ID: sent.Id, ThreadID: sent.ThreadId, Labels: sent.LabelIds}, nil
}

// CreateDraft creates a draft.
func (c *Client) CreateDraft(ctx context.Context, msg OutgoingMessage) (*DraftRef, error) {
	raw, err := buildRawMessage(msg)
	if err != nil {
		return nil, err
	}
	var draft *gapi.Draft
	err = c.call(ctx, func(callCtx context.Context) error {
		var callErr error
		draft, callErr = c.svc.Users.Drafts.Create("me", &gapi.Draft{Message: &gapi.Message{Raw: raw, ThreadId: msg.ThreadID}}).Context(callCtx).Do()
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("gmail: create draft: %w", err)
	}
	return draftRef(draft), nil
}

// UpdateDraft replaces a draft's content.
func (c *Client) UpdateDraft(ctx context.Context, draftID string, msg OutgoingMessage) (*DraftRef, error) {
	if strings.TrimSpace(draftID) == "" {
		return nil, errors.New("gmail: draft id is required")
	}
	raw, err := buildRawMessage(msg)
	if err != nil {
		return nil, err
	}
	var draft *gapi.Draft
	err = c.call(ctx, func(callCtx context.Context) error {
		var callErr error
		draft, callErr = c.svc.Users.Drafts.Update("me", draftID, &gapi.Draft{Id: draftID, Message: &gapi.Message{Raw: raw, ThreadId: msg.ThreadID}}).Context(callCtx).Do()
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("gmail: update draft: %w", err)
	}
	return draftRef(draft), nil
}

// SendDraft sends an existing draft.
func (c *Client) SendDraft(ctx context.Context, draftID string) (*SentMessage, error) {
	if strings.TrimSpace(draftID) == "" {
		return nil, errors.New("gmail: draft id is required")
	}
	var sent *gapi.Message
	err := c.call(ctx, func(callCtx context.Context) error {
		var callErr error
		sent, callErr = c.svc.Users.Drafts.Send("me", &gapi.Draft{Id: draftID}).Context(callCtx).Do()
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("gmail: send draft: %w", err)
	}
	return &SentMessage{ID: sent.Id, ThreadID: sent.ThreadId, Labels: sent.LabelIds}, nil
}

// ListDrafts lists drafts, newest first.
func (c *Client) ListDrafts(ctx context.Context, max int64) ([]DraftRef, error) {
	if max <= 0 || max > 500 {
		max = 50
	}
	var resp *gapi.ListDraftsResponse
	err := c.call(ctx, func(callCtx context.Context) error {
		var callErr error
		resp, callErr = c.svc.Users.Drafts.List("me").MaxResults(max).Context(callCtx).Do()
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("gmail: list drafts: %w", err)
	}
	out := make([]DraftRef, 0, len(resp.Drafts))
	for _, d := range resp.Drafts {
		if d == nil || d.Id == "" {
			continue
		}
		out = append(out, *draftRef(d))
	}
	return out, nil
}

// DeleteDraft deletes a draft.
func (c *Client) DeleteDraft(ctx context.Context, draftID string) error {
	if strings.TrimSpace(draftID) == "" {
		return errors.New("gmail: draft id is required")
	}
	err := c.call(ctx, func(callCtx context.Context) error {
		return c.svc.Users.Drafts.Delete("me", draftID).Context(callCtx).Do()
	})
	if err != nil {
		return fmt.Errorf("gmail: delete draft: %w", err)
	}
	return nil
}

func draftRef(d *gapi.Draft) *DraftRef {
	out := &DraftRef{ID: d.Id}
	if d.Message != nil {
		out.MessageID = d.Message.Id
		out.ThreadID = d.Message.ThreadId
	}
	return out
}

func buildRawMessage(msg OutgoingMessage) (string, error) {
	if strings.TrimSpace(msg.To) == "" && strings.TrimSpace(msg.Cc) == "" && strings.TrimSpace(msg.Bcc) == "" {
		return "", errors.New("gmail: at least one recipient is required")
	}
	subject := mime.QEncoding.Encode("utf-8", msg.Subject)
	if strings.TrimSpace(msg.Subject) == "" {
		subject = ""
	}

	var body bytes.Buffer
	contentType := "text/plain; charset=UTF-8"
	switch {
	case strings.TrimSpace(msg.HTML) != "" && strings.TrimSpace(msg.Text) != "":
		boundary := fmt.Sprintf("memory-%d", time.Now().UnixNano())
		contentType = "multipart/alternative; boundary=" + boundary
		fmt.Fprintf(&body, "--%s\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", boundary, msg.Text)
		fmt.Fprintf(&body, "--%s\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", boundary, msg.HTML)
		fmt.Fprintf(&body, "--%s--\r\n", boundary)
	case strings.TrimSpace(msg.HTML) != "":
		contentType = "text/html; charset=UTF-8"
		body.WriteString(msg.HTML)
	default:
		body.WriteString(msg.Text)
	}

	var b strings.Builder
	if v := strings.TrimSpace(msg.To); v != "" {
		b.WriteString("To: " + v + "\r\n")
	}
	if v := strings.TrimSpace(msg.Cc); v != "" {
		b.WriteString("Cc: " + v + "\r\n")
	}
	if v := strings.TrimSpace(msg.Bcc); v != "" {
		b.WriteString("Bcc: " + v + "\r\n")
	}
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: " + contentType + "\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.Write(body.Bytes())

	return base64.RawURLEncoding.EncodeToString([]byte(b.String())), nil
}
