// Package fixture creates realistic but minimal fake project workspaces for tests.
// The workspace is a temporary directory initialized as a git repo with a small set
// of files that an agent can discover: a README, a few source files, and an AGENTS.md.
// The content is designed so that the emergent-onboard skill can identify ~3 object
// types (Service, Endpoint, Model) and ~2 relationship types (calls, depends_on).
package fixture

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Workspace is a temporary git repository with fake project files.
type Workspace struct {
	// Dir is the absolute path to the workspace root.
	Dir string
	t   *testing.T
}

// NewWorkspace creates a temporary directory, writes fake project files into it,
// and initialises it as a git repo with an initial commit.
// The directory is automatically removed when the test finishes.
func NewWorkspace(t *testing.T) *Workspace {
	t.Helper()

	dir, err := os.MkdirTemp("", "oc-test-workspace-*")
	if err != nil {
		t.Fatalf("fixture: create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	w := &Workspace{Dir: dir, t: t}
	w.writeFiles()
	w.gitInit()
	return w
}

// writeFiles writes the fake project source tree.
func (w *Workspace) writeFiles() {
	w.t.Helper()

	files := map[string]string{
		// opencode.json: auto-allow all tool permissions so the agent never blocks
		// waiting for interactive approval during non-interactive test runs.
		"opencode.json": `{
  "$schema": "https://opencode.ai/config.json",
  "permission": {
    "read":     "allow",
    "write":    "allow",
    "bash":     "allow",
    "edit":     "allow",
    "skill":    "allow",
    "question": "allow"
  }
}
`,
		"README.md": `# Bookstore API

A small REST API for managing a bookstore inventory.

## Services
- **catalog-service** — manages book records (title, author, ISBN, price)
- **order-service** — handles customer orders, calls catalog-service to verify stock
- **notification-service** — sends email/SMS notifications, called by order-service

## Quick start
` + "```" + `bash
go run ./cmd/catalog
go run ./cmd/orders
` + "```" + `
`,

		"AGENTS.md": `# Bookstore API — Agent Context

## Architecture
Three Go microservices communicating over HTTP.

## Object types to model
- Service: catalog-service, order-service, notification-service
- Endpoint: each HTTP route exposed by a service
- Model: data structures (Book, Order, Customer)

## Key relationships
- order-service **calls** catalog-service (GET /books/:isbn)
- order-service **calls** notification-service (POST /notify)
- Endpoint **belongs_to** Service
- Order **references** Book
`,

		"cmd/catalog/main.go": `package main

import (
	"encoding/json"
	"net/http"
)

// Book represents a catalog entry.
type Book struct {
	ISBN   string  ` + "`json:\"isbn\"`" + `
	Title  string  ` + "`json:\"title\"`" + `
	Author string  ` + "`json:\"author\"`" + `
	Price  float64 ` + "`json:\"price\"`" + `
	Stock  int     ` + "`json:\"stock\"`" + `
}

func main() {
	http.HandleFunc("GET /books", listBooks)
	http.HandleFunc("GET /books/{isbn}", getBook)
	http.HandleFunc("POST /books", createBook)
	http.ListenAndServe(":8080", nil)
}

func listBooks(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode([]Book{})
}

func getBook(w http.ResponseWriter, r *http.Request) {
	isbn := r.PathValue("isbn")
	_ = isbn
	json.NewEncoder(w).Encode(Book{})
}

func createBook(w http.ResponseWriter, r *http.Request) {
	var b Book
	json.NewDecoder(r.Body).Decode(&b)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(b)
}
`,

		"cmd/orders/main.go": `package main

import (
	"encoding/json"
	"net/http"
)

// Order represents a customer order.
type Order struct {
	ID       string ` + "`json:\"id\"`" + `
	ISBN     string ` + "`json:\"isbn\"`" + `
	Customer string ` + "`json:\"customer\"`" + `
	Quantity int    ` + "`json:\"quantity\"`" + `
	Status   string ` + "`json:\"status\"`" + `
}

func main() {
	http.HandleFunc("POST /orders", createOrder)
	http.HandleFunc("GET /orders/{id}", getOrder)
	http.ListenAndServe(":8081", nil)
}

func createOrder(w http.ResponseWriter, r *http.Request) {
	var o Order
	json.NewDecoder(r.Body).Decode(&o)

	// Verify stock with catalog-service
	resp, err := http.Get("http://catalog-service:8080/books/" + o.ISBN)
	if err != nil || resp.StatusCode != 200 {
		http.Error(w, "book not found", http.StatusNotFound)
		return
	}

	// Notify via notification-service
	http.Post("http://notification-service:8082/notify", "application/json", r.Body)

	o.Status = "placed"
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(o)
}

func getOrder(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(Order{Status: "placed"})
}
`,

		"cmd/notify/main.go": `package main

import (
	"encoding/json"
	"net/http"
)

// Notification is a send request.
type Notification struct {
	Customer string ` + "`json:\"customer\"`" + `
	Message  string ` + "`json:\"message\"`" + `
	Channel  string ` + "`json:\"channel\"`" + ` // email or sms
}

func main() {
	http.HandleFunc("POST /notify", sendNotification)
	http.ListenAndServe(":8082", nil)
}

func sendNotification(w http.ResponseWriter, r *http.Request) {
	var n Notification
	json.NewDecoder(r.Body).Decode(&n)
	// send...
	json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
}
`,
	}

	for relPath, content := range files {
		full := filepath.Join(w.Dir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			w.t.Fatalf("fixture: mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			w.t.Fatalf("fixture: write %s: %v", relPath, err)
		}
	}
}

// gitInit initialises a git repo and makes an initial commit.
func (w *Workspace) gitInit() {
	w.t.Helper()

	cmds := [][]string{
		{"git", "init", "-q"},
		{"git", "config", "user.email", "test@emergent.test"},
		{"git", "config", "user.name", "Test"},
		{"git", "add", "."},
		{"git", "commit", "-q", "-m", "initial"},
	}
	for _, args := range cmds {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = w.Dir
		if out, err := c.CombinedOutput(); err != nil {
			w.t.Fatalf("fixture: git %v: %v\n%s", args[1:], err, out)
		}
	}
}

// EnvFile writes a .env.local file into the workspace with EMERGENT_SERVER_URL,
// EMERGENT_PROJECT_ID, and EMERGENT_PROJECT_TOKEN. The CLI auto-loads .env.local
// from the current directory, so any emergent command run from the workspace root
// will be authenticated and project-scoped without needing --project-token flags.
func (w *Workspace) EnvFile(serverURL, projectID, projectToken string) string {
	path := filepath.Join(w.Dir, ".env.local")
	content := fmt.Sprintf("EMERGENT_SERVER_URL=%s\nEMERGENT_PROJECT_ID=%s\n", serverURL, projectID)
	if projectToken != "" {
		content += fmt.Sprintf("EMERGENT_PROJECT_TOKEN=%s\n", projectToken)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		w.t.Fatalf("fixture: write .env.local: %v", err)
	}
	return path
}
