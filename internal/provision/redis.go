package provision

import (
	"context"
	"errors"
	"strings"
)

// RedisProvisioner is intentionally minimal — we share one Redis instance
// across all user apps and namespace by key prefix. This trades isolation
// (any app COULD read another's keys if they cooperate against the convention)
// for orders-of-magnitude lower operational cost (no per-project StatefulSet,
// no per-project memory budget).
//
// Apps are expected to use a key prefix that matches their slug; the convention
// is enforced socially, not at the Redis ACL level. Phase 3 could move to
// Redis 6+ ACL with per-project users if isolation becomes a real concern.
//
// The "provisioner" therefore doesn't talk to Redis at all — it just builds
// the URL we'll inject as REDIS_URL.
type RedisProvisioner struct {
	urlTemplate string
}

func NewRedisProvisioner(urlTemplate string) *RedisProvisioner {
	return &RedisProvisioner{urlTemplate: urlTemplate}
}

func (r *RedisProvisioner) Enabled() bool {
	return r != nil && r.urlTemplate != ""
}

// Provision swaps {{slug}} into the configured template and returns the URL.
// No side effects. Idempotent by construction. The template typically looks
// like `redis://<host>:6379/0` (slug unused) or `redis://<host>:6379?prefix=proj-{{slug}}:`
// (if you want the URL to encode the key-prefix convention — most Redis
// clients ignore the `prefix` query param but it's a clear hint to readers).
func (r *RedisProvisioner) Provision(ctx context.Context, slug string) (string, error) {
	_ = ctx
	if !r.Enabled() {
		return "", errors.New("redis provisioner not enabled")
	}
	return strings.ReplaceAll(r.urlTemplate, "{{slug}}", slug), nil
}

// Deprovision is a no-op for the shared-instance design — no resources to
// release, no rows to clean. Kept on the interface for symmetry with the
// Postgres provisioner so the reconciler can call both without branching.
func (r *RedisProvisioner) Deprovision(ctx context.Context, slug string) error {
	_ = ctx
	_ = slug
	return nil
}
