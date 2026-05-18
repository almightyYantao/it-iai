package provision

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// RedisProvisioner sets up a per-project Redis ACL user with key-namespace
// restriction. Each project gets:
//   * a unique username "proj-<slug>"
//   * a random password
//   * access ONLY to keys matching "proj-<slug>:*"
//   * access ONLY to pub/sub channels matching "proj-<slug>:*"
//   * commands: everything except @dangerous (FLUSHALL, CONFIG, KEYS, etc.)
//
// All projects share the same Redis instance (cheap, no extra container per
// project), but the ACL guarantees they can't read or write each other's keys —
// an attempted SET to a non-prefixed key returns NOPERM at the protocol level.
//
// We also inject a sibling env var REDIS_KEY_PREFIX so apps / frameworks that
// want to auto-prefix know the right value without parsing the URL.
type RedisProvisioner struct {
	adminURL   string // redis://default:<adminpw>@redis:6379/0 — inside compose network
	publicHost string // what user pods see — typically <platform-ip>:6379
}

func NewRedisProvisioner(adminURL, publicHost string) *RedisProvisioner {
	return &RedisProvisioner{adminURL: adminURL, publicHost: publicHost}
}

func (r *RedisProvisioner) Enabled() bool {
	return r != nil && r.adminURL != "" && r.publicHost != ""
}

// userForSlug names the ACL user. Slugs are [a-z0-9-]+ and Redis accepts
// those chars in ACL usernames, so a "proj-" prefix is enough to namespace.
func redisUserForSlug(slug string) string { return "proj-" + slug }

// RedisCreds is what a successful Provision returns. Caller writes each field
// into project_env so the pod can read them as env vars.
type RedisCreds struct {
	URL       string // full URL with the per-project user/password baked in
	KeyPrefix string // proj-<slug>: — apps must use this prefix or hit NOPERM
}

// Provision creates / rotates the per-project ACL user. Idempotent — calling
// twice rotates the password (caller's project_env row gets overwritten with
// the new URL, so the old one becomes unreachable anyway).
func (r *RedisProvisioner) Provision(ctx context.Context, slug string) (*RedisCreds, error) {
	if !r.Enabled() {
		return nil, errors.New("redis provisioner not enabled")
	}
	opts, err := redis.ParseURL(r.adminURL)
	if err != nil {
		return nil, fmt.Errorf("parse admin URL: %w", err)
	}
	client := redis.NewClient(opts)
	defer client.Close()

	user := redisUserForSlug(slug)
	prefix := user + ":" // e.g. "proj-foo:"
	pw, err := randomPassword()
	if err != nil {
		return nil, err
	}

	// ACL SETUSER <user> on >password resetkeys ~prefix:* resetchannels &prefix:*
	// +@all -@dangerous
	//
	//   on             — enabled
	//   >password      — set password (replaces any existing one)
	//   resetkeys      — clear any previously-set key patterns (idempotent)
	//   ~<pattern>     — allow only matching keys
	//   resetchannels  — clear previously-set channel patterns
	//   &<pattern>     — allow only matching pub/sub channels
	//   +@all          — start with every command
	//   -@dangerous    — drop the "dangerous" category (FLUSHALL, CONFIG, KEYS,
	//                    DEBUG, SHUTDOWN, etc.) so a project can't see / nuke
	//                    its neighbours
	cmd := client.Do(ctx,
		"ACL", "SETUSER", user,
		"on",
		">"+pw,
		"resetkeys",
		"~"+prefix+"*",
		"resetchannels",
		"&"+prefix+"*",
		"+@all",
		"-@dangerous",
	)
	if err := cmd.Err(); err != nil {
		return nil, fmt.Errorf("acl setuser: %w", err)
	}

	// Persist the ACL to disk (the running config got updated, but on restart
	// non-default users vanish unless we ACL SAVE). Failure here isn't fatal
	// — the URL we return still works against the running server.
	_ = client.Do(ctx, "ACL", "SAVE").Err()

	url := fmt.Sprintf("redis://%s:%s@%s/0", user, pw, r.publicHost)
	return &RedisCreds{URL: url, KeyPrefix: prefix}, nil
}

// Deprovision removes the ACL user. Failure is best-effort.
func (r *RedisProvisioner) Deprovision(ctx context.Context, slug string) error {
	if !r.Enabled() {
		return nil
	}
	opts, err := redis.ParseURL(r.adminURL)
	if err != nil {
		return err
	}
	client := redis.NewClient(opts)
	defer client.Close()

	user := redisUserForSlug(slug)
	if err := client.Do(ctx, "ACL", "DELUSER", user).Err(); err != nil {
		return fmt.Errorf("acl deluser: %w", err)
	}
	_ = client.Do(ctx, "ACL", "SAVE").Err()
	return nil
}
