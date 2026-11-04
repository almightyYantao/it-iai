-- Persistent paths per project (Longhorn PVCs), admin-configured.
CREATE TABLE IF NOT EXISTS project_volumes (
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path       text NOT NULL,
    size_gi    integer NOT NULL CHECK (size_gi BETWEEN 1 AND 500),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, path)
);

-- One-time tickets for the browser terminal (WebSocket can't send headers).
CREATE TABLE IF NOT EXISTS exec_tickets (
    ticket_hash bytea PRIMARY KEY,
    project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    pod         text NOT NULL,
    container   text NOT NULL,
    actor       text NOT NULL,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz
);

-- Crash events already alerted, so a restart isn't announced twice.
CREATE TABLE IF NOT EXISTS crash_events (
    pod_uid     text NOT NULL,
    container   text NOT NULL,
    finished_at timestamptz NOT NULL,
    slug        text NOT NULL,
    reason      text NOT NULL,
    exit_code   integer NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (pod_uid, container, finished_at)
);
