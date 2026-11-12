//go:build unix

package haau3

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// asked runs unloadedEnvFile on dir in a goroutine, so a check that blocks fails the test rather than
// holding the suite.
func asked(t *testing.T, dir string) string {
	t.Helper()
	found := make(chan string, 1)
	go func() { found <- unloadedEnvFile(dir) }()
	select {
	case got := <-found:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("waited on .env")
		return ""
	}
}

// A named pipe called .env is never opened: password managers can serve .env as one, where an open counts
// as a read, or asks the person to approve one.
func TestANamedPipeCalledEnvIsNeverOpened(t *testing.T) {
	dir := t.TempDir()
	pipe := filepath.Join(dir, ".env")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	// A writer waits until a reader opens the pipe.
	opened := make(chan struct{})
	go func() {
		if f, err := os.OpenFile(pipe, os.O_WRONLY, 0); err == nil {
			close(opened)
			_ = f.Close()
		}
	}()
	time.Sleep(50 * time.Millisecond)
	if got := asked(t, dir); got != "" {
		t.Errorf("found %q in a named pipe", got)
	}
	select {
	case <-opened:
		t.Error("the pipe was opened: its writer was let through")
	case <-time.After(200 * time.Millisecond):
	}
	// Let the writer go, as the reader it waits for.
	if f, err := os.OpenFile(pipe, os.O_RDONLY|syscall.O_NONBLOCK, 0); err == nil {
		<-opened
		_ = f.Close()
	}
}

// A pipe with no writer is not waited on either.
func TestANamedPipeWithNoWriterIsNotWaitedOn(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, ".env"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := asked(t, dir); got != "" {
		t.Errorf("found %q in a named pipe", got)
	}
}
