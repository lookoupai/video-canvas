ALTER TABLE drama_reference_drafts
 ADD COLUMN session_id bigint NOT NULL DEFAULT 0,
 ADD COLUMN run_id bigint NOT NULL DEFAULT 0;
