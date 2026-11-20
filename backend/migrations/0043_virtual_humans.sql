-- 真人库：真人与它的图片，每张图片单独在方舟虚拟人像库备案。
-- 不用外键：删除真人时由服务在同一事务里先删图片行再删真人；图片只是 assets 行的引用，不复制文件。
CREATE TABLE virtual_humans (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    name varchar(30) NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE INDEX idx_virtual_humans_user ON virtual_humans USING btree (user_id);

CREATE TABLE virtual_human_images (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    human_id bigint NOT NULL,
    asset_id bigint NOT NULL,
    sort bigint NOT NULL DEFAULT 0,
    -- 备案所在渠道；生成时只有走这个渠道的任务才用 asset:// 引用
    channel_key varchar(64) NOT NULL DEFAULT '',
    -- 方舟素材 ID
    ark_asset_id varchar(128) NOT NULL DEFAULT '',
    -- 备案状态：pending / active / failed
    status varchar(16) NOT NULL DEFAULT 'pending',
    error varchar(500) NOT NULL DEFAULT '',
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

-- 同一张素材在一个用户的真人库里最多出现一次；生成时也按 (user_id, asset_id) 查备案
CREATE UNIQUE INDEX uk_virtual_human_images_user_asset ON virtual_human_images USING btree (user_id, asset_id);

CREATE INDEX idx_virtual_human_images_human ON virtual_human_images USING btree (human_id);

-- 渠道的虚拟人像库配置（素材组 ID、项目名、是否启用）；AK/SK 存在 ai_secrets
ALTER TABLE ai_channels ADD COLUMN filing_json json NOT NULL DEFAULT '{}';
