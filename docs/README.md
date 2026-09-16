# XNCAgent-go 文档

本目录从 Python 仓 [XNCAgent](https://github.com/JerryDtj/XNCAgent) 的 `doc/计划`、`doc/架构` 迁入，供 Go 微服务层对照实现。人设与 Agent 运行说明仍以 Python 仓根 README 为准。

**未迁入**：`doc/私人/`、`doc/话术/`（个人材料与场景语料，不属于本仓）。

## 计划

| 文档 | 角色 |
|------|------|
| [XNCAgent_Go微服务架构设计文档-v3.1.md](./计划/XNCAgent_Go微服务架构设计文档-v3.1.md) | **Go 层权威设计**。网关、用户、会员官职、商业化（Redis 扣款 + Kafka 账本 + DB 投影）、营销、推荐、AB、画像、风控、与 Python Agent 的 SSE 契约、ADR。 |
| [XNCAgent_面试冲刺计划清单.md](./计划/XNCAgent_面试冲刺计划清单.md) | 一期 / 二期落地清单。一期：Gateway + User + Commercial；compose 与 `deploy/postgres/init.sql` 以本仓为准。 |

## 架构

| 文档 | 角色 |
|------|------|
| [架构演进.md](./架构/架构演进.md) | **Python Agent 核心链路**（Query → 安全 → 意图 → RAG → LLM），不是 Go 微服务全景。网关与账务以 v3.1 为准。 |
| [AI_Agent_商业化与推荐系统项目计划.md](./架构/AI_Agent_商业化与推荐系统项目计划.md) | 产品 / 商业化草案（积分计价、套餐锚点、画像与推荐思路）。 |
| [AI调用流程.drawio](./架构/AI调用流程.drawio) / [v2](./架构/AI调用流程v2.drawio) | Agent 调用流程图，用 [diagrams.net](https://app.diagrams.net/) 打开。 |

## 阅读时注意

- **冲突以 v3.1 为准**。商业化计划里的独立商城、更多推荐触发（聊完 / 沉默 / 行为尖峰）、Java/SpEL 示例已被 v3.1 取代：无商城、活动入口合并进充值 preview、推荐仅登录与余额不足、规则用 YAML。
- 一期表结构以 [`deploy/postgres/init.sql`](../deploy/postgres/init.sql) 为准，不含 gift / ab / 画像 / 独立商城。
- 本仓代码仍是骨架时，文档描述的是 **目标架构**，不要当成当前进程已经跑通。
