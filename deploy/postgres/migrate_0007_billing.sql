-- 1. 透支语义 + total 不变式：原 CHECK 要求 available >= 0，与 Q2 冲突。
--    total（v3.1 对账锚）保留，CHECK 升级为不变式，每个事务由 DB 强制
ALTER TABLE accounts DROP CONSTRAINT ck_accounts_non_negative;
ALTER TABLE accounts ADD CONSTRAINT ck_accounts_balance_invariant
    CHECK (frozen >= 0 AND total >= 0 AND total = available + frozen);

-- 2. transactions 补 meta 列（tokens/费率/prehold_id 等扩展信息）
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS meta JSONB NOT NULL DEFAULT '{}';

-- 3. 预扣台账（settle/release 的依据，对账的第二锚）
CREATE TABLE preholds (
    id           UUID PRIMARY KEY,                  -- 即 X-Prehold-Id
    request_id   VARCHAR(64) NOT NULL,              -- 前端幂等键（uuid）
    user_id      BIGINT NOT NULL REFERENCES users(id),
    amount       BIGINT NOT NULL,                   -- 预扣额（分）
    status       VARCHAR(20) NOT NULL DEFAULT 'pending',  -- pending/settled/released
    expires_at   TIMESTAMPTZ NOT NULL,              -- now()+30min
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    settled_at   TIMESTAMPTZ
);
-- 幂等锚：同一 (user_id, request_id) 最多一个"进行中"预扣。
-- 部分唯一索引（带 WHERE）：只对 status='pending' 的行生效——
-- settled/released 后的旧行不挡前端重试（视为新请求），
-- 双击/并发重试则被索引挡住 → ON CONFLICT DO NOTHING → 复用 pending 行。
-- 注意：表内 CONSTRAINT UNIQUE 不支持 WHERE，必须用独立 CREATE UNIQUE INDEX。
CREATE UNIQUE INDEX uq_preholds_user_request_pending
    ON preholds(user_id, request_id) WHERE status = 'pending';
CREATE INDEX idx_preholds_pending ON preholds(status, expires_at) WHERE status='pending';

COMMENT ON TABLE preholds IS '用户金额预扣表';
COMMENT ON COLUMN preholds.id IS '主键';
COMMENT ON COLUMN preholds.request_id IS '请求id,前端幂等键（uuid）';
COMMENT ON COLUMN preholds.user_id IS '所属用户';
COMMENT ON COLUMN preholds.amount IS '预扣金额（分）';
COMMENT ON COLUMN preholds.status IS '预扣状态:pending/settled/released，pending:预扣中，settled:已结算，released:已释放';
COMMENT ON COLUMN preholds.expires_at IS '过期时间';
COMMENT ON COLUMN preholds.created_at IS '创建时间';
COMMENT ON COLUMN preholds.settled_at IS '结算时间';

-- 4. 用量上报（Python 写，Go 结算；幂等锚 = prehold_id，一问一预扣一行）
CREATE TABLE usage (
    id                BIGSERIAL PRIMARY KEY,
    prehold_id        UUID NOT NULL REFERENCES preholds(id),
    user_id           BIGINT NOT NULL,
    message_id        BIGINT,                       -- 消息 id，后续补关联
    model             TEXT NOT NULL,
    prompt_tokens     INT NOT NULL,
    completion_tokens INT NOT NULL,
    cache_meta        JSONB NOT NULL DEFAULT '{}',  -- cached_tokens 等，二期缓存定价用
    estimated         BOOLEAN NOT NULL DEFAULT false,  -- 中断估算轮次打标
    settled           BOOLEAN NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_usage_prehold UNIQUE (prehold_id)
);
CREATE INDEX idx_usage_unsettled ON usage(settled) WHERE settled = false;

COMMENT ON TABLE usage IS 'usage，用量上报表';
COMMENT ON COLUMN usage.id IS '主键';
COMMENT ON COLUMN usage.prehold_id IS '预扣id';
COMMENT ON COLUMN usage.user_id IS '所属用户';
COMMENT ON COLUMN usage.message_id IS '消息id';
COMMENT ON COLUMN usage.model IS '模型';
COMMENT ON COLUMN usage.prompt_tokens IS '提示词花费tokens';
COMMENT ON COLUMN usage.completion_tokens IS '完成词花费tokens';
COMMENT ON COLUMN usage.cache_meta IS '缓存meta，cached_tokens 等，二期缓存定价用';
COMMENT ON COLUMN usage.estimated IS '是否中断估算轮次标记';
COMMENT ON COLUMN usage.settled IS '是否已结算标记';
COMMENT ON COLUMN usage.created_at IS '创建时间';