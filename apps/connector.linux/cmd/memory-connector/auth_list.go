package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/account"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
)

// authListAccount is one entry in the `auth list --json` document.
type authListAccount struct {
	ServerURL string `json:"server_url"`
	Email     string `json:"email,omitempty"`
	Issuer    string `json:"issuer,omitempty"`
	SignedIn  bool   `json:"signed_in"`
	Active    bool   `json:"active"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// authListDoc is the machine-readable `auth list --json` document.
type authListDoc struct {
	SchemaVersion int               `json:"schema_version"`
	Accounts      []authListAccount `json:"accounts"`
}

// runAuthList enumerates the servers this connector has stored sessions for.
// An empty list is a valid, non-error result (exit 0).
func runAuthList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("auth list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth list: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	infos, err := newAuthManager(account.BaseDirForConfig(*configPath)).Accounts()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector auth list: %v\n", err)
		return 1
	}

	if *jsonOut {
		doc := authListDoc{SchemaVersion: authSchemaVersion, Accounts: make([]authListAccount, 0, len(infos))}
		for _, info := range infos {
			entry := authListAccount{
				ServerURL: info.ServerURL,
				Email:     info.Email,
				Issuer:    info.Issuer,
				SignedIn:  info.SignedIn,
				Active:    info.Active,
			}
			if !info.ExpiresAt.IsZero() {
				entry.ExpiresAt = info.ExpiresAt.UTC().Format(time.RFC3339)
			}
			doc.Accounts = append(doc.Accounts, entry)
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector auth list: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(append(b, '\n'))
		return 0
	}

	printAuthListText(stdout, infos)
	return 0
}

// printAuthListText renders one line per account:
// `<server>  <email-or->  <active-or->  <expiry-or->`.
func printAuthListText(w io.Writer, infos []account.AccountInfo) {
	_, _ = fmt.Fprintln(w, "memory-connector auth list")
	if len(infos) == 0 {
		_, _ = fmt.Fprintln(w, "no accounts")
		return
	}
	for _, info := range infos {
		email := info.Email
		if email == "" {
			email = "-"
		}
		active := "-"
		if info.Active {
			active = "active"
		}
		expiry := "-"
		if !info.ExpiresAt.IsZero() {
			expiry = info.ExpiresAt.UTC().Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(w, "%s  %s  %s  %s\n", info.ServerURL, email, active, expiry)
	}
}
