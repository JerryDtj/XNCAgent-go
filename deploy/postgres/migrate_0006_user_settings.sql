-- 用户设置表（Agent 域；前端零状态，本表为唯一权威）
-- 设计稿：doc/架构/详细设计/20261002-工具插件层与情绪音乐设计稿.md §2.2
-- 本文件为权威副本（initdb.d）；Python 仓 deploy/postgres/ 保留镜像。
-- 注：同目录已有 migrate_0005_message_interrupted.sql，两文件可并存（initdb.d 按文件名排序执行）。
-- user_id 用 BIGINT 对齐 users(id) BIGSERIAL（设计稿写 INT，同库外键类型须一致）。
CREATE TABLE IF NOT EXISTS user_settings (
    user_id       BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,  -- 用户 ID
    music_enabled BOOLEAN NOT NULL DEFAULT TRUE,   -- 音乐播放开关（使用网络流量）
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE user_settings IS '用户设置表：前端零状态，本表为唯一权威';
COMMENT ON COLUMN user_settings.user_id IS '用户 ID，主键兼外键，级联删除';
COMMENT ON COLUMN user_settings.music_enabled IS '音乐播放开关（使用网络流量），默认 true';
COMMENT ON COLUMN user_settings.updated_at IS '最近更新时间';
