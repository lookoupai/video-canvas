CREATE TABLE drama_reference_draft_applications (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    drama_id BIGINT NOT NULL,
    reference_id BIGINT NOT NULL,
    draft_id BIGINT NOT NULL,
    draft_revision BIGINT NOT NULL,
    request_key VARCHAR(128) NOT NULL,
    fingerprint VARCHAR(64) NOT NULL,
    plan_digest VARCHAR(64) NOT NULL,
    quote JSONB NOT NULL,
    response JSONB NOT NULL,
    adoption_id BIGINT NOT NULL,
    episode_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX uk_drama_reference_apply_key ON drama_reference_draft_applications (user_id, request_key);
CREATE UNIQUE INDEX uk_drama_reference_apply_draft ON drama_reference_draft_applications (user_id, draft_id, draft_revision);
CREATE INDEX idx_drama_reference_draft_applications_drama_id ON drama_reference_draft_applications (drama_id);
CREATE INDEX idx_drama_reference_draft_applications_reference_id ON drama_reference_draft_applications (reference_id);
CREATE INDEX idx_drama_reference_draft_applications_adoption_id ON drama_reference_draft_applications (adoption_id);
CREATE INDEX idx_drama_reference_draft_applications_episode_id ON drama_reference_draft_applications (episode_id);

CREATE TABLE drama_reference_draft_generations (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    drama_id BIGINT NOT NULL,
    reference_id BIGINT NOT NULL,
    draft_id BIGINT NOT NULL,
    application_id BIGINT NOT NULL,
    request_key VARCHAR(128) NOT NULL,
    fingerprint VARCHAR(64) NOT NULL,
    plan_digest VARCHAR(64) NOT NULL,
    quote JSONB NOT NULL,
    response JSONB NOT NULL,
    status VARCHAR(16) NOT NULL,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX uk_drama_reference_generation_key ON drama_reference_draft_generations (user_id, request_key);
CREATE INDEX idx_drama_reference_draft_generations_drama_id ON drama_reference_draft_generations (drama_id);
CREATE INDEX idx_drama_reference_draft_generations_reference_id ON drama_reference_draft_generations (reference_id);
CREATE INDEX idx_drama_reference_draft_generations_draft_id ON drama_reference_draft_generations (draft_id);
CREATE INDEX idx_drama_reference_draft_generations_application_id ON drama_reference_draft_generations (application_id);
