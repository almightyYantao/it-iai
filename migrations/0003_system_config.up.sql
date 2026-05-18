-- M2: admin-editable system configuration.
-- Stores runtime-mutable settings (Keycloak endpoints, auth host, etc.) so
-- admins can change them from the Web UI without SSHing to the platform
-- and restarting the container.
--
-- Bootstrap secrets (CP_DATABASE_URL, CP_KEK_BASE64) intentionally stay in
-- .env — they must exist before the DB is reachable.

CREATE TABLE IF NOT EXISTS system_config (
    key         text PRIMARY KEY,
    value       text NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid REFERENCES users(id)
);
