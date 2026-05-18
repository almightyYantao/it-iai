package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Provisioner gives each project a private bucket on the platform's MinIO
// plus an isolated access-key pair that can only see that bucket.
//
// Reuses the same MinIO instance the platform uses for source tarballs;
// per-project resources are namespaced by `proj-<slug>` prefix so they don't
// collide with the platform's own buckets (`vibedeploy-source`, etc.).
//
// Per project:
//   * bucket          : proj-<slug>
//   * canned policy   : proj-<slug>-policy (allows ALL S3 actions on bucket
//                       proj-<slug> and its objects; nothing else)
//   * MinIO user      : proj-<slug> with random secret key, policy attached
//
// The minio-go (object client) handles the bucket; madmin-go handles user /
// policy admin. Both talk to the same endpoint with the platform-admin
// credentials (CP_S3_ACCESS_KEY / CP_S3_SECRET_KEY).
type S3Provisioner struct {
	adminEndpoint string // host:port — internal compose network address (e.g. "minio:9000")
	adminKey      string
	adminSecret   string
	publicHost    string // what user pods see in S3_ENDPOINT (e.g. "<platform-ip>:9000")
	region        string
	useSSL        bool
	publicUseSSL  bool
}

func NewS3Provisioner(adminEndpoint, adminKey, adminSecret, publicHost, region string, useSSL, publicUseSSL bool) *S3Provisioner {
	return &S3Provisioner{
		adminEndpoint: adminEndpoint,
		adminKey:      adminKey,
		adminSecret:   adminSecret,
		publicHost:    publicHost,
		region:        region,
		useSSL:        useSSL,
		publicUseSSL:  publicUseSSL,
	}
}

func (p *S3Provisioner) Enabled() bool {
	return p != nil && p.adminEndpoint != "" && p.adminKey != "" && p.adminSecret != "" && p.publicHost != ""
}

// s3IdentFor names the bucket, user, and policy. "proj-<slug>" keeps these
// namespaced and immediately recognisable in the MinIO console.
func s3IdentFor(slug string) string { return "proj-" + slug }

// S3Creds is the full set of env vars a user pod needs to talk to its bucket.
// Reconciler turns each non-empty field into a project_env row (system=true).
type S3Creds struct {
	Endpoint        string // host:port — what the pod uses; no scheme
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	UseSSL          bool
}

// Provision is idempotent: existing bucket/user/policy are reused; only the
// secret key gets rotated. The caller's project_env row gets overwritten
// with the new credentials so the old secret becomes unreachable.
func (p *S3Provisioner) Provision(ctx context.Context, slug string) (*S3Creds, error) {
	if !p.Enabled() {
		return nil, errors.New("s3 provisioner not enabled")
	}

	bucket := s3IdentFor(slug)
	user := bucket
	policyName := bucket + "-policy"

	// 1. Bucket — using the regular S3 client (minio-go), MakeBucket is
	//    idempotent in the sense that "bucket already exists" is a recognisable
	//    error we can tolerate.
	mc, err := minio.New(p.adminEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(p.adminKey, p.adminSecret, ""),
		Secure: p.useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}
	if err := mc.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: p.region}); err != nil {
		// MinIO returns BucketAlreadyOwnedByYou when the same admin tries to
		// re-create their own bucket; that's the idempotent path.
		exists, _ := mc.BucketExists(ctx, bucket)
		if !exists {
			return nil, fmt.Errorf("make bucket: %w", err)
		}
	}

	// 2. Admin client (madmin) for user + policy mgmt.
	adm, err := madmin.New(p.adminEndpoint, p.adminKey, p.adminSecret, p.useSSL)
	if err != nil {
		return nil, fmt.Errorf("madmin client: %w", err)
	}

	// 3. Policy — JSON doc that allows ALL actions on the bucket + its objects,
	//    nothing else. The arn:aws:s3::: prefix is required by MinIO's parser.
	policyJSON := fmt.Sprintf(`{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Action": ["s3:*"],
				"Resource": [
					"arn:aws:s3:::%s",
					"arn:aws:s3:::%s/*"
				]
			}
		]
	}`, bucket, bucket)
	// Validate it's well-formed JSON before pushing.
	if !json.Valid([]byte(policyJSON)) {
		return nil, errors.New("internal: built invalid policy JSON")
	}
	if err := adm.AddCannedPolicy(ctx, policyName, bytes.TrimSpace([]byte(policyJSON))); err != nil {
		return nil, fmt.Errorf("add policy: %w", err)
	}

	// 4. User — rotate secret. AddUser is upsert: a second call replaces the
	//    secret of an existing user.
	secret, err := randomPassword()
	if err != nil {
		return nil, err
	}
	if err := adm.AddUser(ctx, user, secret); err != nil {
		return nil, fmt.Errorf("add user: %w", err)
	}

	// 5. Attach policy.
	if _, err := adm.AttachPolicy(ctx, madmin.PolicyAssociationReq{
		Policies: []string{policyName},
		User:     user,
	}); err != nil {
		// Already-attached is OK; some madmin versions return errors that look
		// non-fatal. String-match to filter the benign case.
		if !strings.Contains(err.Error(), "already") {
			return nil, fmt.Errorf("attach policy: %w", err)
		}
	}

	return &S3Creds{
		Endpoint:        p.publicHost,
		Region:          p.region,
		AccessKeyID:     user,
		SecretAccessKey: secret,
		Bucket:          bucket,
		UseSSL:          p.publicUseSSL,
	}, nil
}

// Deprovision drops the user + policy and (for safety) leaves the bucket
// in place. Dropping the bucket would silently lose every object the project
// had stored; an admin can `mc rb --force` it manually after confirming the
// project is truly gone.
//
// Best-effort: returns the FIRST error but keeps going so partial cleanup is
// still possible.
func (p *S3Provisioner) Deprovision(ctx context.Context, slug string) error {
	if !p.Enabled() {
		return nil
	}
	user := s3IdentFor(slug)
	policyName := user + "-policy"
	adm, err := madmin.New(p.adminEndpoint, p.adminKey, p.adminSecret, p.useSSL)
	if err != nil {
		return err
	}
	var firstErr error
	if _, err := adm.DetachPolicy(ctx, madmin.PolicyAssociationReq{
		Policies: []string{policyName},
		User:     user,
	}); err != nil && !strings.Contains(err.Error(), "not found") {
		firstErr = fmt.Errorf("detach: %w", err)
	}
	if err := adm.RemoveUser(ctx, user); err != nil && firstErr == nil {
		if !strings.Contains(err.Error(), "not found") {
			firstErr = fmt.Errorf("remove user: %w", err)
		}
	}
	if err := adm.RemoveCannedPolicy(ctx, policyName); err != nil && firstErr == nil {
		if !strings.Contains(err.Error(), "not found") {
			firstErr = fmt.Errorf("remove policy: %w", err)
		}
	}
	return firstErr
}
