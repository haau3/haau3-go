//go:build unix

package haau3

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A named pipe called .env is never opened, so it cannot hold the program.
func TestANamedPipeCalledEnvIsNeverWaitedOn(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, ".env"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if got := unloadedEnvFile(dir); got != "" {
		t.Errorf("found %q in a named pipe", got)
	}
	if time.Since(started) > time.Second {
		t.Error("waited on a named pipe")
	}
}
