package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// Deploy Token format: vbd_<env>_<43-char base62-ish>
// We use base64.RawURLEncoding (43 chars for 32 bytes) — close enough to spec; URL-safe.

const TokenPrefix = "vbd_"

var ErrBadFormat = errors.New("malformed token")

// GenerateToken returns (plaintext, sha256-hash, display-prefix).
// `env` is "live" today; reserved for future scoping.
func GenerateToken(env string) (string, []byte, string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", nil, "", err
	}
	body := base64.RawURLEncoding.EncodeToString(b[:])
	tok := TokenPrefix + env + "_" + body
	h := sha256.Sum256([]byte(tok))
	prefix := tok
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	return tok, h[:], prefix, nil
}

func HashToken(tok string) []byte {
	h := sha256.Sum256([]byte(tok))
	return h[:]
}

func LooksLikeDeployToken(tok string) bool {
	return strings.HasPrefix(tok, TokenPrefix)
}

// Project API token format: iai_api_<43-char base64url>. Distinct prefix from
// deploy tokens so the verifier and audit logs can tell them apart at a glance.
const APITokenPrefix = "iai_api_"

// GenerateAPIToken mints a fresh per-project API token. Returns
// (plaintext, sha256-hash, display-prefix). The plaintext is the value the
// owner copies into their integration — we keep only the hash + prefix and
// never reconstruct the plaintext.
func GenerateAPIToken() (string, []byte, string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", nil, "", err
	}
	body := base64.RawURLEncoding.EncodeToString(b[:])
	tok := APITokenPrefix + body
	h := sha256.Sum256([]byte(tok))
	prefix := tok
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	return tok, h[:], prefix, nil
}

func LooksLikeAPIToken(tok string) bool {
	return strings.HasPrefix(tok, APITokenPrefix)
}
