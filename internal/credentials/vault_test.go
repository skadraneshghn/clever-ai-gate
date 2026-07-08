package credentials

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestVaultEncryptDecrypt(t *testing.T) {
	// Generate a random 32-byte key
	keyBytes := make([]byte, 32)
	rand.Read(keyBytes)
	hexKey := hex.EncodeToString(keyBytes)

	vault, err := NewVault(hexKey)
	if err != nil {
		t.Fatalf("failed to create vault: %v", err)
	}

	plaintext := "sk-test-api-key-1234567890abcdef"

	encrypted, err := vault.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Encrypted should not be the same as plaintext
	if encrypted == plaintext {
		t.Error("encrypted value should differ from plaintext")
	}

	decrypted, err := vault.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestVaultDifferentCiphertexts(t *testing.T) {
	keyBytes := make([]byte, 32)
	rand.Read(keyBytes)
	hexKey := hex.EncodeToString(keyBytes)

	vault, err := NewVault(hexKey)
	if err != nil {
		t.Fatalf("failed to create vault: %v", err)
	}

	plaintext := "test-key"

	enc1, _ := vault.Encrypt(plaintext)
	enc2, _ := vault.Encrypt(plaintext)

	// Same plaintext should produce different ciphertexts (random nonce)
	if enc1 == enc2 {
		t.Error("encrypting same plaintext twice should produce different ciphertexts")
	}

	// Both should decrypt to the same value
	dec1, _ := vault.Decrypt(enc1)
	dec2, _ := vault.Decrypt(enc2)

	if dec1 != plaintext || dec2 != plaintext {
		t.Error("both ciphertexts should decrypt to the same plaintext")
	}
}

func TestVaultInvalidKey(t *testing.T) {
	// Too short
	_, err := NewVault("abcdef")
	if err == nil {
		t.Error("expected error for short key")
	}

	// Invalid hex
	_, err = NewVault("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	if err == nil {
		t.Error("expected error for invalid hex")
	}
}

func BenchmarkEncrypt(b *testing.B) {
	keyBytes := make([]byte, 32)
	rand.Read(keyBytes)
	vault, _ := NewVault(hex.EncodeToString(keyBytes))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		vault.Encrypt("sk-test-api-key-1234567890abcdef")
	}
}
