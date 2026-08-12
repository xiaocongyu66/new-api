# Kubernetes 部署（master / worker 分离）

把 new-api 部署为集群内多副本，让模型连接和上下行带宽均摊到所有节点，不依赖单一“主站”。

本目录只包含清单骨架与部署入口，**不含任何真实凭证**。凭证在 GitHub Actions Secrets 中配置，由 `.github/workflows/deploy.yml` 注入集群。

## 角色划分

| 组件 | 副本 | NODE_TYPE | 职责 |
|---|---|---|---|
| `new-api-master` | 1 | 不设置（默认 master） | 后台定时任务：异步任务轮询、渠道模型同步、配额重置、凭证刷新 |
| `new-api-worker` | N（可伸缩） | `slave` | 承担全部外部 relay 请求，每个 Pod 用所在节点出口连上游 |

`k8s/service.yaml` 的 selector 只匹配 `role: worker`，因此 master 不会收到外部请求。

依据：`common/init.go`（`IsMasterNode = os.Getenv("NODE_TYPE") != "slave"`）与 `service/system_task.go`（Runner 仅在 master 启动）。

## 运行约束

- **数据库必须是 PostgreSQL 或 MySQL**。SQLite 文件无法跨 Pod 共享，多副本不可用。
- **所有 Pod 共享同一个 PostgreSQL 和同一个 Redis**。new-api 只识别单个 Redis 端点，不解析 Cluster/Sentinel 列表。
- **`SESSION_SECRET` 必须在所有 Pod 一致**，否则会话在副本间失效。
- **共享 Redis 时 `CRYPTO_SECRET` 必须一致**，否则缓存键摘要不同、无法复用。
- **`NODE_NAME` 每个 Pod 唯一**：清单中用 `fieldRef: metadata.name` 自动注入 Pod 名称。
- **本地磁盘缓存不跨 Pod 共享**（`common/disk_cache.go` 写入容器内目录），多副本下同一文件可能被不同 Pod 各自缓存。缓存策略见 issue #75。

## 需要配置的 GitHub Actions Secrets

在仓库 `Settings → Secrets and variables → Actions` 添加：

| Secret 名称 | 用途 |
|---|---|
| `KUBE_CONFIG_B64` | base64 编码的 kubeconfig，runner 访问集群 |
| `SQL_DSN` | PostgreSQL 连接串，例如指向集群内 `postgres:5432` |
| `REDIS_CONN_STRING` | Redis 连接串，例如指向集群内 `redis:6379` |
| `SESSION_SECRET` | 会话签名密钥 |
| `CRYPTO_SECRET` | 缓存键 HMAC 密钥 |
| `POSTGRES_DB` | 自建 PG 数据库名 |
| `POSTGRES_USER` | 自建 PG 用户名 |
| `POSTGRES_PASSWORD` | 自建 PG 密码 |
| `REDIS_PASSWORD` | 自建 Redis 密码 |

Secrets 的值不会出现在仓库、清单或 Actions 日志中（日志自动脱敏）。

生成 base64 kubeconfig：

```bash
base64 -w0 < ~/.kube/config
```

## 部署方式

### 方式一：通过 publish workflow（推荐）

在 GitHub 仓库 Actions 页面手动触发 `Deploy to Kubernetes`，可指定 worker 副本数和镜像 tag。workflow 会：

1. 从 Secrets 生成/更新集群内 `new-api-secrets`
2. apply PostgreSQL 与 Redis，等待就绪
3. apply master 与 worker，按输入调整副本数
4. 通过 `kubectl set image` 把镜像切换到指定 tag（如 `v0.11.0`）
5. apply Service 与 Ingress
6. 输出 Pod 与 Service 状态

**自动触发**：打 tag 推送到 GitHub 时，`Publish Docker image (Multi-arch)` workflow 会自动构建镜像并推送到 GHCR，成功后自动触发本 workflow 部署到集群。

### 方式二：手动 apply

先自行创建 Secret（值由你填写，不要写入文件）：

```bash
kubectl create secret generic new-api-secrets \
  --from-literal=sql-dsn='<PG 连接串>' \
  --from-literal=redis-conn='<Redis 连接串>' \
  --from-literal=session-secret='<会话密钥>' \
  --from-literal=crypto-secret='<缓存密钥>' \
  --from-literal=postgres-db='<数据库名>' \
  --from-literal=postgres-user='<用户名>' \
  --from-literal=postgres-password='<密码>' \
  --from-literal=redis-password='<Redis 密码>'
```

再按顺序 apply：

```bash
kubectl apply -f k8s/postgres.yaml
kubectl apply -f k8s/redis.yaml
kubectl apply -f k8s/new-api-master.yaml
kubectl apply -f k8s/new-api-worker.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/ingress.yaml
```

## 监控（Prometheus + Grafana，采集 Karmada 指标）

`k8s/monitoring/` 部署一个单实例 Prometheus + Grafana，用于采集 Karmada 控制面（apiserver / controller-manager / scheduler / scheduler-estimator / agent / webhook / descheduler）与宿主集群指标，并预导入 Karmada 官方 5 个 Grafana Dashboard。二者固定在 control-plane 节点，仅暴露集群内 ClusterIP，**不会**暴露到公网，非高可用部署。

文件：

| 文件 | 内容 |
|---|---|
| `k8s/monitoring/prometheus.yaml` | 宿主集群的 Namespace、RBAC、scrape 配置、Prometheus StatefulSet、Service |
| `k8s/monitoring/karmada-apiserver-rbac.yaml` | Karmada API-server 集群的监控 ServiceAccount、`/metrics` 最小 RBAC 和 token Secret |
| `k8s/monitoring/karmada-rules.yaml` | Karmada 官方 recording + alerting 规则（PromQL 与指南一致） |
| `k8s/monitoring/grafana.yaml` | Grafana Deployment + provisioning（数据源 + Dashboard provider）+ ConfigMaps |
| `k8s/monitoring/dashboards-configmap.yaml` | 5 个官方 Dashboard 的 ConfigMap（由 `dashboards/*.json` 生成） |
| `k8s/monitoring/dashboards/*.json` | 官方 Dashboard JSON 源文件（下载自 karmada.io） |

### 前提：Grafana Secret 与 Karmada token

清单不含任何明文凭证。先在宿主集群创建 Grafana 管理员 Secret：

```bash
kubectl -n monitoring create secret generic grafana-admin \
  --from-literal=admin-user=admin \
  --from-literal=admin-password='<强口令>'
```

然后在 **Karmada API-server context** 创建最小权限的抓取身份，等 token Secret 被填充后将该 token 复制到宿主集群：

```bash
kubectl --context karmada-apiserver apply -f k8s/monitoring/karmada-apiserver-rbac.yaml

KARMADA_PROMETHEUS_TOKEN="$(kubectl --context karmada-apiserver -n monitoring \
  get secret karmada-prometheus-token -o jsonpath='{.data.token}' | base64 --decode)"
test -n "$KARMADA_PROMETHEUS_TOKEN"

kubectl -n monitoring create secret generic karmada-prometheus-token \
  --from-literal=token="$KARMADA_PROMETHEUS_TOKEN"
```

`karmada-apiserver` 是环境中该 context 的名称；如果不同，替换为实际 kubeconfig context。

### 部署

```bash
kubectl apply -f k8s/monitoring/prometheus.yaml
kubectl apply -f k8s/monitoring/karmada-rules.yaml
kubectl apply -f k8s/monitoring/grafana.yaml
kubectl apply -f k8s/monitoring/dashboards-configmap.yaml
```

### 访问

```bash
# Prometheus
kubectl -n monitoring port-forward svc/prometheus 9090:9090
# Grafana（admin 口令来自 grafana-admin Secret）
kubectl -n monitoring port-forward svc/grafana 3000:3000
```

Grafana 启动后会自动出现 Prometheus 数据源和 `Karmada` 文件夹下的 5 个官方 Dashboard（API Server Insights / Controller Manager Insights / Member Cluster Insights / Scheduler Insights / Resource Propagation Insights）。

获取本集群 karmada-apiserver 的指标前，请确认 `karmada-prometheus-token` Secret 已注入真实 token；该 job 抓不到会导致该组件 `up=0`，其余组件不受影响。

## 入口层（自建集群无云负载均衡器）

`k8s/ingress.yaml` 需要集群已安装 Nginx Ingress Controller：

```bash
helm upgrade --install ingress-nginx ingress-nginx \
  --repo https://kubernetes.github.io/ingress-nginx \
  --namespace ingress-nginx --create-namespace
```

自建服务器没有云 LB 时，二选一暴露 Ingress Controller：

- **MetalLB**：在集群内提供 `type: LoadBalancer` 能力，分配可漂移的虚拟 IP。
- **NodePort + DNS**：Ingress Controller 用 NodePort，域名解析到多台节点 IP 做粗分流。

流式与 WebSocket 已在 Ingress annotation 中处理：关闭 `proxy-buffering` 与 `proxy-request-buffering`，读写超时设为 3600 秒。

部署前把 `k8s/ingress.yaml` 中的占位域名 `api.example.com` 替换为你的域名。

## 验证

```bash
# master 1 副本、worker N 副本均 Running，且分布在不同节点
kubectl get pods -l app=new-api -o wide

# Service 只挂 worker Pod 的 Endpoint
kubectl get endpoints new-api

# 各实例上报状态（应能看到所有 Pod 名称）
kubectl exec deploy/new-api-master -- wget -qO- http://localhost:3000/api/status
```

扩缩容：

```bash
kubectl scale deployment new-api-worker --replicas=5
```

故障自愈：

```bash
kubectl delete pod -l app=new-api,role=worker --field-selector status.phase=Running --wait=false
kubectl get pods -l role=worker -w
```

## 后续工作

完整的验证 runbook 与故障演练见 issue #76；缓存一致性策略见 issue #75；入口层细化见 issue #74。
