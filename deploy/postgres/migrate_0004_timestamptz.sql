-- 存量 TIMESTAMP（无时区）改为 TIMESTAMPTZ。只执行一次。
-- 改类型时，PostgreSQL 按当前会话时区理解钟面数字。本脚本固定 Asia/Shanghai。
--
-- 2026-10-01 核对过两类存量，不能整库都 +8 小时：
-- 1. 会话时区 UTC 下用 CURRENT_TIMESTAMP 写下的钟面，数字是 UTC。
--    改类型前 +8 小时，再按东八区理解，真实时刻才不变。
--    packages / chat_sessions / chat_messages / chat_session_summaries
-- 2. Go 连接串已经是 TimeZone=Asia/Shanghai，time.Now() 写下的钟面就是东八区。
--    这些列只改类型。再 +8 会把注册时间和 refresh 过期时间拨快 8 小时。
--    users / accounts / user_intimacy / refresh_tokens / transactions / water_mark

BEGIN;

SET TIME ZONE 'Asia/Shanghai';

DO $$
DECLARE
    col_type text;
BEGIN
    SELECT c.data_type INTO col_type
    FROM information_schema.columns c
    WHERE c.table_schema = 'public'
      AND c.table_name = 'chat_messages'
      AND c.column_name = 'created_at';

    IF col_type IS NULL THEN
        RAISE EXCEPTION 'chat_messages 不存在，请先执行 init.sql 和 migrate_0003';
    END IF;

    -- 空库重建时 init.sql / migrate_0003 已经建成 timestamptz。
    -- 这时再 +8 小时会把 packages 种子行拨快，所以直接跳过，更新 0 行。
    IF col_type = 'timestamp with time zone' THEN
        RAISE NOTICE 'chat_messages.created_at 已是 timestamptz，跳过存量修正（更新 0 行）';
        RETURN;
    END IF;

    UPDATE packages
    SET created_at = created_at + interval '8 hours';

    UPDATE chat_sessions
    SET created_at = created_at + interval '8 hours',
        updated_at = updated_at + interval '8 hours',
        last_message_at = last_message_at + interval '8 hours';

    UPDATE chat_messages
    SET created_at = created_at + interval '8 hours';

    UPDATE chat_session_summaries
    SET updated_at = updated_at + interval '8 hours';

    ALTER TABLE users
        ALTER COLUMN created_at TYPE timestamptz USING created_at AT TIME ZONE 'Asia/Shanghai',
        ALTER COLUMN updated_at TYPE timestamptz USING updated_at AT TIME ZONE 'Asia/Shanghai';
    ALTER TABLE refresh_tokens
        ALTER COLUMN expires_at TYPE timestamptz USING expires_at AT TIME ZONE 'Asia/Shanghai',
        ALTER COLUMN created_at TYPE timestamptz USING created_at AT TIME ZONE 'Asia/Shanghai';
    ALTER TABLE accounts
        ALTER COLUMN created_at TYPE timestamptz USING created_at AT TIME ZONE 'Asia/Shanghai',
        ALTER COLUMN updated_at TYPE timestamptz USING updated_at AT TIME ZONE 'Asia/Shanghai';
    ALTER TABLE packages
        ALTER COLUMN created_at TYPE timestamptz USING created_at AT TIME ZONE 'Asia/Shanghai';
    ALTER TABLE transactions
        ALTER COLUMN created_at TYPE timestamptz USING created_at AT TIME ZONE 'Asia/Shanghai',
        ALTER COLUMN updated_at TYPE timestamptz USING updated_at AT TIME ZONE 'Asia/Shanghai';
    ALTER TABLE user_intimacy
        ALTER COLUMN last_chat_at TYPE timestamptz USING last_chat_at AT TIME ZONE 'Asia/Shanghai',
        ALTER COLUMN created_at TYPE timestamptz USING created_at AT TIME ZONE 'Asia/Shanghai',
        ALTER COLUMN updated_at TYPE timestamptz USING updated_at AT TIME ZONE 'Asia/Shanghai';
    ALTER TABLE water_mark
        ALTER COLUMN updated_at TYPE timestamptz USING updated_at AT TIME ZONE 'Asia/Shanghai';
    ALTER TABLE chat_sessions
        ALTER COLUMN last_message_at TYPE timestamptz USING last_message_at AT TIME ZONE 'Asia/Shanghai',
        ALTER COLUMN created_at TYPE timestamptz USING created_at AT TIME ZONE 'Asia/Shanghai',
        ALTER COLUMN updated_at TYPE timestamptz USING updated_at AT TIME ZONE 'Asia/Shanghai';
    ALTER TABLE chat_messages
        ALTER COLUMN created_at TYPE timestamptz USING created_at AT TIME ZONE 'Asia/Shanghai';
    ALTER TABLE chat_session_summaries
        ALTER COLUMN updated_at TYPE timestamptz USING updated_at AT TIME ZONE 'Asia/Shanghai';
END $$;

COMMIT;
