CREATE TABLE marketing_remake_clips (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    source_id bigint NOT NULL,
    recipe varchar(64) NOT NULL,
    asset_id bigint NOT NULL
);
CREATE UNIQUE INDEX uk_mkt_remake_clip ON marketing_remake_clips (user_id, source_id, recipe);
