-- Per-project egress mode, admin-only.
--   off      pods go straight out (public internet open, private ranges closed)
--   observe  pods get HTTPS_PROXY; squid lets everything through and logs it
--   enforce  0.0.0.0/0 is dropped from the NetworkPolicy; only the proxy and its
--            per-project domain allow-list reach the internet
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS egress_mode text NOT NULL DEFAULT 'off'
        CHECK (egress_mode IN ('off', 'observe', 'enforce'));
