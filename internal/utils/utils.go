// Package utils holds small generic helpers shared across the codebase.
// Keep it dependency-light: standard library plus google/uuid only, so
// importing it never drags server-side dependencies into the CLI.
package utils

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
)

// EnvOr returns the environment variable's value, or fallback when the
// variable is unset or empty.
func EnvOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// RandomToken returns byteLen bytes of entropy as unpadded URL-safe base64,
// the shape used for every opaque secret the engine hands out.
func RandomToken(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("utils: random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
