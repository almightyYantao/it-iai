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

// Encrypt seals plaintext under the KEK, binding the result to `aad`.
//
// `aad` is authenticated but not encrypted: Decrypt only succeeds when given
// the identical value. Callers MUST pass the context the ciphertext belongs to
// (see EnvAAD) so a blob cannot be lifted out of one row and replayed under
// another. Passing nil is only legitimate in the KEK-rotation migration, which
// has to read pre-AAD ciphertext.
func (k *KEK) Encrypt(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return k.aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Decrypt opens a ciphertext produced by Encrypt. `aad` must match exactly what
// was supplied at seal time, otherwise this fails — that mismatch is the whole
// point: it is what stops a ciphertext from being moved between projects.
func (k *KEK) Decrypt(ciphertext, aad []byte) ([]byte, error) {
	ns := k.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("ciphertext too short")
	}
	return k.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], aad)
}

// EnvAAD is the canonical AAD for a project_env value, binding the ciphertext
// to both the owning project and the key name.
//
// Without this binding an attacker with write access to project_env can copy
// another project's encrypted value into a project they control and let the
// platform decrypt it for them on the next deploy. Domain-separated by a fixed
// prefix, NUL-delimited so ("a","bc") and ("ab","c") cannot collide.
func EnvAAD(projectID, key string) []byte {
	return []byte("project_env\x00" + projectID + "\x00" + key)
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
