# 凭证盲部署与 publish workflow

本文档说明 new-api 的 Kubernetes 部署如何在**仓库不持有任何明文凭证**的前提下完成：凭证只存在于 GitHub Actions Secrets 中，由 publish workflow 在部署时注入集群，生成集群内 Kubernetes Secret；manifests 只通过 `secretKeyRef` 引用 Secret 名称。

对应 issue #70。基础 workflow 与 Secret 引用骨架已由 PR #77（issue #73）交付，见 `.github/workflows/deploy.yml` 与 `deploy/k8s/README.md`。本文档做原理说明与运维深化，不重写基础 workflow。

## 1. 需要在 GitHub 配置的 Secret

在仓库 `Settings → Secrets and variables → Actions → New repository secret` 添加下表所有条目。值只填在 GitHub，不写入仓库任何文件。

| Secret 名称 | 用途 | 示例占位格式 |
|---|---|---|
| `KUBE_CONFIG_B64` | base64 编码的 kubeconfig，供 runner 访问集群 API server | `base64 -w0 < ~/.kube/config` 的输出；k3s 在 `/etc/rancher/k3s/k3s.yaml` |
| `SQL_DSN` | 主数据库连接串。PG/Redis 跑在集群内时用集群 DNS 名，不用公网 IP | `postgresql://<user>:<pass>@postgres:5432/<db>` |
| `REDIS_CONN_STRING` | Redis 端点连接串。集群内用 `redis` 这个 DNS 名 | `redis://:<pass>@redis:6379/0` |
| `SESSION_SECRET` | 会话与 Token 摘要密钥，所有 Pod 必须一致 | 高强度随机字符串 |
| `CRYPTO_SECRET` | 缓存键 HMAC 密钥，共享 Redis 时所有 Pod 一致 | 高强度随机字符串 |
| `POSTGRES_DB` | 自建 PG 数据库名 | `<db-name>` |
| `POSTGRES_USER` | 自建 PG 用户名 | `<db-user>` |
| `POSTGRES_PASSWORD` | 自建 PG 密码 | `REPLACE_ME` |
| `REDIS_PASSWORD` | 自建 Redis 密码 | `REPLACE_ME` |

生成 base64 kubeconfig：

```bash
base64 -w0 < ~/.kube/config
```

> 提示：`SESSION_SECRET` 不能填 `random_string`，程序会拒绝启动。多副本部署所有 Pod 必须使用同一个值。

## 2. 零明文原理

凭证在三个位置流转，任何一处都不落地明文到仓库：

```
GitHub Actions Secrets（加密存储）
        │  ${{ secrets.* }} 注入为环境变量，日志自动脱敏为 ***
        ▼
publish workflow（.github/workflows/deploy.yml）
        │  kubectl create secret ... --dry-run=client -o yaml | kubectl apply -f -
        ▼
集群内 Kubernetes Secret（new-api-secrets）
        │  Deployment/StatefulSet 通过 secretKeyRef 引用
        ▼
Pod 环境变量
```

三条保证：

1. **仓库零明文**：`deploy/k8s/*.yaml` 只写 `secretKeyRef.name: new-api-secrets` 与 `key: <字段名>`，从不写值。可用如下命令验证仓库无明文连接串：

   ```bash
   grep -rnE "postgres(ql)?://[^ ]*:[^ @]+@|redis://[^ ]*:[^ @]+@" deploy/k8s/ .github/
   ```

   预期无输出。

2. **日志脱敏**：GitHub Actions 对注册过的 Secret 值在日志中自动替换为 `***`，`kubectl` 命令行里的 `${{ secrets.X }}` 不会以明文出现在 run log。

3. **幂等注入**：workflow 用

   ```bash
   kubectl create secret generic new-api-secrets \
     --from-literal=sql-dsn="${{ secrets.SQL_DSN }}" \
     ... \
     --dry-run=client -o yaml | kubectl apply -f -
   ```

   `--dry-run=client -o yaml | kubectl apply` 的组合让「首次创建」和「后续更新」走同一条命令，重复运行不报 `AlreadyExists`，也不会在 shell 历史或中间文件留下明文。

## 3. Secret 轮换

轮换任一凭证时，只需在 GitHub 改对应 Secret 值，重新触发 workflow：`kubectl apply` 会更新 `new-api-secrets`。已运行的 Pod 不会自动加载新值（env 在启动时注入），需要滚动重启：

```bash
kubectl rollout restart deployment/new-api-master
kubectl rollout restart deployment/new-api-worker
```

轮换 `SESSION_SECRET` 会使所有已登录会话失效，属预期行为。

## 4. runner 部署（k3s 实操）

`deploy.yml` 固定使用 **self-hosted runner**（`runs-on: [self-hosted, k8s]`），因为自建集群的 API server 在内网，GitHub 托管 runner 无法直连。runner 是**装在你服务器上的代理程序**，主动连出去到 GitHub 拉任务——GitHub 不反向连你的服务器，因此**不需要把服务器 IP/账号/密码放进 GitHub**。

### 4.1 安装 k3s 集群

选一台配置较好的服务器做 k3s server，其余做 agent：

```bash
# server 节点（如 136.0.34.25）
curl -sfL https://get.k3s.io | sh -s - --disable traefik
# --disable traefik：入口用 Nginx Ingress Controller，避免两套入口冲突

# 记下 node token，供 agent 节点加入用
sudo cat /var/lib/rancher/k3s/server/node-token

# agent 节点（如 156.254.6.210）
curl -sfL https://get.k3s.io | K3S_URL=https://<server-ip>:6443 \
  K3S_TOKEN=<node-token> sh -
```

验证：

```bash
kubectl get nodes   # 所有节点 Ready
```

### 4.2 注册 self-hosted runner

在**能 kubectl 到集群的节点**（通常是 k3s server 节点）上注册 runner：

1. 打开仓库 `Settings → Actions → Runners → New self-hosted runner`
2. 选 Linux，按页面提示在服务器上执行下载、配置、启动
3. 注册时标签填 `k8s`（与 `deploy.yml` 的 `runs-on: [self-hosted, k8s]` 匹配）

runner 运行后，在 `Settings → Actions → Runners` 页面能看到它处于 Idle 状态。

### 4.3 生成 KUBE_CONFIG_B64

在 runner 所在节点执行：

```bash
base64 -w0 < /etc/rancher/k3s/k3s.yaml
```

把输出整段粘贴为 `KUBE_CONFIG_B64` 的值。k3s 的 kubeconfig 里 server 地址是 `https://127.0.0.1:6443`，runner 与 API server 同机时可直接使用；若 runner 与集群不同机，需把 server 地址改成集群可达的 IP 后再 base64。

### 4.4 数据层连接串用集群 DNS 名

PG/Redis 由 `deploy/k8s/postgres.yaml` / `redis.yaml` 部署在集群内时，Pod 之间通过集群 DNS 名互访，`SQL_DSN` 与 `REDIS_CONN_STRING` 必须写成：

| Secret | 值 |
|---|---|
| `SQL_DSN` | `postgresql://<POSTGRES_USER>:<POSTGRES_PASSWORD>@postgres:5432/<POSTGRES_DB>` |
| `REDIS_CONN_STRING` | `redis://:<REDIS_PASSWORD>@redis:6379/0` |

`postgres` 和 `redis` 是 StatefulSet 的集群内 DNS 名，不要填节点 IP 或公网地址。

## 5. CI/CD 链路：发布镜像后自动部署

两条 workflow 配合成全自动链路：

```
git tag v0.11.0 && git push --tags
  → docker-build.yml（GitHub 托管 runner）
      → 构建 amd64/arm64 镜像 → 推送到 ghcr.io/xiaocongyu66/new-api:v0.11.0
      → 创建多架构 manifest → 签名
      → gh workflow run deploy.yml（最后一步）
  → deploy.yml（self-hosted runner）
      → 注入 Secret → apply 数据层/应用层 → kubectl set image 切到 v0.11.0
```

`deploy.yml` 的 `image_tag` 输入用于指定部署哪个镜像 tag（默认 `latest`）。手动触发时也可直接填 tag 部署指定版本。

## 6. 安全边界

- 不要把 kubeconfig、连接串、密码提交进仓库任何文件（包括示例文件、注释、测试夹具）。
- 不要在 workflow 里 `echo` 或 `cat` 出 Secret 值用于调试；如需排查，改用 `kubectl get secret new-api-secrets -o jsonpath=...` 在集群侧本地查看。
- `KUBE_CONFIG_B64` 等价于集群管理员凭证，泄露即集群失守；应使用最小权限的 ServiceAccount kubeconfig 而非 admin kubeconfig（后续可在 #76 runbook 细化 RBAC）。

