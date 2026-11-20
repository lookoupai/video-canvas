CREATE TABLE marketing_remake_intents (
 id bigserial PRIMARY KEY,
 user_id bigint NOT NULL,
 request_key varchar(128) NOT NULL,
 task_key varchar(128) NOT NULL,
 plan_id bigint NOT NULL,
 shot_id bigint NOT NULL,
 revision bigint NOT NULL,
 node_id varchar(64) NOT NULL,
 model_id varchar(128) NOT NULL,
 input jsonb NOT NULL,
 shot_snapshot jsonb NOT NULL,
 reference_audio boolean NOT NULL DEFAULT false,
 created_at timestamptz
);
CREATE UNIQUE INDEX uk_remake_intent_key ON marketing_remake_intents (user_id, request_key);
CREATE UNIQUE INDEX uk_remake_intent_task ON marketing_remake_intents (task_key);
CREATE INDEX idx_remake_intent_plan ON marketing_remake_intents (plan_id);
