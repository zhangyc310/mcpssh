package main

import (
	"strings"
	"testing"
	"time"
	"os/exec"
	"github.com/creack/pty"
)

func TestSessionLocalInteraction(t *testing.T) {
	// This test starts a local shell session to simulate SSH interaction
	// and verifies read/write buffer logic.

	// 1. Setup
	cmd := exec.Command("/bin/sh")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Skipf("Skipping PTY test: %v", err) // Skip if environment doesn't support PTY
	}
	
	sess := &Session{
		ID:   "test-session",
		Cmd:  cmd,
		Ptmx: ptmx,
		done: make(chan struct{}),
	}
	go sess.startReader()
	defer func() {
		close(sess.done)
		sess.Ptmx.Close()
		sess.Cmd.Process.Kill()
	}()

	// 2. Consume initial prompt (if any)
	time.Sleep(500 * time.Millisecond)
	_ = sess.ReadAndClear()

	// 3. Send Command
	input := "echo HelloGemini\n"
	_, err = sess.Ptmx.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// 4. Wait and Read
	time.Sleep(500 * time.Millisecond)
	output := sess.ReadAndClear()

	// 5. Verify
	if !strings.Contains(output, "HelloGemini") {
		t.Errorf("Expected output to contain 'HelloGemini', got:\n%s", output)
	}
}
