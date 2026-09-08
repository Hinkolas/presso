// Package secrets encrypts values the engine must store but never expose in
// plaintext at rest: per-site database and object-cache passwords, WordPress salts,
// login HMAC keys, platform root passwords. The key is derived from
// AUTH_SECRET with HKDF, so rotating AUTH_SECRET invalidates stored secrets
// (documented on the config field) and nothing else needs to be configured.
//
// Ciphertext layout: "pv1" || 12-byte nonce || AES-256-GCM(plaintext). The
// prefix leaves room for a future key rotation scheme.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

const (
	prefix    = "pv1"
	nonceSize = 12
	keyInfo   = "presso/secrets/v1"
)

var ErrMalformed = errors.New("secrets: malformed or tampered ciphertext")

// Box encrypts and decrypts with a single derived key.
type Box struct {
	aead cipher.AEAD
}

// New derives the encryption key from the master secret (AUTH_SECRET).
func New(master string) (*Box, error) {
	if len(master) < 32 {
		return nil, errors.New("secrets: master secret must be at least 32 bytes")
	}
	key, err := hkdf.Key(sha256.New, []byte(master), nil, keyInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("secrets: derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Encrypt seals plaintext under a fresh random nonce.
func (b *Box) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: nonce: %w", err)
	}
	out := make([]byte, 0, len(prefix)+nonceSize+len(plain)+b.aead.Overhead())
	out = append(out, prefix...)
	out = append(out, nonce...)
	return b.aead.Seal(out, nonce, plain, []byte(prefix)), nil
}

// Decrypt opens a value produced by Encrypt.
func (b *Box) Decrypt(blob []byte) ([]byte, error) {
	if len(blob) < len(prefix)+nonceSize+b.aead.Overhead() || string(blob[:len(prefix)]) != prefix {
		return nil, ErrMalformed
	}
	nonce := blob[len(prefix) : len(prefix)+nonceSize]
	plain, err := b.aead.Open(nil, nonce, blob[len(prefix)+nonceSize:], []byte(prefix))
	if err != nil {
		return nil, ErrMalformed
	}
	return plain, nil
}

// EncryptString and DecryptString are the string-typed conveniences most
// callers want.
func (b *Box) EncryptString(s string) ([]byte, error) { return b.Encrypt([]byte(s)) }

func (b *Box) DecryptString(blob []byte) (string, error) {
	p, err := b.Decrypt(blob)
	if err != nil {
		return "", err
	}
	return string(p), nil
}
