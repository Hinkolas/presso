package secrets

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	b, err := New(strings.Repeat("k", 40))
	if err != nil {
		t.Fatal(err)
	}
	ct, err := b.EncryptString("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte("hunter2")) {
		t.Fatal("plaintext leaked")
	}
	pt, err := b.DecryptString(ct)
	if err != nil || pt != "hunter2" {
		t.Fatalf("decrypt: %q %v", pt, err)
	}

	// Two encryptions of the same value differ (random nonce).
	ct2, _ := b.EncryptString("hunter2")
	if bytes.Equal(ct, ct2) {
		t.Fatal("nonce reuse")
	}

	// Tampering is detected.
	ct[len(ct)-1] ^= 0xff
	if _, err := b.Decrypt(ct); !errors.Is(err, ErrMalformed) {
		t.Fatalf("tamper: %v", err)
	}
	if _, err := b.Decrypt([]byte("short")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("short: %v", err)
	}

	// A different master secret cannot decrypt.
	other, _ := New(strings.Repeat("x", 40))
	if _, err := other.Decrypt(ct2); !errors.Is(err, ErrMalformed) {
		t.Fatalf("wrong key: %v", err)
	}
}

func TestShortMaster(t *testing.T) {
	if _, err := New("short"); err == nil {
		t.Fatal("expected error")
	}
}
