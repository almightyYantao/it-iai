package scan

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
)

var (
	slugRegexp     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{1,30}[a-z0-9])?$`)
	reservedSlugs  = map[string]struct{}{}
	reservedList   = []string{
		"api", "kc", "auth", "oauth2", "admin", "console", "registry",
		"minio", "traefik", "www", "mail", "root", "public", "internal",
		"vibedeploy", "platform", "health", "healthz", "metrics",
	}
)

func init() {
	for _, s := range reservedList {
		reservedSlugs[s] = struct{}{}
	}
}

// ValidateSlug returns nil when slug is acceptable.
func ValidateSlug(s string) error {
	s = strings.ToLower(s)
	if !slugRegexp.MatchString(s) {
		return fmt.Errorf("slug must be 3-32 chars, [a-z0-9-], no leading/trailing hyphen")
	}
	if _, bad := reservedSlugs[s]; bad {
		return fmt.Errorf("slug %q is reserved", s)
	}
	return nil
}

// RandomSuffix returns a short, lowercase a-z0-9 suffix like "7k2x".
func RandomSuffix(n int) string {
	const alpha = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alpha[int(b[i])%len(alpha)]
	}
	return string(b)
}
