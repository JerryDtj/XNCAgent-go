-- 8. 聊天会话（Agent 域；一期提前，原规划二期）
CREATE TABLE IF NOT EXISTS chat_sessions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(100) NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    message_count INT NOT NULL DEFAULT 0,
    last_message_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_chat_sessions_user_time ON chat_sessions(user_id, last_message_at DESC);

COMMENT ON TABLE chat_sessions IS '聊天会话列表；title 为空表示 LLM 标题未生成，前端回退首条消息截断';
COMMENT ON COLUMN chat_sessions.id IS '会话主键';
COMMENT ON COLUMN chat_sessions.user_id IS '所属用户，级联删除';
COMMENT ON COLUMN chat_sessions.title IS 'LLM 生成的短标题，≤12 字，可改名';
COMMENT ON COLUMN chat_sessions.status IS 'active / deleted（一期物理删除，status 预留）';
COMMENT ON COLUMN chat_sessions.message_count IS '消息行数（user+assistant 各计 1），每落一条消息同步 +1';
COMMENT ON COLUMN chat_sessions.last_message_at IS '最近消息时间，排序依据；消息落库时同步更新';
COMMENT ON COLUMN chat_sessions.created_at IS '创建时间';
COMMENT ON COLUMN chat_sessions.updated_at IS '最近更新时间';

-- 9. 聊天消息（一轮 = 两行：user + assistant）
CREATE TABLE IF NOT EXISTS chat_messages (
    id BIGSERIAL PRIMARY KEY,
    session_id BIGINT NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(20) NOT NULL,                -- user / assistant
    content TEXT NOT NULL,
    scene VARCHAR(20),                        -- 该轮判定场景，analytics 用，不参与还原
    rewritten_query TEXT,                     -- 改写后的 query，调试/分析用
    degraded BOOLEAN NOT NULL DEFAULT FALSE,  -- 该轮是否走了 bge 降级通道
    emotion_alert BOOLEAN NOT NULL DEFAULT FALSE,
    token_count INT,                          -- 计费预留；流式结束拿到 usage 后回填 assistant 行
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_chat_messages_session_created ON chat_messages(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_chat_messages_user_created ON chat_messages(user_id, created_at);

COMMENT ON TABLE chat_messages IS '聊天消息；assistant 行缺失 = 该轮生成失败';
COMMENT ON COLUMN chat_messages.id IS '消息主键';
COMMENT ON COLUMN chat_messages.session_id IS '所属会话，级联删除';
COMMENT ON COLUMN chat_messages.user_id IS '所属用户（冗余，按用户查历史/越权校验用），级联删除';
COMMENT ON COLUMN chat_messages.role IS '消息角色：user / assistant';
COMMENT ON COLUMN chat_messages.content IS '消息正文';
COMMENT ON COLUMN chat_messages.scene IS '该轮判定场景（6 值之一），仅统计/回放用，还原不读';
COMMENT ON COLUMN chat_messages.rewritten_query IS 'LLM 改写后的 query，调试/分析用';
COMMENT ON COLUMN chat_messages.degraded IS '该轮是否走了 bge 本地降级通道（非 LLM 判定）';
COMMENT ON COLUMN chat_messages.emotion_alert IS '该轮是否触发情绪预警（安全树洞）';
COMMENT ON COLUMN chat_messages.token_count IS 'token 用量，计费预留；流式结束拿到 usage 后回填 assistant 行';
COMMENT ON COLUMN chat_messages.created_at IS '落库时间';

-- 10. 会话摘要（一会话一条；增量刷新，向量副本进 chroma chat_summaries）
CREATE TABLE IF NOT EXISTS chat_session_summaries (
    session_id BIGINT PRIMARY KEY REFERENCES chat_sessions(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    summary TEXT NOT NULL DEFAULT '',
    message_count INT NOT NULL DEFAULT 0,      -- 摘要已覆盖的消息数（进度标记）
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_chat_session_summaries_user ON chat_session_summaries(user_id);

COMMENT ON TABLE chat_session_summaries IS '会话摘要：每跨 10 条消息异步刷新；embedding 存 chroma collection chat_summaries，库内不存向量（不引 pgvector）';
COMMENT ON COLUMN chat_session_summaries.session_id IS '所属会话，主键兼外键，级联删除';
COMMENT ON COLUMN chat_session_summaries.user_id IS '所属用户（冗余，隔离/删除用），级联删除';
COMMENT ON COLUMN chat_session_summaries.summary IS '摘要正文，≤100 字；空 = 尚未生成';
COMMENT ON COLUMN chat_session_summaries.message_count IS '摘要已覆盖的消息数（进度标记），刷新时更新';
COMMENT ON COLUMN chat_session_summaries.updated_at IS '摘要最近刷新时间';