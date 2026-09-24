-- XNCAgent-go 一期库表（对齐 v3.1 §5 / §7.3 / §15）
-- 数据库: xncagent（compose 环境变量 POSTGRES_DB）
-- 不含：gift/ab/画像/独立商城、chat_sessions、三层对账流水（二期再补）
-- 本文件由 deploy/docker-compose.yaml 挂到 /docker-entrypoint-initdb.d/，仅空数据卷首次执行。

-- 1. 用户
CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    email VARCHAR(100) NOT NULL,
    phone VARCHAR(20),
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_users_email UNIQUE (email)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_phone ON users(phone) WHERE phone IS NOT NULL;

COMMENT ON TABLE users IS '用户：一期邮箱验证码登录（未注册自动建号）；phone 预留给二期/三期短信登录';
COMMENT ON COLUMN users.id IS '用户主键';
COMMENT ON COLUMN users.email IS '登录邮箱，唯一';
COMMENT ON COLUMN users.phone IS '预留手机号，非空时唯一；二期/三期短信验证码登录用';
COMMENT ON COLUMN users.status IS 'active / banned';
COMMENT ON COLUMN users.created_at IS '创建时间';
COMMENT ON COLUMN users.updated_at IS '更新时间';

-- 2. JWT Refresh Token
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_refresh_tokens_token UNIQUE (token)
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id);

COMMENT ON TABLE refresh_tokens IS 'Refresh Token，登出时删除以实现吊销';
COMMENT ON COLUMN refresh_tokens.id IS '主键';
COMMENT ON COLUMN refresh_tokens.user_id IS '所属用户';
COMMENT ON COLUMN refresh_tokens.token IS 'Refresh Token 字符串，唯一';
COMMENT ON COLUMN refresh_tokens.expires_at IS '过期时间';
COMMENT ON COLUMN refresh_tokens.created_at IS '签发时间';

-- 3. 积分账户（与 Redis HASH balance:{user_id} 同构）
CREATE TABLE IF NOT EXISTS accounts (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    total BIGINT NOT NULL DEFAULT 0,
    available BIGINT NOT NULL DEFAULT 0,
    frozen BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_accounts_user_id UNIQUE (user_id),
    CONSTRAINT ck_accounts_non_negative CHECK (total >= 0 AND available >= 0 AND frozen >= 0)
);

COMMENT ON TABLE accounts IS '积分账户投影：available = total - frozen';
COMMENT ON COLUMN accounts.id IS '账户主键';
COMMENT ON COLUMN accounts.user_id IS '所属用户，一人一户';
COMMENT ON COLUMN accounts.total IS '总余额';
COMMENT ON COLUMN accounts.available IS '可用';
COMMENT ON COLUMN accounts.frozen IS '预扣冻结';
COMMENT ON COLUMN accounts.created_at IS '开户时间';
COMMENT ON COLUMN accounts.updated_at IS '余额最近变更时间';

-- 4. 套餐（积分 SKU，不是激活码商城）
CREATE TABLE IF NOT EXISTS packages (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(32) NOT NULL,
    name VARCHAR(50) NOT NULL,
    type VARCHAR(20) NOT NULL,
    price_cents BIGINT NOT NULL,
    token_amount BIGINT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_packages_code UNIQUE (code)
);

COMMENT ON TABLE packages IS '充值套餐；价格单位分';
COMMENT ON COLUMN packages.id IS '套餐主键';
COMMENT ON COLUMN packages.code IS '接口 package_code：trial / monthly / yearly';
COMMENT ON COLUMN packages.name IS '展示名称';
COMMENT ON COLUMN packages.type IS '套餐类型：trial / monthly / yearly';
COMMENT ON COLUMN packages.price_cents IS '价格（分）';
COMMENT ON COLUMN packages.token_amount IS '到账积分';
COMMENT ON COLUMN packages.is_active IS '是否上架可售';
COMMENT ON COLUMN packages.created_at IS '创建时间';

INSERT INTO packages (code, name, type, price_cents, token_amount) VALUES
    ('trial', '体验包', 'trial', 990, 100),
    ('monthly', '月卡积分包', 'monthly', 3900, 500),
    ('yearly', '年卡积分包', 'yearly', 29900, 6000)
ON CONFLICT (code) DO NOTHING;

-- 5. 交易流水（Kafka worker 落库投影；event_id 为三方对账锚点）
CREATE TABLE IF NOT EXISTS transactions (
    id BIGSERIAL PRIMARY KEY,
    event_id VARCHAR(64) NOT NULL,
    seq_no BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id),
    prehold_id VARCHAR(64),
    promotion_id BIGINT,
    idempotency_key VARCHAR(64) NOT NULL,
    flow_version INT NOT NULL DEFAULT 1,
    type VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'COMPLETED',
    amount BIGINT NOT NULL,
    balance_before BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    frozen_before BIGINT NOT NULL DEFAULT 0,
    frozen_after BIGINT NOT NULL DEFAULT 0,
    description VARCHAR(255),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_transactions_event_id UNIQUE (event_id),
    CONSTRAINT uq_transactions_idempotency UNIQUE (user_id, idempotency_key, type)
);

CREATE INDEX IF NOT EXISTS idx_transactions_user_id_type ON transactions(user_id, type);
CREATE INDEX IF NOT EXISTS idx_transactions_created_at ON transactions(created_at);
CREATE INDEX IF NOT EXISTS idx_transactions_prehold ON transactions(prehold_id);

COMMENT ON TABLE transactions IS '资金流水投影；权威账本在 Kafka balance_events';
COMMENT ON COLUMN transactions.id IS '流水主键';
COMMENT ON COLUMN transactions.event_id IS '全局唯一，贯穿 Redis Stream / Kafka / DB';
COMMENT ON COLUMN transactions.seq_no IS '用户内单调序号，重放排序';
COMMENT ON COLUMN transactions.user_id IS '所属用户';
COMMENT ON COLUMN transactions.prehold_id IS '预扣记录 ID，仅结算/取消时关联';
COMMENT ON COLUMN transactions.promotion_id IS '活动 ID，仅活动奖励流水关联';
COMMENT ON COLUMN transactions.idempotency_key IS '客户端幂等键，防重复处理';
COMMENT ON COLUMN transactions.flow_version IS '1=Redis 链路 / 2=DB 降级';
COMMENT ON COLUMN transactions.type IS 'RECHARGE / PREHOLD / SETTLE / CANCEL / REFUND / PROMOTION / RECONCILE_FIX';
COMMENT ON COLUMN transactions.status IS 'COMPLETED=已落库；PENDING=对账中间态';
COMMENT ON COLUMN transactions.amount IS '积分变动，正增负减';
COMMENT ON COLUMN transactions.balance_before IS '交易前总余额快照';
COMMENT ON COLUMN transactions.balance_after IS '交易后总余额快照';
COMMENT ON COLUMN transactions.frozen_before IS '交易前冻结余额快照';
COMMENT ON COLUMN transactions.frozen_after IS '交易后冻结余额快照';
COMMENT ON COLUMN transactions.description IS '流水说明';
COMMENT ON COLUMN transactions.created_at IS '落库时间';
COMMENT ON COLUMN transactions.updated_at IS '更新时间';

-- 6. 好友度 / 官职（一期不拆 Member 进程）
CREATE TABLE IF NOT EXISTS user_intimacy (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    current_level INT NOT NULL DEFAULT 1,
    current_score INT NOT NULL DEFAULT 0,
    chat_count INT NOT NULL DEFAULT 0,
    last_chat_at TIMESTAMP,
    version INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_user_intimacy_user_id UNIQUE (user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_intimacy_level ON user_intimacy(current_level);
CREATE INDEX IF NOT EXISTS idx_user_intimacy_score ON user_intimacy(current_score DESC);

COMMENT ON TABLE user_intimacy IS '官职：1里正 2县令 3知府 4丞相 5皇帝；version 乐观锁';
COMMENT ON COLUMN user_intimacy.id IS '主键';
COMMENT ON COLUMN user_intimacy.user_id IS '所属用户，一人一条';
COMMENT ON COLUMN user_intimacy.current_level IS '1-5 对应里正到皇帝';
COMMENT ON COLUMN user_intimacy.current_score IS '当前好友度分数';
COMMENT ON COLUMN user_intimacy.chat_count IS '累计对话次数';
COMMENT ON COLUMN user_intimacy.last_chat_at IS '最近一次对话时间';
COMMENT ON COLUMN user_intimacy.version IS '更新 WHERE version = old_version';
COMMENT ON COLUMN user_intimacy.created_at IS '创建时间';
COMMENT ON COLUMN user_intimacy.updated_at IS '最近更新时间';

-- 7. Kafka worker 水位
CREATE TABLE IF NOT EXISTS water_mark (
    partition_no INT PRIMARY KEY,
    max_offset BIGINT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE water_mark IS 'balance_events 消费水位；提交位点在事务之后';
COMMENT ON COLUMN water_mark.partition_no IS 'Kafka 分区号';
COMMENT ON COLUMN water_mark.max_offset IS '已安全落库的最大 offset';
COMMENT ON COLUMN water_mark.updated_at IS '水位最近推进时间';
