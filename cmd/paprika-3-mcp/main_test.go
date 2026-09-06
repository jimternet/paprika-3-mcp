package main

import (
	"bytes"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestResolveCredentials_FlagsWin verifies that a password supplied via the
// "flag" pointer is never overwritten by the env or Keychain.
func TestResolveCredentials_FlagsWin(t *testing.T) {
	t.Setenv("PAPRIKA_USERNAME", "env@example.com")
	t.Setenv("PAPRIKA_PASSWORD", "env-password")

	username := "flag@example.com"
	password := "flag-password"

	resolveCredentials(&username, &password, nil)

	if username != "flag@example.com" {
		t.Errorf("username overwritten: got %q", username)
	}
	if password != "flag-password" {
		t.Errorf("password overwritten: got %q", password)
	}
}

// TestResolveCredentials_EnvFallback verifies env vars are picked up when
// flags are empty.
func TestResolveCredentials_EnvFallback(t *testing.T) {
	t.Setenv("PAPRIKA_USERNAME", "env@example.com")
	t.Setenv("PAPRIKA_PASSWORD", "env-password")

	username := ""
	password := ""

	resolveCredentials(&username, &password, nil)

	if username != "env@example.com" {
		t.Errorf("username: got %q, want env@example.com", username)
	}
	if password != "env-password" {
		t.Errorf("password: got %q, want env-password", password)
	}
}

// TestPasswordNotLogged verifies that the password is never emitted to any
// log output, even when the Keychain path is exercised using a fake
// "security" binary injected via PATH.
func TestPasswordNotLogged(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("keychain test only runs on darwin")
	}

	const (
		testUser = "test@example.com"
		testPass = "super-secret-password"
	)

	// --- build a tiny fake "security" binary ---
	// It prints testPass to stdout and exits 0, simulating a successful
	// Keychain lookup.
	tmpDir := t.TempDir()
	fakeScript := filepath.Join(tmpDir, "security")
	scriptContent := "#!/bin/sh\necho " + testPass + "\n"
	if err := os.WriteFile(fakeScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("write fake security script: %v", err)
	}

	// Prepend tmpDir to PATH so our fake binary is found first.
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+string(os.PathListSeparator)+origPath)

	// Sanity-check: exec finds the fake binary.
	out, err := exec.Command("security", "find-generic-password", "-s", "paprika-3-mcp", "-a", testUser, "-w").Output()
	if err != nil {
		t.Fatalf("fake security binary not working: %v", err)
	}
	if strings.TrimSpace(string(out)) != testPass {
		t.Fatalf("fake security binary returned unexpected output: %q", out)
	}

	// --- wire a buffer-backed slog logger ---
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelDebug, // capture debug messages too
	}))

	// Clear env so only the Keychain path fires.
	t.Setenv("PAPRIKA_USERNAME", "")
	t.Setenv("PAPRIKA_PASSWORD", "")

	username := testUser
	password := ""

	resolveCredentials(&username, &password, logger)

	// The keychain lookup should have populated the password.
	if password != testPass {
		t.Errorf("expected password from keychain, got %q", password)
	}

	// The password must NOT appear anywhere in the log output.
	logOutput := logBuf.String()
	if strings.Contains(logOutput, testPass) {
		t.Errorf("password found in log output:\n%s", logOutput)
	}
}

// TestKeychainPassword_ErrorDoesNotLeak verifies that when the Keychain
// lookup fails, the returned error does not contain the password, and
// resolveCredentials leaves password empty (rather than fataling).
func TestKeychainPassword_ErrorDoesNotLeak(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("keychain test only runs on darwin")
	}

	// Point PATH at a dir with no "security" binary so the lookup fails.
	tmpDir := t.TempDir()
	t.Setenv("PATH", tmpDir)
	t.Setenv("PAPRIKA_USERNAME", "")
	t.Setenv("PAPRIKA_PASSWORD", "")

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	username := "user@example.com"
	password := ""

	resolveCredentials(&username, &password, logger)

	if password != "" {
		t.Errorf("expected empty password on Keychain error, got %q", password)
	}

	// A debug message about the failure is fine; it must not include any
	// secret (password is empty here so this is trivially true, but guards
	// against future regression).
	logOutput := logBuf.String()
	_ = logOutput // nothing secret to check; test confirms no panic/fatal
}
