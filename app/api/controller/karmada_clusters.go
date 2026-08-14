package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// Recording rules published by deploy/k8s/monitoring/karmada-rules.yaml. The
// panel reads the pre-aggregated rules instead of re-deriving the ratios so the
// UI and the Grafana dashboards always agree.
const (
	metricClusterCPUUtilization    = "karmada:cluster:cpu:utilization"
	metricClusterMemoryUtilization = "karmada:cluster:memory:utilization"
	metricClusterSyncLatencyP95    = "karmada:cluster_sync:latency:p95"
)

// clusterMetricLabel is the label Karmada attaches to per-member-cluster series.
const clusterMetricLabel = "member_cluster"

// clusterDetailEventLimit bounds the event list pulled from a member cluster.
const clusterDetailEventLimit = 20

// ClusterMetrics carries the Prometheus-derived numbers for one member cluster.
// Every field is a pointer because "no metric" and "zero" are different facts:
// a missing Prometheus deployment must not be rendered as 0% utilization.
type ClusterMetrics struct {
	CPUPercent     *float64 `json:"cpu_percent"`
	MemoryPercent  *float64 `json:"memory_percent"`
	SyncP95Seconds *float64 `json:"sync_p95_seconds"`
}

// ClusterNode is one node of a member cluster.
type ClusterNode struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version string `json:"version"`
}

// ClusterEvent is one recent event of a member cluster.
type ClusterEvent struct {
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Object    string `json:"object"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// ClusterDetail is the payload of GET /api/karmada/clusters/:name.
type ClusterDetail struct {
	MemberCluster
	SyncMode    string         `json:"sync_mode"`
	Deployments int            `json:"deployments"`
	Pods        int            `json:"pods"`
	Services    int            `json:"services"`
	Truncated   bool           `json:"truncated"`
	Nodes       []ClusterNode  `json:"nodes"`
	Events      []ClusterEvent `json:"events"`
	Warnings    []string       `json:"warnings,omitempty"`
}

// prometheusBaseURL returns the configured Prometheus endpoint, or "" when
// metrics are not wired up. In-cluster this is the monitoring Service.
func prometheusBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(common.GetEnvOrDefaultString("PROMETHEUS_URL", "")), "/")
}

// promInstantResponse is the subset of the Prometheus instant-query response
// that the panel consumes.
type promInstantResponse struct {
	Data struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// fetchClusterMetrics resolves the three recording rules into per-cluster
// metrics. A missing Prometheus, a failing query, an unlabeled series, or a
// non-numeric sample leaves the corresponding value unset rather than zero.
func fetchClusterMetrics(baseURL string) map[string]ClusterMetrics {
	if baseURL == "" {
		return nil
	}
	client := &http.Client{Timeout: 5 * time.Second}
	metrics := map[string]ClusterMetrics{}
	assign := map[string]func(*ClusterMetrics, float64){
		metricClusterCPUUtilization:    func(m *ClusterMetrics, v float64) { m.CPUPercent = &v },
		metricClusterMemoryUtilization: func(m *ClusterMetrics, v float64) { m.MemoryPercent = &v },
		metricClusterSyncLatencyP95:    func(m *ClusterMetrics, v float64) { m.SyncP95Seconds = &v },
	}
	for _, query := range []string{metricClusterCPUUtilization, metricClusterMemoryUtilization, metricClusterSyncLatencyP95} {
		for cluster, value := range queryPrometheusByCluster(client, baseURL, query) {
			entry := metrics[cluster]
			assign[query](&entry, value)
			metrics[cluster] = entry
		}
	}
	if len(metrics) == 0 {
		return nil
	}
	return metrics
}

// queryPrometheusByCluster runs one instant query and returns the samples keyed
// by member cluster. Errors are reported to the system log and yield no samples,
// because cluster metadata must stay available when monitoring is down.
func queryPrometheusByCluster(client *http.Client, baseURL, query string) map[string]float64 {
	endpoint := baseURL + "/api/v1/query?query=" + url.QueryEscape(query)
	resp, err := client.Get(endpoint)
	if err != nil {
		common.SysError("karmada: prometheus query failed: " + err.Error())
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		common.SysError(fmt.Sprintf("karmada: prometheus query %s returned status %d", query, resp.StatusCode))
		return nil
	}
	var parsed promInstantResponse
	if err := common.DecodeJson(resp.Body, &parsed); err != nil {
		common.SysError("karmada: decode prometheus response: " + err.Error())
		return nil
	}
	samples := map[string]float64{}
	for _, series := range parsed.Data.Result {
		cluster := series.Metric[clusterMetricLabel]
		if cluster == "" || len(series.Value) < 2 {
			continue
		}
		raw, ok := series.Value[1].(string)
		if !ok {
			continue
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			continue
		}
		samples[cluster] = value
	}
	if len(samples) == 0 {
		return nil
	}
	return samples
}

// memberProxyPath builds an aggregated-apiserver proxy path that reaches a
// member cluster's own Kubernetes API through the Karmada control plane.
func memberProxyPath(cluster, apiPath string) string {
	return fmt.Sprintf("/apis/cluster.karmada.io/v1alpha1/clusters/%s/proxy%s", url.PathEscape(cluster), apiPath)
}

// listMeta captures the parts of a Kubernetes list response the panel needs:
// the item count and whether the server paginated the result.
type listMeta struct {
	Metadata struct {
		Continue string `json:"continue"`
	} `json:"metadata"`
	Items []struct{} `json:"items"`
}

// countMemberResources returns the number of objects of one kind in a member
// cluster plus whether the count is a partial page.
func countMemberResources(client *Client, cluster, apiPath string) (int, bool, error) {
	body, err := forward(client, http.MethodGet, memberProxyPath(cluster, apiPath))
	if err != nil {
		return 0, false, err
	}
	var list listMeta
	if err := common.Unmarshal(body, &list); err != nil {
		return 0, false, fmt.Errorf("decode %s: %w", apiPath, err)
	}
	return len(list.Items), list.Metadata.Continue != "", nil
}

// nodeList is the member-cluster node shape the panel renders.
type nodeList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			Conditions []clusterCondition `json:"conditions"`
			NodeInfo   struct {
				KubeletVersion string `json:"kubeletVersion"`
			} `json:"nodeInfo"`
		} `json:"status"`
	} `json:"items"`
}

// eventList is the member-cluster event shape the panel renders.
type eventList struct {
	Items []struct {
		Type           string `json:"type"`
		Reason         string `json:"reason"`
		Message        string `json:"message"`
		LastTimestamp  string `json:"lastTimestamp"`
		InvolvedObject struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"involvedObject"`
	} `json:"items"`
}

// fetchClusterDetail enriches a member cluster with the resource counts, node
// list and recent events read through the aggregated API proxy. Failures of
// individual member-cluster queries become warnings so a NotReady cluster still
// renders its control-plane metadata instead of reporting fabricated zeros.
func fetchClusterDetail(client *Client, cluster clusterItem, metrics map[string]ClusterMetrics) ClusterDetail {
	name := cluster.Metadata.Name
	detail := ClusterDetail{
		MemberCluster: cluster.toMemberCluster(metrics[name]),
		SyncMode:      cluster.Spec.SyncMode,
	}
	counts := []struct {
		label   string
		apiPath string
		target  *int
	}{
		{"deployments", "/apis/apps/v1/deployments", &detail.Deployments},
		{"pods", "/api/v1/pods", &detail.Pods},
		{"services", "/api/v1/services", &detail.Services},
	}
	for _, entry := range counts {
		count, truncated, err := countMemberResources(client, name, entry.apiPath)
		if err != nil {
			detail.Warnings = append(detail.Warnings, fmt.Sprintf("%s unavailable: %s", entry.label, err.Error()))
			continue
		}
		*entry.target = count
		detail.Truncated = detail.Truncated || truncated
	}

	if body, err := forward(client, http.MethodGet, memberProxyPath(name, "/api/v1/nodes")); err != nil {
		detail.Warnings = append(detail.Warnings, "nodes unavailable: "+err.Error())
	} else {
		var nodes nodeList
		if err := common.Unmarshal(body, &nodes); err != nil {
			detail.Warnings = append(detail.Warnings, "nodes unavailable: "+err.Error())
		} else {
			detail.Nodes = make([]ClusterNode, 0, len(nodes.Items))
			for _, node := range nodes.Items {
				detail.Nodes = append(detail.Nodes, ClusterNode{
					Name:    node.Metadata.Name,
					Status:  readyConditionStatus(node.Status.Conditions),
					Version: node.Status.NodeInfo.KubeletVersion,
				})
			}
		}
	}

	eventsPath := memberProxyPath(name, "/api/v1/events") + "?limit=" + strconv.Itoa(clusterDetailEventLimit)
	if body, err := forward(client, http.MethodGet, eventsPath); err != nil {
		detail.Warnings = append(detail.Warnings, "events unavailable: "+err.Error())
	} else {
		var events eventList
		if err := common.Unmarshal(body, &events); err != nil {
			detail.Warnings = append(detail.Warnings, "events unavailable: "+err.Error())
		} else {
			detail.Events = make([]ClusterEvent, 0, len(events.Items))
			for _, event := range events.Items {
				detail.Events = append(detail.Events, ClusterEvent{
					Type:      event.Type,
					Reason:    event.Reason,
					Object:    strings.Trim(event.InvolvedObject.Kind+"/"+event.InvolvedObject.Name, "/"),
					Message:   event.Message,
					Timestamp: event.LastTimestamp,
				})
			}
		}
	}
	return detail
}
