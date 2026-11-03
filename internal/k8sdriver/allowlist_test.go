package k8sdriver

import (
	"regexp"
	"testing"
)

func TestPathAllowListName(t *testing.T) {
	a := pathAllowListName("proj", "/webhook")
	if a != pathAllowListName("proj", "/webhook") {
		t.Fatal("name must be stable")
	}
	if a == pathAllowListName("proj", "/webhook2") {
		t.Fatal("different prefixes must not collide")
	}
	// check-ingress-exposure.sh relies on this suffix to spot per-path lists.
	if !regexp.MustCompile(`^iai-allowlist-proj-p[0-9a-f]{8}$`).MatchString(a) {
		t.Fatalf("unexpected name %q", a)
	}
}
