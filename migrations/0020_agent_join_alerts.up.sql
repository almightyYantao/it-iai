-- One-time agent join tokens. Only the hash is stored; the install script is
-- served once against it, then the row is marked used.
CREATE TABLE IF NOT EXISTS agent_join_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    node_name  text NOT NULL,
    token_hash bytea NOT NULL UNIQUE,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    used_from  text
);

-- Alert notification state: one row per open alert key, for dedupe and
-- "recovered" messages across control-plane restarts.
CREATE TABLE IF NOT EXISTS ops_alert_state (
    key        text PRIMARY KEY,
    level      text NOT NULL,
    target     text NOT NULL,
    text       text NOT NULL,
    first_seen timestamptz NOT NULL DEFAULT now(),
    last_sent  timestamptz NOT NULL DEFAULT now()
);
