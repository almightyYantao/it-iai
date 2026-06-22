-- Make HTTPS the default for new projects. cert-manager + a healthy LE
-- ClusterIssuer are now expected to be present in every cluster, so opting
-- out of TLS is the rare case.
--
-- Existing projects with tls_enabled=false get flipped on too — control-plane's
-- startup ingress sweep re-applies their ingress so cert-manager sees the new
-- cert-manager.io/cluster-issuer annotation and issues a per-host LE cert via
-- HTTP-01.
ALTER TABLE projects ALTER COLUMN tls_enabled SET DEFAULT true;
UPDATE projects SET tls_enabled = true WHERE deleted_at IS NULL AND tls_enabled = false;
