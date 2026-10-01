-- assistant 行在客户端断开时标记截断。已有库补列，默认 false。
ALTER TABLE chat_messages
    ADD COLUMN IF NOT EXISTS interrupted BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN chat_messages.interrupted IS 'assistant 行为 true 表示客户端断开，content 只是已生成的部分';
