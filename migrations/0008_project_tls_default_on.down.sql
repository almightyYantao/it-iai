-- Restore TLS-off default. We do NOT reverse the UPDATE — flipping projects
-- back to HTTP-only silently would surprise owners who started relying on
-- HTTPS. Operators can flip individual projects via PATCH /v1/projects/:slug/tls.
ALTER TABLE projects ALTER COLUMN tls_enabled SET DEFAULT false;
