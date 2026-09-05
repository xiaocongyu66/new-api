# new-api 部署指南与环境配置规范

本文档说明 `new-api` 的部署拓扑模式、生产单机运行所需的环境变量清单以及 GitHub Actions CI/CD 自动化工作流与 Secrets 规范。

---

## 1. 部署模式划分

项目中包含两种独立的部署拓扑，**彼此隔离，配置不可混用**：

| 拓扑模式 | 适用场景 | 配置文件 | 编排与控制 |
|---|---|---|---|
| **单机 Docker Compose（核心生产）** | 稳定独立的生产单机实例（如主站反代服务器） | `deploy/docker-compose.yml` | GitHub Actions `.github/workflows/deploy-server.yml` 自动更新镜像 |
| **Kubernetes / K3s 集群** | 多节点分布式测试/压测集群 | `deploy/k8s/*.yaml` | `.github/workflows/deploy.yml` / `setup-k3s.yml` |

---

## 2. 生产单机环境变量参考（`docker-compose.yml`）

生产环境部署 `new-api` 容器时，需在 `docker-compose.yml`（或其同级 `.env` 文件）中配置以下生产环境变量：

### A. 数据库与存储核心变量

| 环境变量 | 必填 | 示例格式 | 说明 |
|---|---|---|---|
| `SQL_DSN` | **是** | `postgres://user:password@127.0.0.1:5432/newapi?sslmode=disable` | 主数据库连接串。支持 PostgreSQL、MySQL、SQLite（`local` 或留空）。生产环境严禁使用默认弱密码！ |
| `REDIS_CONN_STRING` | **是** | `redis://:password@127.0.0.1:6379` | Redis 连接串，用于分布式缓存、限流和 Session 存储。 |
| `LOG_SQL_DSN` | 否 | `postgres://user:pass@127.0.0.1:5432/newapi_log` | 独立日志库连接串。不配置时，日志表默认直接存储在主数据库中。 |

### B. 认证安全与代理配置

| 环境变量 | 必填 | 推荐设置 | 说明 |
|---|---|---|---|
| `SESSION_SECRET` | **是** | 32 位以上随机强密钥 | Session 签名密钥。必须在生产部署前生成高强度随机字符串！ |
| `CRYPTO_SECRET` | **是** | 32 字节随机强密钥 | 渠道凭证与敏感字段的对称加密密钥（AES-256 需要 32 字节）。 |
| `TRUSTED_PROXIES` | **是** | `127.0.0.1`（或实际反代 IP / CIDR） | 信任反向代理列表。用于准确提取客户端真实 IP 并防止 X-Forwarded-For 伪造。 |
| `SESSION_COOKIE_SECURE` | 推荐 | `true` | 全站开启 HTTPS 时务必设为 `true`，启用 Secure Cookie 及严格的 OriginGuard 防护。 |
| `SESSION_COOKIE_TRUSTED_URL` | 推荐 | `https://your-domain.example.com` | `SESSION_COOKIE_SECURE=true` 时必填的精确站点 HTTPS 域名（不支持通配符与路径）。 |

### C. 时序日志与 TimescaleDB 增强（可选）

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `TIMESCALEDB_ENABLED` | `false` | 是否开启 `logs` 表的 TimescaleDB 超表特性。仅在日志库为 PostgreSQL 且装有扩展时生效。 |
| `TIMESCALEDB_CHUNK_INTERVAL_SECONDS` | `604800`（7天） | 超表时间分块跨度（秒）。建议按日均写入量设置（如 86400 为 1 天）。 |
| `TIMESCALEDB_COMPRESS_AFTER_DAYS` | `0`（关闭） | 超过指定天数的历史日志分块自动压缩（压缩比约 90%+）。 |
| `TIMESCALEDB_RETENTION_DAYS` | `0`（关闭） | 超过指定天数的历史分块整块物理清理（日志为计费凭证，需显式按需开启）。 |

### D. 运行时基础与性能优化

| 环境变量 | 推荐设置 | 说明 |
|---|---|---|
| `BATCH_UPDATE_ENABLED` | `true` | 是否启用配额与额度批量异步回写数据库，大幅降低高并发时数据库锁争用。 |
| `DEFAULT_USER_GROUP` | 视业务而定 | 新注册用户的默认权限分组。 |
| `NODE_NAME` | `node-1` | 实例节点名称，记录在审计日志中便于多节点排查。 |
| `PORT` | `3000` | 容器内服务监听端口（默认 3000）。 |
| `TZ` | `Asia/Shanghai` | 容器时区。 |

---

## 3. GitHub Actions Secrets 命名规范

为了避免将核心生产服务器与测试/临时集群节点混淆，CI Secrets 采用严格的前缀命名区分：

### A. 单机生产部署专属变量（`.github/workflows/deploy-server.yml`）

此组变量**专供单台稳定生产服务器使用**，绝对不要写入 `SERVERS_LIST_JSON` 中：

| Secret 变量名 | 必填 | 示例占位符 | 说明 |
|---|---|---|---|
| `PROD_SERVER_HOST` | **是** | `your-server-ip` | 生产服务器 IP 或可解析域名 |
| `PROD_SERVER_USER` | **是** | `root` | SSH 登录用户名 |
| `PROD_SERVER_SSH_KEY` | **是** | `-----BEGIN OPENSSH PRIVATE KEY...` | 专用的 ED25519 部署私钥（对应公钥部署于服务器 `~/.ssh/authorized_keys`） |
| `PROD_SERVER_SSH_PORT` | 否 | `22` | SSH 端口，不填默认使用 `22` |
| `PROD_DEPLOY_PATH` | 否 | `/opt/new-api/deploy` | 生产服务器上包含 `docker-compose.yml` 的绝对路径，默认 `/opt/new-api/deploy` |

### B. K8s / 临时测试集群变量（与单机生产完全隔离）

以下变量仅供 `deploy/k8s/` 和压测集群使用：

| Secret 变量名 | 作用范围 | 说明 |
|---|---|---|
| `SERVERS_LIST_JSON` | K3s 集群维护 / 压测 | 格式为 `[{"host":"...","user":"...","password":"..."}]`，仅用于不稳定测试节点自动安装 k3s |
| `KUBE_CONFIG_B64` | K8s 部署 | base64 编码的 kubeconfig，用于公网连接 K8s API Server |
| `SQL_DSN` / `REDIS_CONN_STRING` 等 | K8s Pod 注入 | 集群内通过 `new-api-secrets` 挂载到容器 |

---

## 4. 生产自动部署工作流机制（`.github/workflows/deploy-server.yml`）

### 触发条件
1. **自动触发**：当 `Publish Docker image (Multi-arch)` 成功构建并发布新镜像至 GHCR 后，自动下游触发。
2. **手动触发**：在 GitHub Actions 页面选择 `Deploy to Server (Docker Compose)`，输入目标 `image_tag`（如 `latest`、`insight` 或 commit SHA）执行部署。

### 自动化执行步骤与安全防线
1. **轻量只读连接**：Runner 通过内存临时加载 `PROD_SERVER_SSH_KEY`，SSH 登录目标主机。
2. **记录当前镜像**：部署前捕获正在运行的容器镜像 Tag（例如 `ghcr.io/...:prev`），作为回滚基准。
3. **拉取新镜像**：先执行 `docker pull`，确保镜像完整拉取成功后再进行切换。
4. **单容器原子切换**：执行 `docker compose up -d --no-deps new-api`，**仅重启 new-api 容器，绝不触碰数据库、Redis、Nginx/Caddy 等依赖基础设施**。
5. **健康检查轮询**：连续 15 次（每 3 秒一次，共 45 秒）探测 `http://127.0.0.1:3000/api/status` 是否返回 `HTTP 200`。
6. **自动故障回滚**：若 45 秒内未就绪，工作流自动恢复先前的镜像 Tag 并重新拉起旧版本，向 GitHub 汇报失败，防止生产环境陷入不可用状态。
