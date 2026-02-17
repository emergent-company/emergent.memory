// Package main implements the in-VM agent for Firecracker microVMs.
//
// This is a lightweight HTTP server that runs inside the microVM and handles
// tool operations (exec, read, write, list files) on behalf of the host-side
// Firecracker provider. It listens on port 8080 and exposes a simple REST API.
//
// The agent is compiled as a static binary and embedded into the microVM rootfs.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	listenAddr       = ":8080"
	maxOutputBytes   = 50 * 1024        // 50KB output limit
	maxFileReadBytes = 10 * 1024 * 1024 // 10MB max file read for microVM memory safety
	defaultTimeout   = 120 * time.Second
)

// ExecRequest matches the workspace ExecRequest type.
type ExecRequest struct {
	Command   string `json:"command"`
	Workdir   string `json:"workdir,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

// ExecResult matches the workspace ExecResult type.
type ExecResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated,omitempty"`
}

// FileReadRequest matches the workspace FileReadRequest type.
type FileReadRequest struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// FileReadResult matches the workspace FileReadResult type.
type FileReadResult struct {
	Content    string `json:"content"`
	IsDir      bool   `json:"is_dir,omitempty"`
	TotalLines int    `json:"total_lines,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
	IsBinary   bool   `json:"is_binary,omitempty"`
}

// FileWriteRequest matches the workspace FileWriteRequest type.
type FileWriteRequest struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

// FileListRequest matches the workspace FileListRequest type.
type FileListRequest struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

// FileInfo matches the workspace FileInfo type.
type FileInfo struct {
	Path       string    `json:"path"`
	IsDir      bool      `json:"is_dir,omitempty"`
	Size       int64     `json:"size,omitempty"`
	ModifiedAt time.Time `json:"modified_at,omitempty"`
}

// FileListResult matches the workspace FileListResult type.
type FileListResult struct {
	Files []FileInfo `json:"files"`
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/exec", handleExec)
	mux.HandleFunc("/read", handleReadFile)
	mux.HandleFunc("/write", handleWriteFile)
	mux.HandleFunc("/list", handleListFiles)

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 300 * time.Second, // Long timeout for exec operations
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		log.Println("shutting down vm-agent")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	log.Printf("vm-agent listening on %s", listenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"pid":    os.Getpid(),
	})
}

func handleExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	timeout := defaultTimeout
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	start := time.Now()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", req.Command)
	if req.Workdir != "" {
		cmd.Dir = req.Workdir
	} else {
		cmd.Dir = "/workspace"
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() != nil {
			exitCode = -1
		} else {
			exitCode = -1
		}
	}

	stdout := stdoutBuf.String()
	truncated := false
	if len(stdout) > maxOutputBytes {
		stdout = stdout[:maxOutputBytes]
		truncated = true
	}

	result := ExecResult{
		Stdout:     stdout,
		Stderr:     stderrBuf.String(),
		ExitCode:   exitCode,
		DurationMs: time.Since(start).Milliseconds(),
		Truncated:  truncated,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func handleReadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req FileReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	info, err := os.Stat(req.FilePath)
	if err != nil {
		http.Error(w, "file not found: "+req.FilePath, http.StatusNotFound)
		return
	}

	// Directory listing
	if info.IsDir() {
		entries, err := os.ReadDir(req.FilePath)
		if err != nil {
			http.Error(w, "failed to read directory: "+err.Error(), http.StatusInternalServerError)
			return
		}
		var sb strings.Builder
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() {
				name += "/"
			}
			sb.WriteString(name)
			sb.WriteString("\n")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(FileReadResult{
			Content: sb.String(),
			IsDir:   true,
		})
		return
	}

	// Check binary
	if isBinaryFile(req.FilePath) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(FileReadResult{
			IsBinary: true,
			Content:  "Binary file",
			FileSize: info.Size(),
		})
		return
	}

	// Guard against reading very large files into memory (microVM has limited RAM)
	if info.Size() > maxFileReadBytes {
		http.Error(w, "file too large: exceeds 10MB limit", http.StatusRequestEntityTooLarge)
		return
	}

	// Read text file
	data, err := os.ReadFile(req.FilePath)
	if err != nil {
		http.Error(w, "failed to read file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)
	// Remove trailing empty line from split
	if totalLines > 0 && lines[totalLines-1] == "" {
		lines = lines[:totalLines-1]
		totalLines = len(lines)
	}

	// Apply offset and limit (1-indexed)
	startIdx := 0
	if req.Offset > 0 {
		startIdx = req.Offset - 1
	}
	if startIdx > len(lines) {
		startIdx = len(lines)
	}

	endIdx := len(lines)
	if req.Limit > 0 && startIdx+req.Limit < endIdx {
		endIdx = startIdx + req.Limit
	}

	// Format with line numbers
	var sb strings.Builder
	for i := startIdx; i < endIdx; i++ {
		sb.WriteString(strconv.Itoa(i+1) + ": " + lines[i] + "\n")
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(FileReadResult{
		Content:    sb.String(),
		TotalLines: totalLines,
		FileSize:   info.Size(),
	})
}

func handleWriteFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req FileWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Create parent directories
	dir := filepath.Dir(req.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, "failed to create directories: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(req.FilePath, []byte(req.Content), 0644); err != nil {
		http.Error(w, "failed to write file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func handleListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req FileListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	searchPath := req.Path
	if searchPath == "" {
		searchPath = "/workspace"
	}

	var files []FileInfo

	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("walk error at %s: %v", path, err)
			return nil // skip errors but log them
		}

		// Match pattern against base name
		matched, matchErr := filepath.Match(req.Pattern, info.Name())
		if matchErr != nil {
			log.Printf("pattern match error for %s: %v", info.Name(), matchErr)
			return nil
		}
		if !matched {
			return nil
		}

		files = append(files, FileInfo{
			Path:       path,
			IsDir:      info.IsDir(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime(),
		})

		return nil
	})

	if err != nil {
		http.Error(w, "failed to list files: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if files == nil {
		files = []FileInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(FileListResult{Files: files})
}

// isBinaryFile checks if a file appears to be binary by reading first 512 bytes.
func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false
	}
	buf = buf[:n]

	// Check for null bytes (common indicator of binary)
	for _, b := range buf {
		if b == 0 {
			return true
		}
	}

	// Check content type
	contentType := http.DetectContentType(buf)
	return !strings.HasPrefix(contentType, "text/") &&
		contentType != "application/json" &&
		contentType != "application/xml"
}

func init() {
	// Ensure /workspace exists
	_ = os.MkdirAll("/workspace", 0755)

	// Log to stderr for debugging
	log.SetOutput(os.Stderr)
	log.SetPrefix("[vm-agent] ")
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}
