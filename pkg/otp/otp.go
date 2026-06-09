// Package otp provides secure generation and formatting of one-time passwords.
package otp

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

const (
	// DefaultLength is the number of digits in a generated OTP.
	DefaultLength = 6

	// maxDigit is the exclusive upper bound for each random digit (0-9).
	maxDigit = 10
)

// Generate creates a cryptographically-secure N-digit numeric OTP string.
// It uses crypto/rand — NOT math/rand — ensuring each digit is unguessable.
//
// Example output: "483021"
func Generate(length int) (string, error) {
	if length <= 0 {
		length = DefaultLength
	}

	digits := make([]byte, length)
	for i := range digits {
		n, err := rand.Int(rand.Reader, big.NewInt(maxDigit))
		if err != nil {
			return "", fmt.Errorf("otp.Generate: crypto/rand failed at position %d: %w", i, err)
		}
		// Store the ASCII digit character ('0' = 48)
		digits[i] = byte(n.Int64()) + '0'
	}
	return string(digits), nil
}

// MustGenerate is like Generate but panics on error.
// Use only in test helpers, never in production request paths.
func MustGenerate(length int) string {
	code, err := Generate(length)
	if err != nil {
		panic(err)
	}
	return code
}
