CREATE TABLE drama_reference_drafts (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL,
 drama_id bigint NOT NULL,
 reference_id bigint NOT NULL,
 request_key varchar(128) NOT NULL,
 fingerprint varchar(64) NOT NULL,
 base_revision bigint NOT NULL,
 revision bigint NOT NULL DEFAULT 1,
 status varchar(16) NOT NULL DEFAULT 'editing',
 source varchar(16) NOT NULL,
 content jsonb NOT NULL,
 created_at timestamptz,
 updated_at timestamptz
);
CREATE UNIQUE INDEX uk_reference_draft_key ON drama_reference_drafts(reference_id,request_key);
CREATE INDEX idx_reference_draft_page ON drama_reference_drafts(reference_id);
