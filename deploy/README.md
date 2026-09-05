# new-api 部署指南与 CI/CD 规范

本文档说明 `new-api` 的两种部署模式、GitHub Actions CI/CD 自动化工作流以及所需的 Secrets 变量配置规范。

---

## 1. 部署模式划分

项目中包含两种独立的部署拓扑，**彼此隔离，配置不可混用**：

| 拓扑模式 | 适用场景 | 配置文件 | 编排与控制 |
|---|---|---|---|
| **单机 Docker Compose（核心生产）** | 稳定独立的生产单机实例（如 `nailao.biz`） | `deploy/docker-compose.yml` | GitHub Actions `.github/workflows/deploy-server.yml` 自动更新镜像 |
| **Kubernetes / K3s 集群** | 多节点分布式测试/压测集群 | `deploy/k8s/*.yaml` | `.github/workflows/deploy.yml` / `setup-k3s.yml` |

---

## 2. GitHub Actions Secrets 变量命名规范

为了避免将核心生产服务器与测试/临时集群节点混淆，CI Secrets 采用严格的前缀命名区分：

### A. 单机生产部署专属变量（`.github/workflows/deploy-server.yml`）

此组变量**专供单台稳定生产服务器使用**，绝对不要写入 `SERVERS_LIST_JSON` 中：

| Secret 变量名 | 必填 | 示例值 | 说明 |
|---|---|---|---|
| `PROD_SERVER_HOST` | **是** | `154.12.51.245` | 生产服务器 IP 或可解析域名 |
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

## 3. 生产自动部署工作流机制（`.github/workflows/deploy-server.yml`）

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
