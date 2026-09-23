# XNCAgent Go Gateway 功能清单

> **权威计划**：[面试冲刺计划清单](../计划/XNCAgent_面试冲刺计划清单.md)、[v3.1 架构](../计划/XNCAgent_Go微服务架构设计文档-v3.1.md)  
> **工作目录**：`XNCAgent-go` 的 `cmd/gateway`（一期 User 与 Gateway **同进程**，逻辑在 `internal/user`）  
> **ShenYu**：只借鉴设计思想，不抄 Java/WebFlux/Admin。时间点以本仓日计划为准，不以插件全家桶为准。  
> **当前状态（2026-09-15 深夜）**：`GET /health` 返回 `{"code":0,"message":"success"}`，**9.15 硬验收已过**。`pkg/response/response.go` 只有空 `Body{Code int}`，结构体 JSON 标签未写；handler 尚未拆到 `health.go`。这两项收到 **9.16 早 ≤30 分钟**，超时放弃，不挤注册登录。

---

## 一、落地节奏（按日计划，不要提前）

```
9.15 当晚  → /health 硬验收已过（code+message）。统一信封未封装
9.16 早 30 分钟 → 读 JSON 标签，写完 pkg/response，health 改走信封（可拆 health.go）
9.16 白天  → User 注册登录 + JWT 白名单（同进程，不要反代 8081）
9.17–9.19  → Python 仓：对话 / RAG / MCP（Go 网关停手）
9.20       → 只反代 Python Agent 的 SSE（User 仍同进程）
9.21       → Week A 验收，不新加网关功能
Week B     → 对话前 Prehold → SSE → 结束后 Settle（账务在 Commercial）
十一       → 全链路回归，不新开功能
Week C     → Feature Flag、metrics、OTel → 一期收口
二期       → 热更新 / 灰度 / WAF / 服务发现
```

**不要做的压缩**：不是「本周做完 5 个中间件 + yaml 路由表 + 反向代理」。那是 ShenYu divide 的缩影，会挤掉 9.16 的注册登录和 9.17–9.19 的 Python。

---

## 二、ShenYu → 我们什么时候做（对照，不驱动排期）

| ShenYu 概念 | 本质 | 我们怎么做 | 何时 |
|-------------|------|------------|------|
| `shenyu.health.paths`（`/actuator/health`、`/health_check`） | 探针路径跳过插件链 | `GET /health` 免鉴权、免限流；进程活着即 200 | **9.15** |
| Actuator health | 进程存活 | 统一 JSON，**不要**在 `/health` 里 ping DB/Redis | **9.15** |
| （K8s 习惯，v3.1 §19.2 已写） | 就绪探针 | 以后才加 `GET /ready`（Postgres/Redis）；失败 503。不是今晚 | Week C / 上 K8s 时 |
| Client heartbeat → Admin | 注册保活 | 不做（没有 ShenYu Admin） | 永不 |
| `upstreamCheck` | 定时探活下游 | 本地单实例，不做 | 二期多实例再说 |
| jwt / sign | 鉴权 | Gin JWT 中间件；白名单含 `/health` | **9.16** |
| divide 反代 | 转发给 upstream | **只**转 Python Agent；User/Commercial 一期同进程 | **9.20** |
| 插件链 order | 中间件顺序 | 跟 v3.1 §4.1，不是 ShenYu 默认顺序 | 随功能逐天加 |
| rate_limiter | 限流 | 一期单机令牌桶 | Week B 有余力 / 二期前 |
| logging / metrics / tracing | 可观测 | `gin.Default()` 先顶着；zap + Prometheus + OTel | 日志可 9.16 顺手；metrics/OTel 是 **Week C** |
| Feature Flag / Admin 开关 | 热开关 | 一个 Redis key，关闭时主链路无额外分支 | **Week C**（面试清单，不是 Week B） |
| 选择器 + 规则 + 数据同步 | 动态路由 | 一期硬编码路由；二期再热更新 | 二期-3 |
| gray / waf / discovery | 灰度、WAF、注册发现 | — | 二期-3 / 二期-4 |
| response-cache | 响应缓存 | — | 暂不做 |

---

## 三、功能清单（按我们的分期）

### 9.15 — `/health` + 统一 JSON

> 面试清单 Week A 模块：Gin、`/health`、统一 JSON、RequestID。当天硬验收只有 health JSON。

| # | 功能 | 实现要点 | 验收 |
|---|------|----------|------|
| 1 | `GET /health` | **已过**：`main.go` 里 `gin.H{"code":0,"message":"success"}`。9.16 早再改走 `response.OK`。**禁止**探 Postgres/Redis | `curl.exe` 含 `"code":0` |
| 2 | 统一响应 JSON | **未完成**，9.16 早补。`pkg/response`：`{code, message, data, request_id}`。字段名是 **`message`**，不要 `msg`。成功 `code=0` | health 与后续错误都走同一结构 |
| 3 | RequestID | 可选。无 `X-Request-ID` 则生成 UUID，写入 ctx + 响应头。没有也不挡 Check | 有则响应头与 body 同一 ID |

**信封约定**（后面接口都用，不要再手写 `gin.H{"message":...}`）：

```json
{"code": 0, "message": "ok", "data": {"status": "UP"}}
```

| code | 含义 | HTTP | 何时启用 |
|------|------|------|----------|
| 0 | 成功 | 200 | 9.15 |
| 1 | 通用失败 | 400/500 | 9.15 封装即可 |
| 40101 | 未登录 / token 无效 | 401 | 9.16 JWT |

`gin.Default()` 已带 Logger + Recovery，9.15 **不要**换成 zap，也 **不要**写 `/ready`。

### 9.16 — User + JWT（同进程）

> 天计划：注册 → 登录双 token → 无 token 访问受保护接口 401。不要 `ALTER`、不要 `down -v`、不要再建 users 表。

| # | 功能 | 实现要点 | 验收 |
|---|------|----------|------|
| 4 | 注册 / 登录 | `POST /api/v1/users/register`、`POST /api/v1/users/login`；email + bcrypt；access 2h + refresh 7d；逻辑在 `internal/user` | 天计划三条 curl |
| 5 | JWT 中间件 | 白名单：`/health`、`/api/v1/users/register`、`/api/v1/users/login`（refresh 若当天做也放行）。其余 Bearer。失败走统一 JSON 401 | 不带 token 401，带 token 200 |
| 6 | 端口配置 | 可用 Viper 读 `configs/config.yaml` 的 `port: 8199`；读失败仍默认 8199 | `go run ./cmd/gateway` 仍听 8199 |

**不要**：`routes.yaml` 把 User 反代到 `localhost:8081`。一期没有独立 User 进程。

### 9.17–9.19 — 网关停手

工作在 Python 仓（CLI 流式、RAG、树洞、MCP）。Go 仓不写反代、不写熔断、不写 CORS。

### 9.20 — 只透传 Python SSE

| # | 功能 | 实现要点 | 验收 |
|---|------|----------|------|
| 7 | 反代 Agent | `POST /api/v1/agent/chat` → 本机 Python（如 `http://127.0.0.1:<python端口>`）。`httputil.ReverseProxy`；SSE 小 `FlushInterval`、禁止整段缓冲；透传 `Authorization` | `curl -N` 看到多行 `data:` |
| 8 | SSE 超时 | 普通接口可 10s；**对话不设总超时**，仅空闲超时（如 60s 无数据才断） | 长连接不被 10s 掐断 |

CORS：本周无前端，不做。访问日志：`gin.Default()` 足够，9.21 前不必 zap。

### 9.21 — Week A 验收

聊天 + 检索 + 登录全通。缺啥修啥，**不新加**网关功能（限流、Flag、OTel 都不动）。

### Week B（9.22–9.30）— 扣款编排

Gateway **只触发** Prehold → SSE → Settle，**不算金额**（Commercial 的 Redis Lua）。客户端断连仍要 Settle：`c.Request.Context().Done()` + 结束兜底。

单机限流（`golang.org/x/time/rate`）若有余力可加在 JWT **之前**（与 v3.1 §4.1 一致：先限流再验签）。不是本周 P0。熔断/多 upstream 一期本地用不上。

### Week C（10.08–10.12）— 一期收口

| # | 功能 | 说明 |
|---|------|------|
| Feature Flag | Redis 一个开关；关闭时主链路无额外分支 | 面试清单放在 Week C |
| OTel | 一次对话一条 Gateway→Agent trace；Jaeger OTLP gRPC 4317 | Week C 重点 |
| `/metrics` | 可选；Grafana 非验收硬项 | 有余力再做 |
| `/ready` | 探 Postgres/Redis，失败 503 | 配合 compose/K8s，不要合并进 `/health` |

十一（10.01–10.07）只回归、修 bug，不新开功能。

### 二期（一期回归绿灯后）

| # | 功能 | 对应批次 |
|---|------|----------|
| 配置热更新 | Redis Pub/Sub，不发版改路由/阈值 | 二期-3 |
| 用户级限流 | Redis 滑动窗口 | 二期-3 Risk |
| 灰度 | `X-Gray` / user_id 哈希 | 二期-3 AB |
| WAF | IP/UA 黑名单 | 二期-3 Risk |
| 服务发现 | K8s Endpoints，取代写死 Python 地址 | 二期-4 |
| 响应缓存 | 套餐列表 GET | 按需 |

---

## 四、中间件顺序（v3.1 §4.1，按天往链上挂）

```
Request
  → Recovery          （gin.Default 已有，9.15）
  → RequestID         （9.15 可选）
  → AccessLog         （gin.Default 已有）
  → Rate Limit        （Week B 有余力；在 Auth 前，防刷 JWT）
  → Feature Flag      （Week C）
  → JWT Auth          （9.16；/health 等白名单 skip）
  → Handler
       同进程：/api/v1/users/* 、日后 commercial
       9.20：/api/v1/agent/chat 反代 Python
```

限流在 JWT **之前**。不要写成「先认证再限流」。

---

## 五、9.20 才需要的反代（不要提前建成路由中心）

一期只有 Python 需要跨进程。不要给 User/Commercial 配 `8081`/`8082`。

```yaml
# 示例：9.20 再加，路径可写在代码里，不必先上 yaml
agent:
  path: /api/v1/agent/chat
  upstream: http://127.0.0.1:8000   # 以当时 Python 监听为准
  sse: true
  timeout: 0s                       # 不设总超时，仅空闲超时
```

---

## 六、代码往哪放（按天长，不要一次拆完）

```
cmd/gateway/main.go           # 入口；端口读 configs/config.yaml，默认 :8199
pkg/response/json.go          # 9.15 统一 {code, message, data, request_id}
internal/user/                # 9.16 注册登录（目录已有）
internal/middleware/jwt.go    # 9.16
pkg/httpclient/ 或 internal/proxy/  # 9.20 才出现，只打 Python
```

`internal/router`、`loadbalance`、`featureflag`、`metrics` 等目录 **用到再建模**，不要 9.15 按 ShenYu 插件表一次建齐。

---

## 七、明确不做（ShenYu 有也不抄）

| ShenYu 功能 | 原因 |
|-------------|------|
| Admin 控制台 + MySQL 元数据 | 一个网关实例，yaml/代码足够 |
| WebFlux / Reactor | Go goroutine 即可 |
| Dubbo/gRPC/Sofa 协议转换 | 一期对外 HTTP；User 同进程不用协议转换 |
| spEL / Groovy 规则脚本 | 攻击面；匹配只用 path |
| zk/nacos/consul 多注册中心 | 一期写死 Python 地址 |
| Hystrix 线程池隔离 | 超时即可 |
| 集群 Redis 限流 | 二期 Risk |
| 在 `/health` 里做 upstreamCheck / 依赖大盘 | 那是 readiness 或独立探活，会误杀 liveness |

---

*整理日期：2026-09-15。依据：Week A 天计划、面试冲刺清单、v3.1 §4 / §14 / §19.2。ShenYu 官方文档仅作对照。*
