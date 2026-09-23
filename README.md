# XNCAgent-go

XNCAgent 是「小喜子」的专属 Agent 项目：一名戏精附体的赛博弄臣。本仓库是它的 **Go 微服务层**，负责高并发网关、用户体系、积分账务与商业化闭环。

对话状态机、LLM、RAG、MCP 工具在 Python 仓 [JerryDtj/XNCAgent](https://github.com/JerryDtj/XNCAgent)。两仓一起构成完整系统。

| 仓库 | 职责 |
|------|------|
| [XNCAgent](https://github.com/JerryDtj/XNCAgent) | Python Agent Core：对话、RAG、MCP、Sandbox |
| **XNCAgent-go**（本仓） | Gateway / User / Commercial；本地 compose 与一期库表 |

人设与说话规则（反差双簧、情绪雷达、安全树洞）见 Python 仓 [README](https://github.com/JerryDtj/XNCAgent/blob/main/README.md)。

## 当前状态

本仓目前是 **一期骨架 + 本地基础设施**：

- 已有：`GET /health`、注册登录 JWT（`make run`）、统一 JSON 信封、`internal/database` 连接池、`deploy/docker-compose.yaml`、`deploy/postgres/init.sql`
- 还没有：gRPC proto、业务镜像；Commercial 逻辑尚未写
- 一期进程目标：Gateway + User + Commercial（worker 可先同进程）；好友度 / 官职表并进 User，不拆独立 Member 进程
- 二期再拆：Promotion、Recommend、Profile、AB、Risk

完整设计见 [docs/计划](docs/计划/XNCAgent_Go微服务架构设计文档-v3.1.md)，落地节奏见 [一期 / 二期清单](docs/计划/XNCAgent_面试冲刺计划清单.md)。

## 架构概览

```
Client (HTTP/JSON)
        │
        ▼
API Gateway  Gin :8199
  JWT · RequestID · Rate Limit · Feature Flag · SSE 透传
        │
        ├── gRPC ──► User (:50051) / Commercial (:50053) / …
        │
        └── HTTP + SSE ──► Python Agent Core
                              /api/v1/agent/chat
                              /api/v1/agent/recommend
                              /health

资金链路（v3.1）：
  Redis（扣款现场） → Kafka balance_events（权威账本） → Postgres（异步投影）
```

账本唯一：Kafka 是全系统唯一权威账本；Redis 是现场投影；DB 是异步投影。对话扣费走预扣（Prehold）→ Python SSE 聊天 → 结算（Settle）。降级开关 `commercial_flow_mode`：`redis` | `db_direct`。

一期会员等级用古代官职：里正 → 县令 → 知府 → 丞相 → 皇帝。

## 环境准备

- Go：以 [`go.mod`](go.mod) 为准（当前 `go 1.27.1`）
- Docker Compose：仅拉起基础设施，**不含** Gateway / User / Commercial 业务镜像
- Python Agent：另仓独立运行，Gateway 后续通过 HTTP + SSE 调用

## 本地基础设施

```bash
make up
# 等价：docker compose -f deploy/docker-compose.yaml up -d
```

| 服务 | 地址 | 说明 |
|------|------|------|
| Postgres 15 | `localhost:5432` | 用户 `xnc`，库 `xncagent`，密码 `xnc123`（**仅本地演示，非生产**）；启动时执行 [`init.sql`](deploy/postgres/init.sql) |
| Redis 7 | `localhost:6379` | 扣款现场 |
| Kafka | 宿主机 **9093**（容器内 9092） | Topic 规划见 v3.1；账本 `balance_events` |
| Jaeger UI | `http://localhost:16686` | OTLP gRPC `:4317` |

一期表：`users`、`refresh_tokens`、`accounts`、`packages`（体验包 / 月卡 / 年卡）、`transactions`、`user_intimacy`、`water_mark`。不含 gift / ab / 独立商城 / 三层对账流水（二期再补）。

停止：

```bash
make down
# 等价：docker compose -f deploy/docker-compose.yaml down
```

## 数据库文件放哪

两类文件职责不同，不要混：

| 你要做的事 | 放哪 |
|------------|------|
| 进程连 Postgres / Redis / Kafka（host、端口、密码） | [`configs/config.yaml`](configs/config.yaml) |
| 用 Docker 拉起 Postgres / Redis / Kafka / Jaeger | [`deploy/docker-compose.yaml`](deploy/docker-compose.yaml)（即 golang-standards 的 `deployments/`） |
| 一期库表 DDL（空数据卷首次执行） | [`deploy/postgres/init.sql`](deploy/postgres/init.sql) |

Python 仓的 `xncagent/config/*.yaml` 对应本仓 `configs/`。compose 只放 Go 仓，不要复制到 Python 仓。

## 项目结构

只列当前真实存在的路径。独立 `cmd/user` 等拆进程时再加。

```
XNCAgent-go/
├── cmd/gateway/main.go          # 一期唯一进程，Gin :8199
├── internal/
│   ├── config/config.go         # 读 configs/config.yaml
│   ├── database/postgres.go     # GORM + 连接池
│   ├── middleware/jwt.go        # 鉴权；白名单 /health、register、login
│   └── user/                    # 注册 / 登录 / me（与 Gateway 同进程）
├── pkg/response/response.go     # 统一 JSON 信封
├── configs/config.yaml          # 进程配置：server / database / redis / kafka / jwt
├── deploy/
│   ├── docker-compose.yaml      # 启动基础设施
│   └── postgres/init.sql        # 一期 7 张表
├── docs/                        # 架构与落地计划
├── Makefile                     # make up / down / run
├── go.mod                       # module github.com/JerryDtj/XNCAgent-go
└── README.md
```

## 文档

| 文档 | 说明 |
|------|------|
| [docs/README.md](docs/README.md) | 文档索引 |
| [Go 微服务架构 v3.1](docs/计划/XNCAgent_Go微服务架构设计文档-v3.1.md) | Go 层权威设计 |
| [一期 / 二期落地清单](docs/计划/XNCAgent_面试冲刺计划清单.md) | 分期与验收 |
| [架构演进](docs/架构/架构演进.md) | Python Agent 核心链路（Query → RAG → LLM） |
| [商业化与推荐计划](docs/架构/AI_Agent_商业化与推荐系统项目计划.md) | 产品侧草案，冲突处以 v3.1 为准 |

## 一期 vs 二期

**一期（必须可回归）**

1. Gateway + User + Commercial（worker 可先同进程）
2. Redis Lua 预扣结算 + 幂等键；Kafka `balance_events`
3. 迎新 + 充值赠送挂在充值 preview
4. 最小 OTel：一次对话一条 Gateway → Agent trace
5. compose 拉起 Postgres / Redis / Kafka + 后续业务进程

**二期（一期绿灯后）**：Promotion 独立进程、Recommend + Profile、Risk / AB、三层对账自动化、K8s HPA。

## 远程仓库

私有仓已存在：[JerryDtj/XNCAgent-go](https://github.com/JerryDtj/XNCAgent-go)。后续提交后执行：

```bash
git push origin main
```

请勿提交 `.env` 或生产密钥。compose 里的 `xnc123` 只用于本地演示。
