package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// promInstantPayload builds a Prometheus instant-query response for one series
// keyed by the cluster_name label (as karmada-controller-manager emits).
func promInstantPayload(cluster, value string) string {
	return `{"status":"success","data":{"resultType":"vector","result":[` +
		`{"metric":{"cluster_name":"` + cluster + `"},"value":[1755100000,"` + value + `"]}]}}`
}

func newPrometheusStub(t *testing.T, values map[string]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/query", r.URL.Path)
		query := r.URL.Query().Get("query")
		value, ok := values[query]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(promInstantPayload("member-a", value)))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFetchClusterMetricsMapsRecordingRules(t *testing.T) {
	prometheus := newPrometheusStub(t, map[string]string{
		metricClusterCPUUtilization:    "42.5",
		metricClusterMemoryUtilization: "63.25",
		metricClusterSyncLatencyP95:    "1.75",
	})

	metrics := fetchClusterMetrics(prometheus.URL)

	require.Contains(t, metrics, "member-a")
	require.NotNil(t, metrics["member-a"].CPUPercent)
	require.NotNil(t, metrics["member-a"].MemoryPercent)
	require.NotNil(t, metrics["member-a"].SyncP95Seconds)
	assert.InDelta(t, 42.5, *metrics["member-a"].CPUPercent, 0.001)
	assert.InDelta(t, 63.25, *metrics["member-a"].MemoryPercent, 0.001)
	assert.InDelta(t, 1.75, *metrics["member-a"].SyncP95Seconds, 0.001)
}

func TestFetchClusterMetricsWithoutPrometheusReturnsNothing(t *testing.T) {
	assert.Nil(t, fetchClusterMetrics(""))
}

func TestFetchClusterMetricsSkipsUnusableSeries(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("query") {
		case metricClusterCPUUtilization:
			// Series without the cluster_name label cannot be attributed.
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"metric":{},"value":[1,"5"]}]}}`))
		case metricClusterMemoryUtilization:
			// Non-numeric sample value.
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"metric":{"cluster_name":"member-a"},"value":[1,"NaN%"]}]}}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer upstream.Close()

	assert.Nil(t, fetchClusterMetrics(upstream.URL))
}

func TestListClustersUsesClusterScopedPathAndMergesMetrics(t *testing.T) {
	setupKarmadaTest(t)
	prometheus := newPrometheusStub(t, map[string]string{
		metricClusterCPUUtilization:    "31",
		metricClusterMemoryUtilization: "57",
		metricClusterSyncLatencyP95:    "0.5",
	})
	t.Setenv("PROMETHEUS_URL", prometheus.URL)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/apis/cluster.karmada.io/v1alpha1/clusters", r.URL.Path)
		_, _ = w.Write([]byte(`{"items":[
			{"metadata":{"name":"member-a"},
			 "spec":{"apiEndpoint":"https://member-a:6443","syncMode":"Push"},
			 "status":{"conditions":[{"type":"Ready","status":"True"}],
			           "nodeSummary":{"readyNum":3,"totalNum":4},
			           "kubernetesVersion":"v1.27.0"}},
			{"metadata":{"name":"member-b"},
			 "spec":{"apiEndpoint":"https://member-b:6443","syncMode":"Pull"},
			 "status":{"conditions":[{"type":"Ready","status":"False"}],
			           "nodeSummary":{"readyNum":0,"totalNum":2},
			           "kubernetesVersion":"v1.26.3"}}
		]}`))
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/karmada/clusters", nil)
	ListKarmadaClusters(c)

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Clusters []MemberCluster `json:"clusters"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Len(t, resp.Data.Clusters, 2)

	first := resp.Data.Clusters[0]
	assert.Equal(t, "member-a", first.Name)
	assert.Equal(t, "Ready", first.Status)
	assert.Equal(t, "https://member-a:6443", first.APIEndpoint)
	assert.Equal(t, "v1.27.0", first.Version)
	assert.Equal(t, 3, first.ReadyNodes)
	assert.Equal(t, 4, first.TotalNodes)
	require.NotNil(t, first.CPUPercent)
	assert.InDelta(t, 31, *first.CPUPercent, 0.001)
	require.NotNil(t, first.SyncP95Seconds)
	assert.InDelta(t, 0.5, *first.SyncP95Seconds, 0.001)

	second := resp.Data.Clusters[1]
	assert.Equal(t, "NotReady", second.Status)
	assert.Equal(t, 0, second.ReadyNodes)
	assert.Equal(t, 2, second.TotalNodes)
}

func TestListClustersWithoutPrometheusLeavesMetricsUnknown(t *testing.T) {
	setupKarmadaTest(t)
	t.Setenv("PROMETHEUS_URL", "")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"member-a"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`))
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/karmada/clusters", nil)
	ListKarmadaClusters(c)

	assert.Contains(t, recorder.Body.String(), `"cpu_percent":null`)
	assert.Contains(t, recorder.Body.String(), `"sync_p95_seconds":null`)
}

func TestGetKarmadaClusterReturnsDetailFromMemberProxy(t *testing.T) {
	setupKarmadaTest(t)
	t.Setenv("PROMETHEUS_URL", "")
	seen := map[string]string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Path] = r.URL.RawQuery
		switch r.URL.Path {
		case "/apis/cluster.karmada.io/v1alpha1/clusters/member-a":
			_, _ = w.Write([]byte(`{"metadata":{"name":"member-a"},
				"spec":{"apiEndpoint":"https://member-a:6443","syncMode":"Push"},
				"status":{"conditions":[{"type":"Ready","status":"True"}],
				          "nodeSummary":{"readyNum":2,"totalNum":2},
				          "kubernetesVersion":"v1.27.0"}}`))
		case "/apis/cluster.karmada.io/v1alpha1/clusters/member-a/proxy/apis/apps/v1/deployments":
			_, _ = w.Write([]byte(`{"metadata":{"continue":"next-page"},"items":[{},{},{}]}`))
		case "/apis/cluster.karmada.io/v1alpha1/clusters/member-a/proxy/api/v1/pods":
			_, _ = w.Write([]byte(`{"metadata":{},"items":[{},{},{},{},{}]}`))
		case "/apis/cluster.karmada.io/v1alpha1/clusters/member-a/proxy/api/v1/services":
			_, _ = w.Write([]byte(`{"metadata":{},"items":[{}]}`))
		case "/apis/cluster.karmada.io/v1alpha1/clusters/member-a/proxy/api/v1/nodes":
			_, _ = w.Write([]byte(`{"items":[
				{"metadata":{"name":"node-1"},"status":{"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.27.0"}}},
				{"metadata":{"name":"node-2"},"status":{"conditions":[{"type":"Ready","status":"False"}],"nodeInfo":{"kubeletVersion":"v1.27.0"}}}
			]}`))
		case "/apis/cluster.karmada.io/v1alpha1/clusters/member-a/proxy/api/v1/events":
			_, _ = w.Write([]byte(`{"items":[
				{"type":"Warning","reason":"BackOff","message":"Back-off restarting failed container",
				 "lastTimestamp":"2026-08-14T05:00:00Z","involvedObject":{"kind":"Pod","name":"api-0"}}
			]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/karmada/clusters/member-a", nil)
	c.Params = gin.Params{{Key: "name", Value: "member-a"}}
	GetKarmadaCluster(c)

	var resp struct {
		Success bool          `json:"success"`
		Message string        `json:"message"`
		Data    ClusterDetail `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success, resp.Message)

	assert.Equal(t, "member-a", resp.Data.Name)
	assert.Equal(t, "Ready", resp.Data.Status)
	assert.Equal(t, "Push", resp.Data.SyncMode)
	assert.Equal(t, 3, resp.Data.Deployments)
	assert.Equal(t, 5, resp.Data.Pods)
	assert.Equal(t, 1, resp.Data.Services)
	assert.True(t, resp.Data.Truncated, "continue token must surface as truncated counts")
	require.Len(t, resp.Data.Nodes, 2)
	assert.Equal(t, "node-1", resp.Data.Nodes[0].Name)
	assert.Equal(t, "Ready", resp.Data.Nodes[0].Status)
	assert.Equal(t, "NotReady", resp.Data.Nodes[1].Status)
	require.Len(t, resp.Data.Events, 1)
	assert.Equal(t, "Warning", resp.Data.Events[0].Type)
	assert.Equal(t, "Pod/api-0", resp.Data.Events[0].Object)
	assert.Empty(t, resp.Data.Warnings)
	assert.Equal(t, "limit=20", seen["/apis/cluster.karmada.io/v1alpha1/clusters/member-a/proxy/api/v1/events"])
}

func TestGetKarmadaClusterDetailReportsUnreachableMember(t *testing.T) {
	setupKarmadaTest(t)
	t.Setenv("PROMETHEUS_URL", "")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apis/cluster.karmada.io/v1alpha1/clusters/member-a" {
			_, _ = w.Write([]byte(`{"metadata":{"name":"member-a"},"status":{"conditions":[{"type":"Ready","status":"False"}]}}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"cluster is not reachable"}`))
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/karmada/clusters/member-a", nil)
	c.Params = gin.Params{{Key: "name", Value: "member-a"}}
	GetKarmadaCluster(c)

	var resp struct {
		Success bool          `json:"success"`
		Data    ClusterDetail `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	assert.Equal(t, "NotReady", resp.Data.Status)
	assert.Equal(t, 0, resp.Data.Pods)
	require.NotEmpty(t, resp.Data.Warnings, "unreachable member cluster must be reported, not silently zeroed")
	assert.Contains(t, resp.Data.Warnings[0], "deployments")
}
