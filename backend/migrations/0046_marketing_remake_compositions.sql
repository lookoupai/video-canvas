-- 合成任务保留独立快照，重排和重新采用仅影响下一次提交。
CREATE TABLE marketing_remake_compositions (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    plan_id bigint NOT NULL,
    idempotency_key varchar(128) NOT NULL,
    revision bigint NOT NULL,
    all_shots boolean NOT NULL,
    parts jsonb NOT NULL,
    status varchar(16) NOT NULL,
    attempt bigint NOT NULL,
    asset_id bigint,
    error text NOT NULL DEFAULT '',
    created_at timestamptz
);
CREATE UNIQUE INDEX uk_remake_composition_key ON marketing_remake_compositions(plan_id, idempotency_key);
