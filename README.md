# XNCAgent-go

XNCAgent 是「小喜子」的专属 Agent 项目：一名戏精附体的赛博弄臣。本仓库是它的 **Go 微服务层**，负责高并发网关、用户体系、积分账务与商业化闭环。

对话状态机、LLM、RAG、MCP 工具在 Python 仓 [JerryDtj/XNCAgent](https://github.com/JerryDtj/XNCAgent)。两仓一起构成完整系统。

| 仓库 | 职责 |
|------|------|
| [XNCAgent](https://github.com/JerryDtj/XNCAgent) | Python Agent Core：对话、RAG、MCP、Sandbox |
| **XNCAgent-go**（本仓） | Gateway / User / Commercial；本地 compose 与一期库表 |

人设与说话规则（反差双簧、情绪雷达、安全树洞）见 Python 仓 [README](https://github.com/JerryDtj/XNCAgent/blob/main/README.md)。

## 当前状态

本仓目前是 **一期骨架 + 本地基础设施**，业务进程尚未实现：

- 已有：`cmd/` / `internal/` / `pkg/` 目录规划、`deploy/docker-compose.yaml`、`deploy/init.sql`
- 没有：`main.go`、gRPC proto、业务镜像；**还不能 `go run`**
- 一期进程目标：Gateway + User + Commercial（worker 可先同进程）；好友度 / 官职表并进 User，不拆独立 Member 进程
- 二期再拆：Promotion、Recommend、Profile、AB、Risk

完整设计见 [docs/计划](docs/计划/XNCAgent_Go微服务架构设计文档-v3.1.md)，落地节奏见 [一期 / 二期清单](docs/计划/XNCAgent_面试冲刺计划清单.md)。

## 架构概览

```
Client (HTTP/JSON)
        │
        ▼
API Gateway  Gin :8080
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
cd deploy
docker compose up -d
```

| 服务 | 地址 | 说明 |
|------|------|------|
| Postgres 15 | `localhost:5432` | 用户 `xnc`，库 `xncagent`，密码 `xnc123`（**仅本地演示，非生产**）；启动时执行 [`init.sql`](deploy/init.sql) |
| Redis 7 | `localhost:6379` | 扣款现场 |
| Kafka | 宿主机 **9093**（容器内 9092） | Topic 规划见 v3.1；账本 `balance_events` |
| Jaeger UI | `http://localhost:16686` | OTLP gRPC `:4317` |

一期表：`users`、`refresh_tokens`、`accounts`、`packages`（体验包 / 月卡 / 年卡）、`transactions`、`user_intimacy`、`water_mark`。不含 gift / ab / 独立商城 / 三层对账流水（二期再补）。

停止：

```bash
cd deploy
docker compose down
```

## 项目结构

```
XNCAgent-go/
├── cmd/
│   ├── gateway/          # API Gateway（Gin :8080）
│   ├── user/             # 用户服务
│   └── commercial/       # 商业化服务（worker 一期可同进程）
├── internal/
│   ├── middleware/       # 网关中间件
│   ├── router/           # 网关路由
│   ├── user/
│   └── commercial/
├── pkg/
│   ├── errcode/          # 统一错误码
│   ├── httpclient/       # 调 Python Agent 等
│   ├── logger/
│   └── response/         # 统一 JSON
├── api/proto/            # gRPC Protobuf（待补）
├── configs/              # 服务配置（待补）
├── deploy/
│   ├── docker-compose.yaml
│   └── init.sql
├── docs/                 # 架构与落地计划
├── tests/
├── go.mod                # module github.com/JerryDtj/XNCAgent-go
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

## 推送到 GitHub

```bash
git add .
git commit -m "feat: 初始化 XNCAgent-go 微服务骨架与文档"

# 远程已存在时
git remote add origin git@github.com:JerryDtj/XNCAgent-go.git
git push -u origin main

# 或用 GitHub CLI 新建公开仓再推送
gh repo create XNCAgent-go --public --source=. --remote=origin --push
```

请勿提交 `.env` 或生产密钥。compose 里的 `xnc123` 只用于本地演示。
