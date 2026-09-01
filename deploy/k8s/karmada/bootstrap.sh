#!/usr/bin/env bash
# Karmada 控制面 + Dashboard 一键部署（幂等，可重复执行）
# 用法: bash bootstrap.sh [control-plane-IP]
#   IP 缺省时从本地 ~/.kube/karmada/karmada-apiserver.config 解析
# 前置: 本机 ~/.ssh/k3s_deploy 私钥可免密登录 control-plane root
# 产出: karmada 控制面(156) + member join + dashboard, 最后打印 token 和隧道命令
set -euo pipefail

KKEY=~/.ssh/k3s_deploy
LOCAL_KCFG=~/.kube/karmada/karmada-apiserver.config

CP_IP="${1:-}"
if [[ -z "$CP_IP" ]]; then
  CP_IP=$(grep -oE 'https://[0-9.]+:32443' "$LOCAL_KCFG" 2>/dev/null | head -1 | sed -E 's|https://||;s|:32443||' || true)
fi
[[ -z "$CP_IP" ]] && { echo "ERROR: 未指定 control-plane IP 且无法从 $LOCAL_KCFG 解析"; exit 1; }
echo ">>> control-plane: $CP_IP"

# ---------- 0. SSH 连通性 ----------
ssh -i "$KKEY" -o BatchMode=yes -o ConnectTimeout=10 "root@$CP_IP" 'echo ssh-ok' >/dev/null

# ---------- 1. 生成远程执行脚本（全部集群操作）----------
cat > /tmp/karmada-remote.sh <<'REMOTE'
set -euo pipefail
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
KK=/etc/karmada/karmada-apiserver.config
NODE=$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')
[ "$(kubectl get nodes -o jsonpath='{.items[0].metadata.labels.node-role\.kubernetes\.io/control-plane}')" != "" ] || NODE=$(kubectl get nodes -o jsonpath='{.items[?(@.metadata.labels.node-role\.kubernetes\.io/control-plane)].metadata.name}')
CP_IP=$(kubectl get nodes -o jsonpath='{.items[?(@.metadata.labels.node-role\.kubernetes\.io/control-plane)].status.addresses[?(@.type=="InternalIP")].address}' | head -1)
echo "== node=$NODE cp_ip=$CP_IP =="
mkdir -p /etc/karmada /var/lib/karmada-etcd

# ---------- 清理旧部署（幂等）----------
force_del_ns() {
  local ns=$1
  kubectl get ns "$ns" >/dev/null 2>&1 || return 0
  for p in $(kubectl get pods -n "$ns" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null); do
    kubectl delete pod -n "$ns" "$p" --force --grace-period=0 >/dev/null 2>&1 || true
  done
  kubectl delete ns "$ns" --timeout=10s >/dev/null 2>&1 || true
  if kubectl get ns "$ns" -o json >/dev/null 2>&1; then
    kubectl get ns "$ns" -o json | python3 -c "
import json,sys,subprocess
d=json.load(sys.stdin); d['spec']['finalizers']=[]
subprocess.run(['kubectl','replace','--raw','/api/v1/namespaces/$ns/finalize','-f','-'],input=json.dumps(d).encode())" || true
  fi
}
echo "== 清理旧 karmada =="
force_del_ns karmada-system
force_del_ns karmada-cluster
kubectl delete svc karmada-apiserver -n karmada-system --ignore-not-found >/dev/null 2>&1 || true
rm -rf /var/lib/karmada-etcd/* /etc/karmada/*

# ---------- 2. karmadactl init（容忍超时，失败重试一次）----------
run_init() {
  kubectl delete svc karmada-apiserver -n karmada-system --ignore-not-found >/dev/null 2>&1 || true
  /usr/local/bin/karmadactl init \
    --kubeconfig /etc/rancher/k3s/k3s.yaml \
    --karmada-apiserver-advertise-address "$CP_IP" \
    --etcd-storage-mode hostPath \
    --etcd-data /var/lib/karmada-etcd \
    --etcd-node-selector-labels "kubernetes.io/hostname=$NODE" \
    --namespace karmada-system \
    --wait-component-ready-timeout 120 >/var/log/karmada-init.log 2>&1 || true
}
echo "== karmadactl init (pass 1) =="
run_init
kubectl -n karmada-system patch deploy karmada-apiserver --type=json \
  -p '[{"op":"add","path":"/spec/template/spec/nodeSelector","value":{"kubernetes.io/hostname":"'"$NODE"'"}}]' >/dev/null 2>&1 || true
# 证书代际: etcd 必须重启加载最新证书
kubectl -n karmada-system delete pod etcd-0 >/dev/null 2>&1 || true
kubectl -n karmada-system rollout status deploy karmada-apiserver --timeout=300s
# aggregated-apiserver: hostNetwork + ClusterFirstWithHostNet + 0.0.0.0:443（karmada 平面 ClusterIP 不可路由）
kubectl -n karmada-system patch deploy karmada-aggregated-apiserver --type=json -p '[{"op":"add","path":"/spec/template/spec/hostNetwork","value":true},{"op":"add","path":"/spec/template/spec/dnsPolicy","value":"ClusterFirstWithHostNet"},{"op":"replace","path":"/spec/template/spec/containers/0/command/22","value":"--bind-address=0.0.0.0"}]' >/dev/null 2>&1 || true
kubectl -n karmada-system patch deploy karmada-aggregated-apiserver --type=json -p '[{"op":"add","path":"/spec/template/spec/containers/0/command/-","value":"--secure-port=443"}]' >/dev/null 2>&1 || true

# NodePort svc: 若清理中删过则重建。严禁二次 init——会重新生成证书, 造成 kubeconfig/etcd 代际错乱
kubectl -n karmada-system get svc karmada-apiserver >/dev/null 2>&1 || kubectl -n karmada-system expose deploy karmada-apiserver --port=5443 --target-port=5443 --type=NodePort --name=karmada-apiserver >/dev/null
kubectl -n karmada-system patch svc karmada-apiserver -p '{"spec":{"ports":[{"name":"karmada-apiserver","port":5443,"targetPort":5443,"nodePort":32443}]}}' >/dev/null

# ---------- 3. CRDs ----------
echo "== CRDs =="
kubectl --kubeconfig "$KK" apply --validate=false -R -f /etc/karmada/crds/bases/ >/dev/null

# ---------- 4. controller kubeconfig secret ----------
if ! kubectl -n karmada-system get secret karmada-controller-manager-config >/dev/null 2>&1; then
  python3 - "$KK" <<'PY'
import base64, json, subprocess, sys
kk = sys.argv[1]
def get(path): return base64.b64decode(subprocess.check_output(["kubectl","--kubeconfig",kk,"-n","karmada-system","get","secret","karmada-cert","-o",f"jsonpath={{.data.{path}}}"])).decode()
kfg = {"apiVersion":"v1","kind":"Config",
 "clusters":[{"name":"karmada","cluster":{"server":"https://karmada-apiserver.karmada-system.svc.cluster.local:5443","certificate-authority-data":base64.b64encode(get("ca.crt").encode()).decode()}}],
 "users":[{"name":"karmada-admin","user":{"client-certificate-data":base64.b64encode(get("apiserver.crt").encode()).decode(),"client-key-data":base64.b64encode(get("apiserver.key").encode()).decode()}}],
 "contexts":[{"name":"karmada","context":{"cluster":"karmada","user":"karmada-admin"}}],"current-context":"karmada"}
open("/tmp/cm-kubeconfig","w").write(json.dumps(kfg))
PY
  kubectl -n karmada-system create secret generic karmada-controller-manager-config --from-file=karmada.config=/tmp/cm-kubeconfig --dry-run=client -o yaml | kubectl apply -f - >/dev/null
fi

# ---------- 5. 控制面组件（k3s 平面直接建，绕过 karmada 平面无 deployment 控制器的死结）----------
echo "== 控制面组件 =="
PIN="kubernetes.io/hostname: $NODE"
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata: {name: karmada-aggregated-apiserver, namespace: karmada-system, labels: {app: karmada-aggregated-apiserver}}
spec:
  replicas: 1
  selector: {matchLabels: {app: karmada-aggregated-apiserver}}
  template:
    metadata: {labels: {app: karmada-aggregated-apiserver}}
    spec:
      nodeSelector: {$PIN}
      containers:
      - name: aa
        image: docker.io/karmada/karmada-aggregated-apiserver:v1.14.0
        command: ["/bin/karmada-aggregated-apiserver","--kubeconfig=/etc/karmada/kubeconfig/karmada.config","--authentication-kubeconfig=/etc/karmada/kubeconfig/karmada.config","--authorization-kubeconfig=/etc/karmada/kubeconfig/karmada.config","--etcd-servers=https://etcd-0.etcd.karmada-system.svc.cluster.local:2379","--etcd-cafile=/etc/karmada/pki/etcd-ca/ca.crt","--etcd-certfile=/etc/karmada/pki/etcd-ca/etcd-client.crt","--etcd-keyfile=/etc/karmada/pki/etcd-ca/etcd-client.key","--client-ca-file=/etc/karmada/pki/front-proxy-ca/front-proxy-ca.crt","--tls-cert-file=/etc/karmada/pki/karmada-cert/karmada.crt","--tls-private-key-file=/etc/karmada/pki/karmada-cert/karmada.key","--requestheader-client-ca-file=/etc/karmada/pki/front-proxy-ca/front-proxy-ca.crt","--requestheader-allowed-names=front-proxy-client","--requestheader-extra-headers-prefix=X-Remote-Extra-","--requestheader-group-headers=X-Remote-Group","--requestheader-username-headers=X-Remote-User","--bind-address=0.0.0.0","--secure-port=443"]
        livenessProbe: {httpGet: {path: /livez, port: 443, scheme: HTTPS}, initialDelaySeconds: 15, periodSeconds: 20}
        readinessProbe: {httpGet: {path: /readyz, port: 443, scheme: HTTPS}, initialDelaySeconds: 5, periodSeconds: 10}
        volumeMounts: [{name: cfg, mountPath: /etc/karmada/kubeconfig, readOnly: true},{name: cert, mountPath: /etc/karmada/pki/karmada-cert, readOnly: true},{name: ec, mountPath: /etc/karmada/pki/etcd-ca, readOnly: true},{name: fp, mountPath: /etc/karmada/pki/front-proxy-ca, readOnly: true}]
      volumes:
      - {name: cfg, secret: {secretName: karmada-controller-manager-config}}
      - {name: cert, secret: {secretName: karmada-cert}}
      - {name: ec, secret: {secretName: karmada-cert}}
      - {name: fp, secret: {secretName: karmada-cert}}
---
apiVersion: v1
kind: Service
metadata: {name: karmada-aggregated-apiserver, namespace: karmada-system}
spec: {selector: {app: karmada-aggregated-apiserver}, ports: [{port: 443, targetPort: 443}]}
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: karmada-controller-manager, namespace: karmada-system, labels: {app: karmada-controller-manager}}
spec:
  replicas: 1
  selector: {matchLabels: {app: karmada-controller-manager}}
  template:
    metadata: {labels: {app: karmada-controller-manager}}
    spec:
      nodeSelector: {$PIN}
      containers:
      - name: karmada-controller-manager
        image: docker.io/karmada/karmada-controller-manager:v1.14.0
        command: ["/bin/karmada-controller-manager","--kubeconfig=/etc/karmada/kubeconfig/karmada.config","--metrics-bind-address=0.0.0.0:8080","--health-probe-bind-address=0.0.0.0:10357","--v=4"]
        livenessProbe: {httpGet: {path: /healthz, port: 10357}, initialDelaySeconds: 15, periodSeconds: 20}
        readinessProbe: {httpGet: {path: /healthz, port: 10357}, initialDelaySeconds: 5, periodSeconds: 10}
        volumeMounts: [{name: cfg, mountPath: /etc/karmada/kubeconfig, readOnly: true}]
      volumes: [{name: cfg, secret: {secretName: karmada-controller-manager-config}}]
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: kube-controller-manager, namespace: karmada-system, labels: {app: kube-controller-manager}}
spec:
  replicas: 1
  selector: {matchLabels: {app: kube-controller-manager}}
  template:
    metadata: {labels: {app: kube-controller-manager}}
    spec:
      nodeSelector: {$PIN}
      containers:
      - name: kcm
        image: registry.k8s.io/kube-controller-manager:v1.31.3
        command: ["kube-controller-manager","--kubeconfig=/etc/karmada/kubeconfig/karmada.config","--service-account-private-key-file=/etc/karmada/pki/karmada.key","--root-ca-file=/etc/karmada/pki/ca.crt","--controllers=namespace,serviceaccount-token,garbagecollector"]
        volumeMounts: [{name: cfg, mountPath: /etc/karmada/kubeconfig, readOnly: true},{name: pki, mountPath: /etc/karmada/pki, readOnly: true}]
      volumes:
      - {name: cfg, secret: {secretName: karmada-controller-manager-config}}
      - {name: pki, secret: {secretName: karmada-cert}}
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: karmada-scheduler, namespace: karmada-system, labels: {app: karmada-scheduler}}
spec:
  replicas: 1
  selector: {matchLabels: {app: karmada-scheduler}}
  template:
    metadata: {labels: {app: karmada-scheduler}}
    spec:
      nodeSelector: {$PIN}
      containers:
      - name: sched
        image: docker.io/karmada/karmada-scheduler:v1.14.0
        command: ["/bin/karmada-scheduler","--kubeconfig=/etc/karmada/kubeconfig/karmada.config","--metrics-bind-address=0.0.0.0:8080","--health-probe-bind-address=0.0.0.0:10357"]
        livenessProbe: {httpGet: {path: /healthz, port: 10357}, initialDelaySeconds: 15, periodSeconds: 20}
        readinessProbe: {httpGet: {path: /healthz, port: 10357}, initialDelaySeconds: 5, periodSeconds: 10}
        volumeMounts: [{name: cfg, mountPath: /etc/karmada/kubeconfig, readOnly: true}]
      volumes: [{name: cfg, secret: {secretName: karmada-controller-manager-config}}]
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: karmada-webhook, namespace: karmada-system, labels: {app: karmada-webhook}}
spec:
  replicas: 1
  selector: {matchLabels: {app: karmada-webhook}}
  template:
    metadata: {labels: {app: karmada-webhook}}
    spec:
      nodeSelector: {$PIN}
      containers:
      - name: hook
        image: docker.io/karmada/karmada-webhook:v1.14.0
        command: ["/bin/karmada-webhook","--kubeconfig=/etc/karmada/kubeconfig/karmada.config","--cert-dir=/etc/karmada/pki","--metrics-bind-address=0.0.0.0:8080","--health-probe-bind-address=0.0.0.0:10357"]
        volumeMounts: [{name: cfg, mountPath: /etc/karmada/kubeconfig, readOnly: true},{name: cert, mountPath: /etc/karmada/pki, readOnly: true}]
      volumes:
      - {name: cfg, secret: {secretName: karmada-controller-manager-config}}
      - {name: cert, secret: {secretName: karmada-cert}}
EOF
for d in karmada-aggregated-apiserver karmada-controller-manager kube-controller-manager karmada-scheduler karmada-webhook; do
  kubectl -n karmada-system rollout status deploy "$d" --timeout=120s >/dev/null 2>&1 || echo "WARN: $d not ready yet (继续)"
done

# ---------- 6. ExternalName svc + APIService (cluster.karmada.io) ----------
CA_B64=$(kubectl -n karmada-system get secret karmada-cert -o jsonpath='{.data.ca\.crt}')
kubectl -n karmada-system create svc externalname karmada-aggregated-apiserver-external \
  --external-name=karmada-aggregated-apiserver.karmada-system.svc.cluster.local >/dev/null 2>&1 || true
cat <<EOF | kubectl --kubeconfig "$KK" apply -f - >/dev/null
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata: {name: v1alpha1.cluster.karmada.io}
spec:
  group: cluster.karmada.io
  version: v1alpha1
  groupPriorityMinimum: 2000
  versionPriority: 10
  service: {name: karmada-aggregated-apiserver-external, namespace: karmada-system, port: 443}
  caBundle: $CA_B64
EOF
# 上游形态：ExternalName svc 名必须恰为 karmada-aggregated-apiserver
kubectl -n karmada-system get svc karmada-aggregated-apiserver-external >/dev/null 2>&1 && {
  kubectl -n karmada-system delete svc karmada-aggregated-apiserver --ignore-not-found >/dev/null 2>&1 || true
  kubectl -n karmada-system create svc externalname karmada-aggregated-apiserver --external-name=karmada-aggregated-apiserver.karmada-system.svc.cluster.local >/dev/null 2>&1 || true
  kubectl -n karmada-system delete svc karmada-aggregated-apiserver-external --ignore-not-found >/dev/null 2>&1 || true
}
kubectl --kubeconfig "$KK" wait --for=condition=Available apiservice/v1alpha1.cluster.karmada.io --timeout=120s

# ---------- 7. karmada 平面 RBAC（dashboard/impersonator 白名单）----------
kubectl --kubeconfig "$KK" -n karmada-system create sa karmada-dashboard --dry-run=client -o yaml | kubectl --kubeconfig "$KK" apply -f - >/dev/null
kubectl --kubeconfig "$KK" -n karmada-system create sa default --dry-run=client -o yaml | kubectl --kubeconfig "$KK" apply -f - >/dev/null
cat <<EOF | kubectl --kubeconfig "$KK" apply -f - >/dev/null
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: karmada-userinfo-impersonator}
rules:
- apiGroups: [""]
  resources: [users, groups, serviceaccounts, user-info]
  verbs: [impersonate]
- apiGroups: [authentication.k8s.io]
  resources: [userextras, selfsubjectreviews]
  verbs: [impersonate, create]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: karmada-userinfo-impersonator}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: karmada-userinfo-impersonator}
subjects: [{kind: ServiceAccount, name: karmada-dashboard, namespace: karmada-system}]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: karmada-dashboard-admin}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: cluster-admin}
subjects: [{kind: ServiceAccount, name: karmada-dashboard, namespace: karmada-system}]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: karmada-system-default-admin}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: cluster-admin}
subjects: [{kind: ServiceAccount, name: default, namespace: karmada-system}]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: karmada-dashboard-cluster-proxy}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: karmada-dashboard-cluster-proxy}
subjects:
- {kind: ServiceAccount, name: karmada-dashboard, namespace: karmada-system}
- {kind: Group, name: system:serviceaccounts, apiGroup: rbac.authorization.k8s.io}
- {kind: Group, name: system:serviceaccounts:default, apiGroup: rbac.authorization.k8s.io}
- {kind: Group, name: system:serviceaccounts:karmada-system, apiGroup: rbac.authorization.k8s.io}
- {kind: Group, name: system:authenticated, apiGroup: rbac.authorization.k8s.io}
- {kind: User, name: system:admin, apiGroup: rbac.authorization.k8s.io}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: karmada-dashboard-cluster-proxy}
rules:
- apiGroups: [cluster.karmada.io]
  resources: [clusters, clusters/proxy]
  verbs: ["*"]
- apiGroups: [cluster.karmada.io]
  resources: [clusters]
  verbs: [list, get]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: karmada-default-ext-auth-reader, namespace: kube-system}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: extension-apiserver-authentication-reader}
subjects: [{kind: ServiceAccount, name: default, namespace: karmada-system}]
EOF
# member(host=self) 的 dashboard 身份提权
kubectl -n karmada-system create sa karmada-dashboard --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1 || true
kubectl create clusterrolebinding karmada-dashboard --clusterrole=cluster-admin --serviceaccount=karmada-system:karmada-dashboard --dry-run=client -o yaml | kubectl apply -f - >/dev/null

# ---------- 8. join member + apiEndpoint 修正 ----------
echo "== join k3s-prod =="
if ! kubectl --kubeconfig "$KK" get cluster k3s-prod >/dev/null 2>&1; then
  /usr/local/bin/karmadactl join k3s-prod --kubeconfig "$KK" \
    --cluster-kubeconfig /etc/rancher/k3s/k3s.yaml --cluster-context default
fi
kubectl --kubeconfig "$KK" patch cluster k3s-prod --type=merge \
  -p '{"spec":{"apiEndpoint":"https://'"$CP_IP"':6443"}}' >/dev/null

# ---------- 9. dashboard ----------
echo "== dashboard =="
kubectl -n karmada-system create sa karmada-dashboard --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1 || true
kubectl -n karmada-system create secret generic karmada-dashboard-token \
  --from-literal=token="$(kubectl --kubeconfig "$KK" -n karmada-system create token karmada-dashboard --duration=8760h)" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
cat <<'EOF' | kubectl apply -f - >/dev/null
apiVersion: v1
kind: ConfigMap
metadata: {name: karmada-dashboard-config, namespace: karmada-system}
data:
  dashboard-config.yaml: |
    path_prefix: ''
    docker_registries: []
    chart_registries: []
---
apiVersion: v1
kind: ConfigMap
metadata: {name: karmada-dashboard-configmap, namespace: karmada-system}
data:
  prod.yaml: |
    docker_registries: []
    chart_registries: []
    menu_configs:
      - {path: /overview, enable: true, sidebar_key: OVERVIEW}
      - {path: /topology, enable: true, sidebar_key: TOPOLOGY}
      - {path: /metrics, enable: true, sidebar_key: METRICS}
      - path: /multicloud-resource-manage
        enable: true
        sidebar_key: MULTICLOUD-RESOURCE-MANAGE
        children: [{path: namespace, enable: true, sidebar_key: NAMESPACE},{path: workload, enable: true, sidebar_key: WORKLOAD},{path: service, enable: true, sidebar_key: SERVICE},{path: config, enable: true, sidebar_key: CONFIG}]
      - path: /multicloud-policy-manage
        enable: true
        sidebar_key: MULTICLOUD-POLICY-MANAGE
        children: [{path: propagation-policy, enable: true, sidebar_key: PROPAGATION-POLICY},{path: override-policy, enable: true, sidebar_key: OVERRIDE-POLICY}]
      - {path: /cluster-manage, enable: true, sidebar_key: CLUSTER-MANAGE}
      - path: /basic-config
        enable: true
        sidebar_key: BASIC-CONFIG
        children: [{path: karmada-config, enable: true, sidebar_key: KARMADA-CONFIG},{path: helm, enable: true, sidebar_key: HELM},{path: registry, enable: true, sidebar_key: REGISTRY},{path: oem, enable: false, sidebar_key: OEM},{path: upgrade, enable: false, sidebar_key: UPGRADE}]
EOF
cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: apps/v1
kind: Deployment
metadata: {name: karmada-dashboard-api, namespace: karmada-system, labels: {app: karmada-dashboard-api}}
spec:
  replicas: 1
  selector: {matchLabels: {app: karmada-dashboard-api}}
  template:
    metadata: {labels: {app: karmada-dashboard-api}}
    spec:
      serviceAccountName: karmada-dashboard
      nodeSelector: {$PIN}
      containers:
      - name: api
        image: karmada/karmada-dashboard-api:latest
        command: ["/bin/karmada-dashboard-api"]
        args: ["--karmada-kubeconfig=/etc/karmada/kubeconfig/karmada.config","--karmada-context=karmada-admin","--kubeconfig=/etc/karmada/kubeconfig/karmada.config","--context=karmada-admin","--insecure-bind-address=0.0.0.0"]
        ports: [{containerPort: 8000}]
        volumeMounts: [{name: cfg, mountPath: /etc/karmada/kubeconfig, readOnly: true}]
      volumes: [{name: cfg, secret: {secretName: karmada-controller-manager-config}}]
---
apiVersion: v1
kind: Service
metadata: {name: karmada-dashboard-api, namespace: karmada-system}
spec: {selector: {app: karmada-dashboard-api}, ports: [{port: 8000, targetPort: 8000}]}
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: karmada-dashboard-web, namespace: karmada-system, labels: {app: karmada-dashboard-web}}
spec:
  replicas: 1
  selector: {matchLabels: {app: karmada-dashboard-web}}
  template:
    metadata: {labels: {app: karmada-dashboard-web}}
    spec:
      nodeSelector: {$PIN}
      containers:
      - name: web
        image: karmada/karmada-dashboard-web:latest
        command: ["/bin/karmada-dashboard-web"]
        args: ["--static-dir=/static","--insecure-bind-address=0.0.0.0","--dashboard-config-path=/config/dashboard-config.yaml","--api-proxy-endpoint=http://karmada-dashboard-api.karmada-system.svc.cluster.local:8000","--enable-metrics-scraper-proxy=false"]
        ports: [{containerPort: 8000}]
        volumeMounts: [{name: cfg, mountPath: /config/dashboard-config.yaml, subPath: dashboard-config.yaml}]
      volumes: [{name: cfg, configMap: {name: karmada-dashboard-config}}]
---
apiVersion: v1
kind: Service
metadata: {name: karmada-dashboard-web, namespace: karmada-system}
spec: {selector: {app: karmada-dashboard-web}, ports: [{port: 8000, targetPort: 8000}], type: ClusterIP}
EOF
kubectl -n karmada-system rollout status deploy karmada-dashboard-web --timeout=120s >/dev/null
kubectl -n karmada-system rollout status deploy karmada-dashboard-api --timeout=120s >/dev/null

# ---------- 10. 健康检查 ----------
echo "== 健康检查 =="
kubectl --kubeconfig "$KK" wait --for=condition=Ready cluster/k3s-prod --timeout=120s
LOGIN=$(kubectl -n karmada-system get secret karmada-dashboard-token -o jsonpath='{.data.token}' | base64 -d)
WIP=$(kubectl -n karmada-system get svc karmada-dashboard-web -o jsonpath='{.spec.clusterIP}')
CODE=$(curl -sS -m 10 -o /dev/null -w '%{http_code}' -X POST "http://$WIP:8000/api/v1/login" -H "Content-Type: application/json" -H "Authorization: Bearer $LOGIN" -d "{\"token\":\"$LOGIN\"}")
[[ "$CODE" == "200" ]] || { echo "login failed: $CODE"; exit 1; }
echo "login=200 OK | web ClusterIP=$WIP"
echo "$WIP" > /var/log/karmada-web-clusterip
REMOTE
scp -qi "$KKEY" /tmp/karmada-remote.sh "root@$CP_IP:/tmp/karmada-remote.sh"

# ---------- 2. 远程执行 ----------
echo ">>> 远程部署开始（约 3-6 分钟）..."
ssh -i "$KKEY" -o BatchMode=yes "root@$CP_IP" 'bash /tmp/karmada-remote.sh'

# ---------- 3. 本地收尾 ----------
mkdir -p ~/.kube/karmada
scp -qi "$KKEY" "root@$CP_IP:/etc/karmada/karmada-apiserver.config" "$LOCAL_KCFG"
chmod 600 "$LOCAL_KCFG"
TOKEN=$(ssh -i "$KKEY" -o BatchMode=yes "root@$CP_IP" \
  'kubectl --kubeconfig /etc/karmada/karmada-apiserver.config -n karmada-system create token karmada-dashboard --duration=8760h')
WIP=$(ssh -i "$KKEY" -o BatchMode=yes "root@$CP_IP" 'cat /var/log/karmada-web-clusterip')
echo "$TOKEN" > ~/.kube/karmada/dashboard.token; chmod 600 ~/.kube/karmada/dashboard.token

# ---------- 4. 本地验证（绕代理直连 32443）----------
export NO_PROXY='*' no_proxy='*'
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
CLUSTERS=$(curl -sk -m 10 -H "Authorization: Bearer $TOKEN" "https://$CP_IP:32443/apis/cluster.karmada.io/v1alpha1/clusters" | python3 -c "import json,sys; d=json.load(sys.stdin); print([c['metadata']['name'] for c in d['items']])")
[[ "$CLUSTERS" == *"k3s-prod"* ]] && echo "✓ member k3s-prod registered: $CLUSTERS" || { echo "✗ member check failed: $CLUSTERS"; exit 1; }

cat <<EOF

========================================
✅ Karmada 部署完成
  控制面:   https://$CP_IP:32443 (etcd/组件均在 $CP_IP)
  member:   k3s-prod (Ready)
  dashboard token: ~/.kube/karmada/dashboard.token
  本机管理: NO_PROXY='*' karmadactl --kubeconfig $LOCAL_KCFG get clusters

打开 Dashboard（两步）:
  1) ssh -i $KKEY -N -L 8000:$WIP:8000 root@$CP_IP
  2) 浏览器 http://127.0.0.1:8000/  → 粘贴 token:
     $(head -c 60 "$HOME/.kube/karmada/dashboard.token")...
========================================
EOF