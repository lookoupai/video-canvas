CREATE TABLE drama_reference_preparations (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL,
 drama_id bigint NOT NULL,
 reference_id bigint NOT NULL,
 base_revision bigint NOT NULL,
 source_asset_id bigint NOT NULL,
 duration_ms bigint NOT NULL,
 recipe_hash varchar(64) NOT NULL,
 processor_version varchar(32) NOT NULL,
 document jsonb NOT NULL DEFAULT '{}',
 shot_ids jsonb NOT NULL DEFAULT '[]',
 status varchar(16) NOT NULL DEFAULT 'queued',
 attempt bigint NOT NULL DEFAULT 1,
 total_frames bigint NOT NULL DEFAULT 0,
 completed_frames bigint NOT NULL DEFAULT 0,
 has_audio boolean,
 error text NOT NULL DEFAULT '',
 created_at timestamptz,
 updated_at timestamptz
);
CREATE UNIQUE INDEX uk_reference_preparation_recipe ON drama_reference_preparations(reference_id,recipe_hash);
CREATE INDEX idx_reference_preparation_page ON drama_reference_preparations(reference_id);
CREATE TABLE drama_reference_preparation_requests (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL,
 drama_id bigint NOT NULL,
 reference_id bigint NOT NULL,
 preparation_id bigint NOT NULL,
 request_key varchar(128) NOT NULL,
 kind varchar(16) NOT NULL,
 fingerprint varchar(64) NOT NULL,
 created_at timestamptz
);
CREATE UNIQUE INDEX uk_reference_preparation_request ON drama_reference_preparation_requests(reference_id,request_key);
CREATE INDEX idx_drama_reference_preparation_requests_preparation_id ON drama_reference_preparation_requests(preparation_id);
CREATE TABLE drama_reference_preparation_frames (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL,
 preparation_id bigint NOT NULL,
 shot_id varchar(128) NOT NULL,
 start_ms bigint NOT NULL,
 end_ms bigint NOT NULL,
 at_ms bigint NOT NULL,
 asset_id bigint NOT NULL DEFAULT 0,
 cache_id bigint NOT NULL,
 created_at timestamptz,
 updated_at timestamptz
);
CREATE UNIQUE INDEX uk_reference_preparation_frame ON drama_reference_preparation_frames(preparation_id,shot_id,at_ms);
CREATE INDEX idx_reference_preparation_frame_page ON drama_reference_preparation_frames(preparation_id);
CREATE TABLE drama_reference_frame_caches (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL,
 storage_token varchar(48) NOT NULL,
 cache_key varchar(64) NOT NULL,
 source_asset_id bigint NOT NULL,
 start_ms bigint NOT NULL,
 end_ms bigint NOT NULL,
 at_ms bigint NOT NULL,
 processor_version varchar(32) NOT NULL,
 asset_id bigint NOT NULL DEFAULT 0,
 created_at timestamptz,
 updated_at timestamptz
);
CREATE UNIQUE INDEX uk_reference_frame_cache ON drama_reference_frame_caches(user_id,cache_key);
ALTER TABLE drama_reference_drafts ADD COLUMN preparation_id bigint NOT NULL DEFAULT 0;
