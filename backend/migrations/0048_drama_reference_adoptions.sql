CREATE TABLE drama_reference_adoptions (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL,
 drama_id bigint NOT NULL,
 reference_id bigint NOT NULL,
 reference_revision bigint NOT NULL,
 request_key varchar(128) NOT NULL,
 request_keys jsonb NOT NULL DEFAULT '{}',
 fingerprint varchar(64) NOT NULL,
 mode varchar(16) NOT NULL,
 episode_id bigint NOT NULL,
 snapshot jsonb NOT NULL,
 segment_ids jsonb NOT NULL DEFAULT '[]',
 created_at timestamptz
);
CREATE UNIQUE INDEX uk_drama_adoption_key ON drama_reference_adoptions(user_id,request_key);
CREATE INDEX idx_drama_reference_adoptions_drama_id ON drama_reference_adoptions(drama_id);
CREATE INDEX idx_drama_reference_adoptions_reference_id ON drama_reference_adoptions(reference_id);
CREATE INDEX idx_drama_reference_adoptions_episode_id ON drama_reference_adoptions(episode_id);
ALTER TABLE drama_segments ADD COLUMN remake jsonb NOT NULL DEFAULT '{}';
ALTER TABLE drama_episodes ADD COLUMN reference_basis jsonb NOT NULL DEFAULT '{}';
