-- Per-minute node usage for the ops dashboard sparklines. Kept 7 days.
CREATE TABLE IF NOT EXISTS ops_samples (
    ts        timestamptz NOT NULL DEFAULT now(),
    node      text NOT NULL,
    cpu_milli bigint NOT NULL,
    cpu_cap   bigint NOT NULL,
    mem_bytes bigint NOT NULL,
    mem_cap   bigint NOT NULL,
    PRIMARY KEY (node, ts)
);
CREATE INDEX IF NOT EXISTS ops_samples_ts_idx ON ops_samples (ts);
