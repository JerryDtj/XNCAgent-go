-- 已有数据卷不会重跑 init.sql。删掉已无用的密码列：
-- docker exec -i xnc-postgres psql -U xnc -d xncagent < deploy/postgres/migrate_0002_email_code_login.sql

ALTER TABLE users DROP COLUMN IF EXISTS password_hash;

COMMENT ON TABLE users IS '用户：一期邮箱验证码登录（未注册自动建号）；phone 预留给二期/三期短信登录';
COMMENT ON COLUMN users.phone IS '预留手机号，非空时唯一；二期/三期短信验证码登录用';
