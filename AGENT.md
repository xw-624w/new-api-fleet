# new-api-fleet · 项目上下文（给 AI 智能体读）

> 本文档是自包含的项目说明。任何具备代码阅读能力的智能体，读完这一份就能理解
> 这个仓库是什么、改了什么、怎么跑起来、怎么继续开发。

---

## 1. 这是什么

这是 [New API](https://github.com/QuantumNous/new-api) `v1.0.0-rc.49` 的一个 fork，
在它之上增加了一套**算力（GPU）监控与运维**功能，内部称「算力服务 / fleet」。

- 上游：AI 网关 / 模型分发平台（Go + Gin + GORM + PostgreSQL，前端 React + rsbuild）
- 本仓库：上游 rc.49 + 自研算力服务，**不改动上游的业务逻辑**
- 分支约定：
  - `main`  = 上游 rc.49 原版（干净基线，不要在这里提交）
  - `fleet` = rc.49 + 算力服务（所有改动都在这里）

## 2. 解决什么问题

企业内部有多台 GPU 服务器（NVIDIA + Intel Arc 混部），上面的模型服务（vLLM、ollama、
rerank、embedding 等）各自为政，管理者回答不了三个问题：

1. 一共多少卡、现在几张在干活？
2. 哪个业务占了多少算力、部署在哪台机器？
3. 哪些卡买了但没人用？

算力服务就是回答这三个问题的：采集端把每台机器的硬件实况推到 new-api，
看板把「卡 / 应用 / 机器」三个维度呈现出来，并支持人工维护台账（机房、负责人、序列号）。

## 3. 架构

```
GPU 机器                         new-api 服务器                 浏览器
┌─────────────┐    HTTP POST    ┌──────────────────┐   GET   ┌────────┐
│ fleet-agent │ ──────────────▶ │ /api/fleet/ingest│ ──────▶ │ 看板   │
│ (每台一个)   │  Bearer flt_xxx │  ↓ 入库           │         │/dashboard
└─────────────┘                 │ PostgreSQL        │         │/servers│
       ▲                        └──────────────────┘         └────────┘
       │ 自更新（拉新二进制）
       └──────────────── /api/fleet/agent/bin
```

- 采集端**主动推送**，服务器不反向连接 GPU 机器（无 SSH 依赖）
- 每台机器一个独立 token（`flt_...`），库里只存哈希
- 机器身份用 **machine_uuid**（`/sys/class/dmi/id/product_uuid`），换 IP / 改主机名不会串台账

## 4. 代码结构

### 后端（Go）

| 路径 | 作用 |
|:---|:---|
| `model/fleet.go` | 10 张表的模型定义 + 上报入库 + 汇总/时序/排行/台账 |
| `controller/fleet.go` | 上报、汇总、主机、时序、应用、台账接口 |
| `controller/fleet_installer.go` | 采集端安装脚本与二进制下发 |
| `controller/fleet_alias.go` | 应用别名（把容器名映射成业务名） |
| `fleet/cmd/fleet-agent/` | 采集端源码（独立二进制，见第 6 节） |
| `router/api-router.go` | 路由注册（搜 `fleetRoute`） |
| `model/main.go` | AutoMigrate 列表（搜 `算力服务`） |

### 前端（React + TS）

| 路径 | 作用 |
|:---|:---|
| `web/src/features/dashboard/components/servers/server-fleet.tsx` | 主看板（统计卡、机器表、单机详情、排行、浪费） |
| `web/src/features/dashboard/components/servers/trend-chart.tsx` | 算力趋势图（Top6+其他、按应用/按机器、日聚合） |
| `web/src/features/dashboard/components/servers/alias-dialog.tsx` | 应用标记（别名）对话框 |
| `web/src/features/dashboard/components/servers/meta-dialog.tsx` | 台账编辑 |
| `web/src/features/dashboard/components/servers/history-dialog.tsx` | 台账变更历史与回滚 |
| `web/src/features/dashboard/components/servers/api.ts` | 接口封装 |
| `web/src/features/dashboard/components/servers/types.ts` | 类型定义 |

## 5. 数据模型（10 张表）

| 表 | 内容 | 谁写 |
|:---|:---|:---|
| `fleet_hosts` | 机器（hostname、ip、uuid、状态） | 采集端 |
| `fleet_gpus` | 加速卡当前值（型号、显存、利用率、温度） | 采集端 |
| `fleet_apps` | 应用当前值（名称、占用卡数、利用率、显存） | 采集端 |
| `fleet_host_samples` / `fleet_gpu_samples` / `fleet_app_samples` | 时序样本（默认保留 30 天） | 采集端 |
| `fleet_agents` | 采集端凭据（hostname + token 哈希） | 管理员 |
| `fleet_host_meta` | **人工台账**（机房、负责人、序列号、BMC 等） | 人 |
| `fleet_meta_history` | 台账变更历史（diff + 快照，可回滚） | 系统 |
| `fleet_app_aliases` | 应用别名（service / image / name → 业务名） | 人 |

**原则**：机器写的字段人不能覆盖，人写的字段机器不会覆盖。

## 6. 采集端（fleet-agent）

独立 Go 程序，编译后随镜像下发（镜像内 `/fleet-agent-bin`）。

```bash
# 本机调试
go build -o fleet-agent ./fleet/cmd/fleet-agent
./fleet-agent -server http://127.0.0.1:8951 -token flt_xxx -once -dry-run
```

主要参数：

| 参数 | 说明 |
|:---|:---|
| `-server` | new-api 地址 |
| `-token` | 该机器的 token（`flt_...`） |
| `-interval` | 采集周期，默认 30s |
| `-once` | 采集一次就退出（调试用） |
| `-dry-run` | 打印 JSON 不上报 |
| `-simulate <file>` | 模拟模式，用 JSON 造数据（无卡环境演示用） |
| `-auto-update` | 默认开启：每 6 小时检查服务端版本，不同则自替换重启 |

**应用识别链**（从准到不准）：
1. 手动别名（`fleet_app_aliases`）
2. 容器 label `fleet.name` / `fleet.model` / `fleet.owner`
3. HTTP 探测（vLLM `/v1/models`、ollama `/api/tags`）
4. 命令行参数 `--served-model-name` / `--model`
5. 环境变量 `MODEL_NAME`
6. 兜底：容器服务名 / 进程名

**归属规则**：一张卡上只有一个应用 → 利用率精确归属；多个应用共享一张卡 →
按显存占比分摊，并标记 `estimated=true`。

## 7. 接口清单

全部需要管理员登录（`AdminAuth`），除 `ingest` 和 `agent/*` 用采集端 token。

| 方法 | 路径 | 说明 |
|:---|:---|:---|
| POST | `/api/fleet/ingest` | 采集端上报 |
| GET | `/api/fleet/agent/install.sh?token=` | 生成安装脚本 |
| GET | `/api/fleet/agent/bin?token=` | 下载采集端二进制 |
| GET | `/api/fleet/agent/version?token=` | 采集端版本（自更新用） |
| GET | `/api/fleet/summary?range=1d\|7d\|29d` | 顶部统计 |
| GET | `/api/fleet/hosts` | 机器列表 |
| GET | `/api/fleet/hosts/:id` | 单机详情（卡 + 应用 + 台账） |
| GET | `/api/fleet/series?range=&group_by=app\|host` | 趋势时序 |
| GET | `/api/fleet/apps?range=&host_id=&top=` | 应用排行 |
| PUT | `/api/fleet/hosts/:id/meta` | 修改台账（PATCH 语义） |
| GET | `/api/fleet/hosts/:id/meta/history` | 台账变更历史 |
| POST | `/api/fleet/hosts/:id/meta/rollback` | 回滚台账 |
| GET/PUT/DELETE | `/api/fleet/app-aliases` | 应用别名 |

## 8. 关键计算口径

| 指标 | 算法 |
|:---|:---|
| 卡时（GPU·h） | `Σ(每次采样利用率) / 100 × 采样间隔 / 3600`，只按实际采样跨度折算 |
| 平均在用卡数 | `区间卡时 / 区间实际覆盖小时数` |
| 每张卡占用 | `应用利用率 / 占用卡数`（避免出现 121% 这种数字） |
| 趋势分桶 | 1 天 → 1 分钟；多天 → **1 天**（按本地时区切日） |
| 「今日」 | 按**服务器本地时区** 0 点起算，不是 UTC |

## 9. 部署

生产只需要两个文件：镜像 tar.gz + `upgrade.sh`。

```bash
# 服务端
bash upgrade.sh          # 备份 → 载入镜像 → 重建容器 → 核对

# 采集端（每台 GPU 机器）
TOKEN=$(sed -n 's/^FLEET_TOKEN=//p' /etc/fleet-agent/fleet-agent.env | head -1)
curl -fsSL "http://<服务器>:8951/api/fleet/agent/install.sh?token=$TOKEN" | bash
systemctl restart fleet-agent
```

从这一版起采集端会自更新，以后升级只换服务端镜像。

环境变量（可选）：

| 变量 | 默认 | 说明 |
|:---|:---|:---|
| `FLEET_SAMPLE_RETENTION_DAYS` | 30 | 原始样本保留天数 |
| `FLEET_AGENT_BIN` | `/fleet-agent-bin` | 下发用的采集端路径 |

## 10. 已知限制 / 待办

- Intel Arc 依赖 `xpu-smi` 或 sysfs 兜底；驱动异常时只能拿到部分指标
- 首次安装采集端需要手工执行一次（旧版本没有自更新代码）
- 日志 `node_name`、配额数据的模型筛选是本仓库对上游表的少量扩展
- 原始样本只保留 30 天，更长的历史需要做按小时/按天的 rollup 表（未实现）

## 11. 继续开发时怎么改

| 想加什么 | 改哪里 |
|:---|:---|
| 新的采集指标 | `fleet/cmd/fleet-agent/agent_main.go` 的 `collectLocal` + `model/fleet.go` 的表结构 |
| 新的统计口径 | `model/fleet.go` 的 `GetFleetSummary` / `ListFleetApps` |
| 新的图表 | `web/.../servers/trend-chart.tsx` + `api.ts` |
| 新的台账字段 | `model/fleet.go` 的 `FleetHostMeta` + `meta-dialog.tsx` |

改前端后要跑：`cd web && bun run build`（或 `pnpm build`）；
改后端后要跑：`go build ./...`，并确认 `go.mod` 的 Go 版本要求。
