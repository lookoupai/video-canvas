CREATE TABLE drama_reference_states (
 drama_id bigint PRIMARY KEY,
 user_id bigint NOT NULL,
 request_key varchar(128) NOT NULL,
 revision bigint NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX uk_drama_reference_create ON drama_reference_states(user_id,request_key);
CREATE TABLE drama_reference_episodes (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL,
 drama_id bigint NOT NULL,
 request_key varchar(128) NOT NULL,
 seq bigint NOT NULL,
 title varchar(255) NOT NULL,
 asset_id bigint NOT NULL,
 duration_ms bigint NOT NULL,
 revision bigint NOT NULL DEFAULT 1,
 attempt bigint NOT NULL DEFAULT 0,
 status varchar(16) NOT NULL DEFAULT 'idle',
 error text NOT NULL DEFAULT '',
 method varchar(32) NOT NULL DEFAULT '',
 input bytea,
 document jsonb,
 bindings jsonb NOT NULL DEFAULT '{}',
 updated_at timestamptz
);
CREATE INDEX idx_drama_reference_order ON drama_reference_episodes(drama_id,seq);
CREATE UNIQUE INDEX uk_drama_reference_episode_key ON drama_reference_episodes(drama_id,request_key);
CREATE TABLE drama_reference_revisions (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL,
 reference_id bigint NOT NULL,
 revision bigint NOT NULL,
 method varchar(32) NOT NULL,
 document jsonb NOT NULL,
 bindings jsonb NOT NULL,
 created_at timestamptz
);
CREATE UNIQUE INDEX uk_drama_reference_revision ON drama_reference_revisions(reference_id,revision);
CREATE TABLE drama_reference_roles (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL,
 drama_id bigint NOT NULL,
 name varchar(128) NOT NULL,
 target_node_id varchar(128) NOT NULL DEFAULT '',
 revision bigint NOT NULL DEFAULT 1
);
CREATE INDEX idx_drama_reference_roles_drama_id ON drama_reference_roles(drama_id);
