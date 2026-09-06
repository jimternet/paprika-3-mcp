//go:build darwin

package main

import (
	"os/exec"
	"strings"
)

// keychainPassword looks up the password for username from the macOS Keychain.
// It runs: security find-generic-password -s paprika-3-mcp -a <username> -w
// Returns the trimmed password on success, or ("", err) if not found.
func keychainPassword(username string) (string, error) {
	out, err := exec.Command(
		"security", "find-generic-password",
		"-s", "paprika-3-mcp",
		"-a", username,
		"-w",
	).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
