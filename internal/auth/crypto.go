package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

// KEK wraps an AES-256-GCM key derived from CP_KEK_BASE64.
// We keep the raw key bytes around so we can derive sub-keys for things
// unrelated to AEAD — see Derive().
type KEK struct {
	aead cipher.AEAD
	key  []byte // 32 bytes
}

func LoadKEK(b64 string) (*KEK, error) {
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		// Permit short dev key by padding/truncating to 32 — better than crashing in dev.
		k := make([]byte, 32)
		copy(k, key)
		key = k
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	dup := make([]byte, len(key))
	copy(dup, key)
	return &KEK{aead: gcm, key: dup}, nil
}

func (k *KEK) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return k.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (k *KEK) Decrypt(ciphertext []byte) ([]byte, error) {
	ns := k.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("ciphertext too short")
	}
	return k.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}

// Derive returns a 32-byte sub-key bound to a label. Used to keep auxiliary
// signing keys (OIDC state HMAC, future webhook signatures) cryptographically
// separated from the AEAD master key without making the operator manage a
// second secret.
//
// Construction: HMAC-SHA256(label, raw_kek). Deterministic, fast, sufficient
// for our (server-side only, never persisted) state-signing use case.
func (k *KEK) Derive(label string) []byte {
	if k == nil || len(k.key) == 0 {
		return nil
	}
	mac := hmac.New(sha256.New, []byte(label))
	mac.Write(k.key)
	return mac.Sum(nil)
}
