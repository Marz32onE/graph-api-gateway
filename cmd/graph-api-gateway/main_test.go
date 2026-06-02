package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marz32one/graph-api-gateway/internal/auth"
	"github.com/marz32one/graph-api-gateway/internal/config"
)

// discardLogger returns a slog logger that writes nowhere, so tests stay quiet.
func discardLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// writeKeyFile creates a temp file containing the given lines and returns its
// path. The file is cleaned up automatically with the test.
func writeKeyFile(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api-keys.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return path
}

func TestLoadAPIKeys_NoSourceReturnsEmptyEnabledSet(t *testing.T) {
	cfg := &config.Config{} // neither APIKeys nor APIKeysFile set
	ks, err := loadAPIKeys(cfg, discardLogger(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ks == nil {
		t.Fatal("want non-nil KeySet when auth is disabled, got nil")
	}
	if !ks.Empty() {
		t.Errorf("want Empty()==true when no key source is configured, got false")
	}
	if got := ks.Snapshot(); got != 0 {
		t.Errorf("Snapshot: want 0 keys, got %d", got)
	}
}

func TestLoadAPIKeys_CSVKeysValidate(t *testing.T) {
	cfg := &config.Config{APIKeys: "k1,k2"}
	ks, err := loadAPIKeys(cfg, discardLogger(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := ks.Snapshot(); got != 2 {
		t.Fatalf("Snapshot: want 2 keys from %q, got %d", cfg.APIKeys, got)
	}
	if !ks.Validate("k1") {
		t.Errorf("Validate(%q): want true", "k1")
	}
	if !ks.Validate("k2") {
		t.Errorf("Validate(%q): want true", "k2")
	}
	if ks.Validate("nope") {
		t.Errorf("Validate(%q): want false for a key not in the CSV", "nope")
	}
}

func TestLoadAPIKeys_FileKeysValidate(t *testing.T) {
	path := writeKeyFile(t, "filekey1", "filekey2")
	cfg := &config.Config{APIKeysFile: path}
	ks, err := loadAPIKeys(cfg, discardLogger(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := ks.Snapshot(); got != 2 {
		t.Fatalf("Snapshot: want 2 keys loaded from file, got %d", got)
	}
	if !ks.Validate("filekey1") {
		t.Errorf("Validate(%q): want true for a file-loaded key", "filekey1")
	}
	if !ks.Validate("filekey2") {
		t.Errorf("Validate(%q): want true for a file-loaded key", "filekey2")
	}
}

func TestLoadAPIKeys_FileTakesPrecedenceOverCSV(t *testing.T) {
	path := writeKeyFile(t, "fileonly")
	cfg := &config.Config{APIKeysFile: path, APIKeys: "csvonly"}
	ks, err := loadAPIKeys(cfg, discardLogger(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ks.Validate("fileonly") {
		t.Errorf("Validate(%q): want true — file keys must win when both sources set", "fileonly")
	}
	if ks.Validate("csvonly") {
		t.Errorf("Validate(%q): want false — CSV must be ignored when APIKeysFile is set", "csvonly")
	}
	if got := ks.Snapshot(); got != 1 {
		t.Errorf("Snapshot: want 1 (file only), got %d", got)
	}
}

func TestLoadAPIKeys_MissingFileReturnsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")
	cfg := &config.Config{APIKeysFile: missing}
	ks, err := loadAPIKeys(cfg, discardLogger(t))
	if err == nil {
		t.Fatalf("want error for a non-existent key file, got nil (ks=%v)", ks)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q should reference the offending path %q", err.Error(), missing)
	}
}

func TestReloadAPIKeys_PicksUpFileChanges(t *testing.T) {
	path := writeKeyFile(t, "old")

	ks := auth.NewKeySet()
	if err := ks.LoadFile(path); err != nil {
		t.Fatalf("initial LoadFile: %v", err)
	}
	if !ks.Validate("old") {
		t.Fatalf("precondition: Validate(%q) want true", "old")
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan struct{})
	go func() {
		defer close(done)
		reloadAPIKeys(ctx, ks, path, 20*time.Millisecond, discardLogger(t))
	}()

	// Rotate the secret on disk.
	if err := os.WriteFile(path, []byte("new\n"), 0o600); err != nil {
		t.Fatalf("rewrite key file: %v", err)
	}

	// Poll until the reloader observes the new key, with a hard deadline.
	deadline := time.After(2 * time.Second)
	for !ks.Validate("new") {
		select {
		case <-deadline:
			t.Fatalf("reloadAPIKeys did not pick up %q within deadline", "new")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// After the rotation, the old key must no longer be accepted (atomic swap,
	// not a merge).
	if ks.Validate("old") {
		t.Errorf("Validate(%q): want false after the file was rewritten", "old")
	}

	// Cancelling the context must stop the goroutine; it should return promptly.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reloadAPIKeys did not return after context cancellation")
	}
}
