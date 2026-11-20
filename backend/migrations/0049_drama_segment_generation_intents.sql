CREATE TABLE drama_segment_generation_intents (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    request_key varchar(128) NOT NULL,
    task_key varchar(128) NOT NULL,
    drama_id bigint NOT NULL,
    canvas_id bigint NOT NULL,
    episode_id bigint NOT NULL,
    segment_id bigint NOT NULL,
    segment_revision bigint NOT NULL,
    fingerprint varchar(64) NOT NULL,
    model_id varchar(128) NOT NULL,
    input jsonb NOT NULL,
    remake_snapshot jsonb NOT NULL,
    created_at timestamptz
);
CREATE UNIQUE INDEX uk_drama_segment_intent_request ON drama_segment_generation_intents (user_id, request_key);
CREATE UNIQUE INDEX idx_drama_segment_generation_intents_task_key ON drama_segment_generation_intents (task_key);
CREATE INDEX idx_drama_segment_generation_intents_drama_id ON drama_segment_generation_intents (drama_id);
CREATE INDEX idx_drama_segment_generation_intents_segment_id ON drama_segment_generation_intents (segment_id);
