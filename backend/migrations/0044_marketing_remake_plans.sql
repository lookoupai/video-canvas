CREATE TABLE marketing_remake_plans (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    project_id bigint NOT NULL,
    script_id bigint NOT NULL,
    script_revision bigint NOT NULL,
    revision bigint NOT NULL DEFAULT 1,
    original jsonb NOT NULL,
    data jsonb NOT NULL,
    created_at timestamptz,
    updated_at timestamptz
);
CREATE UNIQUE INDEX uk_mkt_plan_script_rev ON marketing_remake_plans (script_id, script_revision);
