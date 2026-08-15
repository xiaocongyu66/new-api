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

func TestGetKarmadaTopologyBuildsFilteredPropagationChain(t *testing.T) {
	setupKarmadaTest(t)
	var workSelector string
	var requestedMemberB bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/policy.karmada.io/v1alpha1/namespaces/default/propagationpolicies":
			assert.Equal(t, "50", r.URL.Query().Get("limit"))
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"deploy-policy","namespace":"default","labels":{"propagationpolicy.karmada.io/permanent-id":"policy-42"}},"spec":{"resourceSelectors":[{"apiVersion":"apps/v1","kind":"Deployment","name":"web"}]}}]}`))
		case "/apis/work.karmada.io/v1alpha2/namespaces/default/resourcebindings":
			assert.Equal(t, "50", r.URL.Query().Get("limit"))
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"deploy-binding","namespace":"default","labels":{"propagationpolicy.karmada.io/permanent-id":"policy-42","resourcebinding.karmada.io/permanent-id":"binding-42"}},"spec":{"resource":{"apiVersion":"apps/v1","kind":"Deployment","namespace":"default","name":"web"},"clusters":[{"name":"member-a"},{"name":"member-b"}]},"status":{"conditions":[{"type":"Scheduled","status":"True"}]}}]}`))
		case "/apis/work.karmada.io/v1alpha1/namespaces/karmada-es-member-a/works":
			workSelector = r.URL.Query().Get("labelSelector")
			assert.Equal(t, "50", r.URL.Query().Get("limit"))
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"web-work","namespace":"karmada-es-member-a"},"status":{"conditions":[{"type":"Applied","status":"True"}]}}]}`))
		case "/apis/cluster.karmada.io/v1alpha1/clusters/member-a/proxy/apis/apps/v1/namespaces/default/deployments/web":
			_, _ = w.Write([]byte(`{"metadata":{"name":"web","namespace":"default"},"spec":{"replicas":2,"selector":{"matchLabels":{"app":"web"}}},"status":{"replicas":2,"readyReplicas":2}}`))
		case "/apis/cluster.karmada.io/v1alpha1/clusters/member-a/proxy/api/v1/namespaces/default/pods":
			assert.Equal(t, "app=web", r.URL.Query().Get("labelSelector"))
			assert.Equal(t, "50", r.URL.Query().Get("limit"))
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"web-7d5b","namespace":"default"},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}}]}`))
		default:
			if r.URL.Path == "/apis/work.karmada.io/v1alpha1/namespaces/karmada-es-member-b/works" ||
				r.URL.Path == "/apis/cluster.karmada.io/v1alpha1/clusters/member-b/proxy/apis/apps/v1/namespaces/default/deployments/web" {
				requestedMemberB = true
			}
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	c, recorder := newResourceContext(t, http.MethodGet,
		"/api/karmada/topology?namespace=default&cluster=member-a&kind=Deployment", nil, "")
	GetKarmadaTopology(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "resourcebinding.karmada.io/permanent-id=binding-42", workSelector)
	assert.False(t, requestedMemberB, "cluster filter must prevent member-b upstream requests")

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Nodes []struct {
				ID      string `json:"id"`
				Type    string `json:"type"`
				Status  string `json:"status"`
				Cluster string `json:"cluster"`
			} `json:"nodes"`
			Edges []struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"edges"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Nodes, 5)
	require.Len(t, response.Data.Edges, 4)

	types := map[string]struct {
		ID      string
		Status  string
		Cluster string
	}{}
	for _, node := range response.Data.Nodes {
		types[node.Type] = struct {
			ID      string
			Status  string
			Cluster string
		}{node.ID, node.Status, node.Cluster}
	}
	for _, nodeType := range []string{"PropagationPolicy", "ResourceBinding", "Work", "Deployment", "Pod"} {
		node, ok := types[nodeType]
		require.True(t, ok, "topology must include %s", nodeType)
		assert.Equal(t, "healthy", node.Status)
	}
	assert.Equal(t, "member-a", types["Deployment"].Cluster)
	assert.Equal(t, "member-a", types["Pod"].Cluster)
	assertTopologyEdge(t, response.Data.Edges, types["PropagationPolicy"].ID, types["ResourceBinding"].ID)
	assertTopologyEdge(t, response.Data.Edges, types["ResourceBinding"].ID, types["Work"].ID)
	assertTopologyEdge(t, response.Data.Edges, types["Work"].ID, types["Deployment"].ID)
	assertTopologyEdge(t, response.Data.Edges, types["Deployment"].ID, types["Pod"].ID)
}

func TestGetKarmadaTopologySupportsStatefulSet(t *testing.T) {
	setupKarmadaTest(t)
	var workloadPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/policy.karmada.io/v1alpha1/namespaces/default/propagationpolicies":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"stateful-policy","namespace":"default","labels":{"propagationpolicy.karmada.io/permanent-id":"policy-42"}}}]}`))
		case "/apis/work.karmada.io/v1alpha2/namespaces/default/resourcebindings":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"stateful-binding","namespace":"default","labels":{"propagationpolicy.karmada.io/permanent-id":"policy-42","resourcebinding.karmada.io/permanent-id":"binding-42"}},"spec":{"resource":{"kind":"StatefulSet","namespace":"default","name":"web"},"clusters":[{"name":"member-a"}]},"status":{"conditions":[{"type":"Scheduled","status":"True"}]}}]}`))
		case "/apis/work.karmada.io/v1alpha1/namespaces/karmada-es-member-a/works":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"web-work","namespace":"karmada-es-member-a"},"status":{"conditions":[{"type":"Applied","status":"True"}]}}]}`))
		case "/apis/cluster.karmada.io/v1alpha1/clusters/member-a/proxy/apis/apps/v1/namespaces/default/statefulsets/web":
			workloadPath = r.URL.Path
			_, _ = w.Write([]byte(`{"metadata":{"name":"web","namespace":"default"},"spec":{"replicas":2,"selector":{"matchLabels":{"app":"web"}}},"status":{"readyReplicas":2}}`))
		case "/apis/cluster.karmada.io/v1alpha1/clusters/member-a/proxy/api/v1/namespaces/default/pods":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	c, recorder := newResourceContext(t, http.MethodGet, "/api/karmada/topology?namespace=default&cluster=member-a&kind=StatefulSet", nil, "")
	GetKarmadaTopology(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "/apis/cluster.karmada.io/v1alpha1/clusters/member-a/proxy/apis/apps/v1/namespaces/default/statefulsets/web", workloadPath)
	assert.Contains(t, recorder.Body.String(), `"type":"StatefulSet"`)
}

func TestGetKarmadaTopologyRejectsMissingNamespaceAndUnsupportedKind(t *testing.T) {
	setupKarmadaTest(t)
	for _, query := range []string{"", "namespace=default&kind=Pod"} {
		c, recorder := newResourceContext(t, http.MethodGet, "/api/karmada/topology?"+query, gin.Params{}, "")
		GetKarmadaTopology(c)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, "query %q must be rejected", query)
	}
}

func TestTopologyUsesPolicyPermanentIDAndSkipsMalformedClusters(t *testing.T) {
	policy := map[string]any{
		"metadata": map[string]any{"labels": map[string]any{"propagationpolicy.karmada.io/permanent-id": "policy-42"}},
	}
	assert.Equal(t, "policy-42", topologyPolicyID(policy))
	assert.Equal(t, []string{"member-a"}, topologyClusters(map[string]any{
		"clusters": []any{"not-a-cluster", map[string]any{"name": "member-a"}, map[string]any{}},
	}, ""))
}

func TestTopologyConditionStatusPrefersFailure(t *testing.T) {
	status := map[string]any{"conditions": []any{
		map[string]any{"type": "Scheduled", "status": "Unknown"},
		map[string]any{"type": "Dispatched", "status": "False"},
	}}
	assert.Equal(t, "failed", conditionStatus(status, "Scheduled", "Dispatched"))
}

func assertTopologyEdge(t *testing.T, edges []struct {
	From string `json:"from"`
	To   string `json:"to"`
}, from, to string) {
	t.Helper()
	for _, edge := range edges {
		if edge.From == from && edge.To == to {
			return
		}
	}
	t.Fatalf("missing topology edge %s -> %s", from, to)
}
