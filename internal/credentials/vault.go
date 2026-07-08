package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// Vault handles AES-256-GCM encryption and decryption of provider API keys.
// Keys are encrypted before storage in PostgreSQL and decrypted into memory
// during cache loading. The hot-path never touches encrypted data.
type Vault struct {
	gcm cipher.AEAD
}

// NewVault creates a new encryption vault from a hex-encoded 32-byte master key.
func NewVault(hexKey string) (*Vault, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid master key hex encoding: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes (256 bits), got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &Vault{gcm: gcm}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM and returns hex-encoded ciphertext.
// Each encryption uses a random nonce, making identical plaintexts produce different ciphertexts.
func (v *Vault) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, v.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Seal appends the encrypted data to nonce, so the result is nonce+ciphertext+tag
	ciphertext := v.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// Decrypt decrypts hex-encoded ciphertext produced by Encrypt.
func (v *Vault) Decrypt(hexCiphertext string) (string, error) {
	ciphertext, err := hex.DecodeString(hexCiphertext)
	if err != nil {
		return "", fmt.Errorf("invalid ciphertext hex encoding: %w", err)
	}

	nonceSize := v.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := v.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}

	return string(plaintext), nil
}
