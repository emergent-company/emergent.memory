// Package dockertests — bookstore_fixture.go
//
// Self-contained bookstore workspace fixture for Docker CLI tests.
// Mirrors the fixture in tools/opencode-test-suite/internal/fixture/workspace.go
// but lives here so this module has no cross-module dependencies.
package dockertests

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// bookstoreWorkspace is a temporary git repo containing a minimal three-service
// bookstore API.  It is the canonical test project used across all e2e test suites.
type bookstoreWorkspace struct {
	// Dir is the absolute path to the workspace root.
	Dir string
	t   *testing.T
}

// newBookstoreWorkspace creates a temp directory, writes the bookstore project
// files into it (including opencode.json with auto-allow permissions), and
// runs `git init` + initial commit.
// The directory is removed automatically when the test finishes.
func newBookstoreWorkspace(t *testing.T) *bookstoreWorkspace {
	t.Helper()

	dir, err := os.MkdirTemp("", "emergent-cli-test-workspace-*")
	if err != nil {
		t.Fatalf("bookstore fixture: create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	ws := &bookstoreWorkspace{Dir: dir, t: t}
	ws.writeFiles()
	ws.gitInit()
	return ws
}

// writeEnvLocal writes .env.local into the workspace so that the emergent CLI
// and opencode auto-authenticate and scope all calls to this project.
func (ws *bookstoreWorkspace) writeEnvLocal(serverURL, projectID, projectToken string) {
	ws.t.Helper()

	content := fmt.Sprintf("MEMORY_SERVER_URL=%s\nMEMORY_PROJECT_ID=%s\n", serverURL, projectID)
	if projectToken != "" {
		content += fmt.Sprintf("MEMORY_PROJECT_TOKEN=%s\n", projectToken)
	}
	path := filepath.Join(ws.Dir, ".env.local")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		ws.t.Fatalf("bookstore fixture: write .env.local: %v", err)
	}
}

// writeFiles writes the canonical bookstore project file tree.
func (ws *bookstoreWorkspace) writeFiles() {
	ws.t.Helper()

	files := map[string]string{
		// opencode.json — auto-allow all tool permissions so the agent never
		// blocks waiting for interactive approval during non-interactive test runs.
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

func listBooks(w http.ResponseWriter, r *http.Request)  { json.NewEncoder(w).Encode([]Book{}) }
func getBook(w http.ResponseWriter, r *http.Request)    { json.NewEncoder(w).Encode(Book{}) }
func createBook(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusCreated) }
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
	json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
}
`,
	}

	for relPath, content := range files {
		full := filepath.Join(ws.Dir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			ws.t.Fatalf("bookstore fixture: mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			ws.t.Fatalf("bookstore fixture: write %s: %v", relPath, err)
		}
	}
}

// gitInit initialises a git repo and makes an initial commit.
func (ws *bookstoreWorkspace) gitInit() {
	ws.t.Helper()

	cmds := [][]string{
		{"git", "init", "-q"},
		{"git", "config", "user.email", "test@emergent.test"},
		{"git", "config", "user.name", "Test"},
		{"git", "add", "."},
		{"git", "commit", "-q", "-m", "initial"},
	}
	for _, args := range cmds {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = ws.Dir
		if out, err := c.CombinedOutput(); err != nil {
			ws.t.Fatalf("bookstore fixture: git %v: %v\n%s", args[1:], err, out)
		}
	}
}
