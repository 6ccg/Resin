ALTER TABLE nodes_dynamic ADD COLUMN inspection_json TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes_dynamic ADD COLUMN recovery_success_count INTEGER NOT NULL DEFAULT 0;
