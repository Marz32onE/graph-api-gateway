package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeKeyFile writes content to a fresh file inside t.TempDir and returns its
// path.
func writeKeyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "keys.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return path
}

func TestNewKeySetIsAuthDisabled(t *testing.T) {
	ks := NewKeySet()

	if !ks.Empty() {
		t.Fatalf("Empty() = false, want true for a fresh KeySet")
	}
	if got := ks.Snapshot(); got != 0 {
		t.Fatalf("Snapshot() = %d, want 0 for a fresh KeySet", got)
	}
	if ks.Validate("anything") {
		t.Fatalf("Validate(%q) = true, want false when auth disabled", "anything")
	}
	if ks.Validate("") {
		t.Fatalf("Validate(\"\") = true, want false when auth disabled")
	}
}

func TestLoadCSVTrimsDropsBlanksAndDedupes(t *testing.T) {
	ks := NewKeySet()
	ks.LoadCSV("a, b ,,a,c")

	if ks.Empty() {
		t.Fatalf("Empty() = true after LoadCSV, want false")
	}
	if got := ks.Snapshot(); got != 3 {
		t.Fatalf("Snapshot() = %d after LoadCSV(%q), want 3 (a,b,c)", got, "a, b ,,a,c")
	}

	// Each loaded key validates true...
	for _, k := range []string{"a", "b", "c"} {
		if !ks.Validate(k) {
			t.Errorf("Validate(%q) = false, want true (key was loaded)", k)
		}
	}
	// ...and trimming means the un-trimmed form is NOT a stored key.
	for _, k := range []string{" b ", " b", "b ", "", "d"} {
		if ks.Validate(k) {
			t.Errorf("Validate(%q) = true, want false (not a stored key)", k)
		}
	}
}

func TestLoadCSVEmptyStringDisablesAuth(t *testing.T) {
	ks := NewKeySet()
	ks.LoadCSV("a,b")
	if ks.Empty() {
		t.Fatalf("precondition: Empty() = true after loading keys")
	}

	// An all-blank CSV atomically swaps in an empty set, re-disabling auth.
	ks.LoadCSV("  , ,")

	if !ks.Empty() {
		t.Fatalf("Empty() = false after LoadCSV of only-blanks, want true")
	}
	if got := ks.Snapshot(); got != 0 {
		t.Fatalf("Snapshot() = %d after LoadCSV of only-blanks, want 0", got)
	}
	if ks.Validate("a") {
		t.Fatalf("Validate(%q) = true after auth re-disabled, want false", "a")
	}
}

func TestLoadFileParsesStripsCommentsBlanksAndDedupes(t *testing.T) {
	// One key per line: blank lines, a comment, surrounding whitespace, and a
	// duplicate (k1 appears twice) must all be collapsed.
	content := strings.Join([]string{
		"",
		"  k1  ",
		"# this is a comment",
		"k2",
		"",
		"   ",
		"k1",
		"#k3-looks-like-comment",
		"k3",
	}, "\n")
	path := writeKeyFile(t, content)

	ks := NewKeySet()
	if err := ks.LoadFile(path); err != nil {
		t.Fatalf("LoadFile: unexpected error: %v", err)
	}

	if got := ks.Snapshot(); got != 3 {
		t.Fatalf("Snapshot() = %d after LoadFile, want 3 (k1,k2,k3)", got)
	}
	if ks.Empty() {
		t.Fatalf("Empty() = true after LoadFile, want false")
	}

	for _, k := range []string{"k1", "k2", "k3"} {
		if !ks.Validate(k) {
			t.Errorf("Validate(%q) = false, want true (key was loaded from file)", k)
		}
	}
	// Comment lines and untrimmed forms are not keys.
	for _, k := range []string{"# this is a comment", "#k3-looks-like-comment", "  k1  ", ""} {
		if ks.Validate(k) {
			t.Errorf("Validate(%q) = true, want false (comment/blank/untrimmed)", k)
		}
	}
}

func TestLoadFileNonExistentPathReturnsError(t *testing.T) {
	ks := NewKeySet()
	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")

	err := ks.LoadFile(missing)
	if err == nil {
		t.Fatalf("LoadFile(%q) = nil error, want an error for a missing path", missing)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not mention the missing path %q", err.Error(), missing)
	}
	// A failed load must leave the set untouched (still auth-disabled here).
	if !ks.Empty() {
		t.Errorf("Empty() = false after failed LoadFile, want true (set unchanged)")
	}
}

func TestLoadFileAtomicallyReplacesPreviousKeys(t *testing.T) {
	// Pins the hot-reload swap semantics: a later LoadFile fully replaces the
	// active set rather than merging into it.
	fileA := writeKeyFile(t, "a\n")
	fileB := filepath.Join(t.TempDir(), "keysB.txt")
	if err := os.WriteFile(fileB, []byte("b\n"), 0o600); err != nil {
		t.Fatalf("write fileB: %v", err)
	}

	ks := NewKeySet()
	if err := ks.LoadFile(fileA); err != nil {
		t.Fatalf("LoadFile(A): %v", err)
	}
	if !ks.Validate("a") || ks.Validate("b") {
		t.Fatalf("after LoadFile(A): want only %q valid, got Validate(a)=%v Validate(b)=%v",
			"a", ks.Validate("a"), ks.Validate("b"))
	}

	if err := ks.LoadFile(fileB); err != nil {
		t.Fatalf("LoadFile(B): %v", err)
	}
	if got := ks.Snapshot(); got != 1 {
		t.Fatalf("Snapshot() = %d after LoadFile(B), want 1 (set replaced, not merged)", got)
	}
	if ks.Validate("a") {
		t.Errorf("Validate(%q) = true after swap to file B, want false (old key dropped)", "a")
	}
	if !ks.Validate("b") {
		t.Errorf("Validate(%q) = false after swap to file B, want true", "b")
	}
}

func TestValidateMatchingAndNonMatching(t *testing.T) {
	ks := NewKeySet()
	ks.LoadCSV("secret")

	tests := []struct {
		name      string
		presented string
		want      bool
	}{
		{name: "empty presented", presented: "", want: false},
		{name: "exact match", presented: "secret", want: true},
		{name: "non-match same length", presented: "secrXt", want: false},
		{name: "non-match shorter", presented: "sec", want: false},
		{name: "non-match longer", presented: "secretXX", want: false},
		{name: "case differs", presented: "SECRET", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ks.Validate(tc.presented); got != tc.want {
				t.Errorf("Validate(%q) = %v, want %v", tc.presented, got, tc.want)
			}
		})
	}
}

func TestValidateMatchesNonFirstKey(t *testing.T) {
	// Pins that Validate iterates the whole set rather than early-exiting on the
	// first stored key: the last loaded key must still validate true.
	ks := NewKeySet()
	ks.LoadCSV("first,second,third")

	if got := ks.Snapshot(); got != 3 {
		t.Fatalf("precondition: Snapshot() = %d, want 3", got)
	}
	for _, k := range []string{"first", "second", "third"} {
		if !ks.Validate(k) {
			t.Errorf("Validate(%q) = false, want true (all stored keys must validate)", k)
		}
	}
	if ks.Validate("fourth") {
		t.Errorf("Validate(%q) = true, want false (not stored)", "fourth")
	}
}

func TestEmptyAndSnapshotReflectStateAcrossLoads(t *testing.T) {
	ks := NewKeySet()
	if !ks.Empty() || ks.Snapshot() != 0 {
		t.Fatalf("fresh: Empty()=%v Snapshot()=%d, want true/0", ks.Empty(), ks.Snapshot())
	}

	ks.LoadCSV("a,b")
	if ks.Empty() || ks.Snapshot() != 2 {
		t.Fatalf("after CSV(a,b): Empty()=%v Snapshot()=%d, want false/2", ks.Empty(), ks.Snapshot())
	}

	ks.LoadCSV("x")
	if ks.Empty() || ks.Snapshot() != 1 {
		t.Fatalf("after CSV(x): Empty()=%v Snapshot()=%d, want false/1", ks.Empty(), ks.Snapshot())
	}

	ks.LoadCSV("")
	if !ks.Empty() || ks.Snapshot() != 0 {
		t.Fatalf("after CSV(empty): Empty()=%v Snapshot()=%d, want true/0", ks.Empty(), ks.Snapshot())
	}
}

func TestKeySetSatisfiesValidatorInterface(t *testing.T) {
	// Round-trip the *KeySet through the Validator interface the gateway depends
	// on, exercising both interface methods.
	var v Validator = NewKeySet()
	if !v.Empty() {
		t.Fatalf("via Validator: Empty() = false, want true for fresh KeySet")
	}
	if v.Validate("anything") {
		t.Fatalf("via Validator: Validate(%q) = true, want false", "anything")
	}

	ks := NewKeySet()
	ks.LoadCSV("token")
	v = ks
	if v.Empty() {
		t.Errorf("via Validator: Empty() = true after load, want false")
	}
	if !v.Validate("token") {
		t.Errorf("via Validator: Validate(%q) = false, want true", "token")
	}
}

// LoadFile with an empty path is rejected outright — an operator who sets
// API_KEYS_FILE to "" must get an error, not a silently auth-disabled set.
func TestLoadFileEmptyPathReturnsError(t *testing.T) {
	ks := NewKeySet()
	err := ks.LoadFile("")
	if err == nil {
		t.Fatal("LoadFile(\"\") = nil, want error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error %q does not mention empty path", err.Error())
	}
	if !ks.Empty() {
		t.Error("set must stay empty after a failed LoadFile")
	}
}
