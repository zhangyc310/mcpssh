package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Session represents a running SSH (or shell) process
type Session struct {
	ID        string
	Cmd       *exec.Cmd
	Ptmx      *os.File
	CreatedAt time.Time

	// Output buffering
	outputBuf bytes.Buffer
	bufMu     sync.Mutex
	done      chan struct{}
	exited    chan struct{}
}

// SessionManager manages multiple sessions
type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

var manager = &SessionManager{
	sessions: make(map[string]*Session),
}

func main() {
	s := server.NewMCPServer("SSH-Session-Manager", "2.0.0")

	// Tool: Start Session
	s.AddTool(mcp.NewTool("start_session",
		mcp.WithDescription("Start a new SSH session (or shell command). Returns a session_id. Provide the SSH host alias or destination directly."),
		mcp.WithString("host", mcp.Required(), mcp.Description("SSH host alias (e.g. from ~/.ssh/config) or valid SSH destination. Use 'local' to run a local shell.")),
	), startSessionHandler)

	// Tool: Interact Session
	s.AddTool(mcp.NewTool("interact_session",
		mcp.WithDescription("Write input to the session and/or read pending output."),
		mcp.WithString("session_id", mcp.Required()),
		mcp.WithString("input", mcp.Description("Command or text to send to the terminal (e.g. 'ls -la\n'). Optional.")),
		mcp.WithString("wait_duration", mcp.Description("Time to wait for output after sending input (in seconds). Default 0.5s. Set higher for slow commands.")),
	), interactSessionHandler)

	// Tool: Close Session
	s.AddTool(mcp.NewTool("close_session",
		mcp.WithDescription("Terminate a session."),
		mcp.WithString("session_id", mcp.Required()),
	), closeSessionHandler)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
	}
}

// --- Logic Implementation ---

func (sm *SessionManager) Add(sess *Session) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessions[sess.ID] = sess
}

func (sm *SessionManager) Get(id string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	sess, ok := sm.sessions[id]
	return sess, ok
}

func (sm *SessionManager) Remove(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sess, ok := sm.sessions[id]; ok {
		close(sess.done) // Stop the reader
		sess.Ptmx.Close()
		if sess.Cmd.Process != nil {
			sess.Cmd.Process.Kill()
		}
		delete(sm.sessions, id)
	}
}

// startReader constantly reads from PTY and appends to buffer
func (s *Session) startReader() {
	buf := make([]byte, 8192)
	defer close(s.exited) // Signal that process exited

	for {
		select {
		case <-s.done:
			return
		default:
			n, err := s.Ptmx.Read(buf)
			if n > 0 {
				s.bufMu.Lock()
				s.outputBuf.Write(buf[:n])
				s.bufMu.Unlock()
			}
			if err != nil {
				if err != io.EOF {
					// Log error if needed, or just exit
				}
				return
			}
		}
	}
}

// ReadAndClear returns the current buffer content and clears it.
func (s *Session) ReadAndClear() string {
	s.bufMu.Lock()
	defer s.bufMu.Unlock()
	out := s.outputBuf.String()
	s.outputBuf.Reset()
	return out
}

// --- Handlers ---

func startSessionHandler(ctx context.Context, args mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	host := args.GetString("host", "")
	if host == "" {
		return mcp.NewToolResultError("Host argument is required"), nil
	}

	var c *exec.Cmd
	if host == "local" {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}
		c = exec.Command(shell)
	} else {
		// Use -tt to force PTY, BatchMode to fail fast on auth issues
		c = exec.Command("ssh", "-tt", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", host)
	}

	// Start PTY
	ptmx, err := pty.Start(c)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to start session: %v", err)), nil
	}

	// Create Session
	sessID := uuid.New().String()
	sess := &Session{
		ID:        sessID,
		Cmd:       c,
		Ptmx:      ptmx,
		CreatedAt: time.Now(),
		done:      make(chan struct{}),
		exited:    make(chan struct{}),
	}

	// Start background reader
	go sess.startReader()

	manager.Add(sess)

	// Wait a bit for initial banner/login output
	time.Sleep(1 * time.Second)
	initialOutput := sess.ReadAndClear()

	// Check if process exited immediately (e.g. connection error)
	select {
	case <-sess.exited:
		manager.Remove(sessID)
		return mcp.NewToolResultError(fmt.Sprintf("Session started but exited immediately (SSH error?):\n%s", initialOutput)), nil
	default:
		// Session is healthy
	}

	return mcp.NewToolResultText(fmt.Sprintf("Session started. ID: %s\n\nOutput:\n%s", sessID, initialOutput)), nil
}

func interactSessionHandler(ctx context.Context, args mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessID := args.GetString("session_id", "")
	input := args.GetString("input", "")
	waitSecStr := args.GetString("wait_duration", "0.5")

	sess, ok := manager.Get(sessID)
	if !ok {
		return mcp.NewToolResultError("Session not found"), nil
	}

	// Check if process is alive
	select {
	case <-sess.exited:
		output := sess.ReadAndClear() // Read any remaining output
		manager.Remove(sessID)        // Cleanup
		return mcp.NewToolResultText(fmt.Sprintf("[Session exited]\nRemaining Output:\n%s", output)), nil
	default:
	}

	if input != "" {
		_, err := sess.Ptmx.Write([]byte(input))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Write error: %v", err)), nil
		}
	}

	// Parse wait duration
	waitDuration, err := time.ParseDuration(waitSecStr + "s")
	if err != nil {
		waitDuration = 500 * time.Millisecond
	}
	time.Sleep(waitDuration)

	output := sess.ReadAndClear()
	if output == "" && input == "" {
		output = "(No new output)"
	}

	return mcp.NewToolResultText(output), nil
}

func closeSessionHandler(ctx context.Context, args mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessID := args.GetString("session_id", "")
	manager.Remove(sessID)
	return mcp.NewToolResultText("Session closed"), nil
}