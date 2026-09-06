//go:build !darwin

package main

// keychainPassword is a no-op on non-darwin platforms.
func keychainPassword(_ string) (string, error) {
	return "", nil
}
