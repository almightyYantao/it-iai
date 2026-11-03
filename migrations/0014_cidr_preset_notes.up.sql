-- Per-entry notes for preset CIDRs ("上海办公室", "新加坡 VPN"), keyed by the
-- normalised CIDR. Display only; enforcement still reads cidrs[].
ALTER TABLE cidr_presets
    ADD COLUMN IF NOT EXISTS notes jsonb NOT NULL DEFAULT '{}';
