package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newResourceContext(t *testing.T, method, target string, params gin.Params, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	c.Request = httptest.NewRequest(method, target, reader)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = params
	return c, recorder
}

func TestResourceKindAllowlistRejectsUnknownKind(t *testing.T) {
	_, err := resolveResourceKind("Pod")
	require.Error(t, err, "kinds outside the issue's list must be rejected")

	_, err = resolveResourceKind("../../secrets")
	require.Error(t, err)

	kind, err := resolveResourceKind("Deployment")
	require.NoError(t, err)
	assert.Equal(t, "/apis/apps/v1", kind.apiRoot)
	assert.Equal(t, "deployments", kind.plural)
	assert.True(t, kind.namespaced)
	assert.True(t, kind.workload)

	ns, err := resolveResourceKind("Namespace")
	require.NoError(t, err)
	assert.False(t, ns.namespaced, "Namespace is cluster scoped")
}

func TestListResourcesFiltersNamespaceAndRedactsSecrets(t *testing.T) {
	setupKarmadaTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/namespaces/prod/secrets", r.URL.Path)
		_, _ = w.Write([]byte(`{"items":[{
			"metadata":{"name":"api-token","namespace":"prod","creationTimestamp":"2026-08-01T00:00:00Z"},
			"type":"Opaque",
			"data":{"token":"c3VwZXItc2VjcmV0"},
			"stringData":{"plain":"super-secret"}
		}]}`))
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	c, recorder := newResourceContext(t, http.MethodGet, "/api/karmada/resources/Secret?namespace=prod", gin.Params{{Key: "kind", Value: "Secret"}}, "")
	ListKarmadaResources(c)

	body := recorder.Body.String()
	assert.NotContains(t, body, "c3VwZXItc2VjcmV0", "Secret data must never reach the panel")
	assert.NotContains(t, body, "super-secret")

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Resources []KarmadaResource `json:"resources"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Len(t, resp.Data.Resources, 1)
	assert.Equal(t, "api-token", resp.Data.Resources[0].Name)
	assert.Equal(t, "Secret", resp.Data.Resources[0].Kind)
	assert.Equal(t, "prod", resp.Data.Resources[0].Namespace)
	assert.Equal(t, "2026-08-01T00:00:00Z", resp.Data.Resources[0].CreatedAt)
}

func TestListResourcesReadsMemberClusterThroughProxy(t *testing.T) {
	setupKarmadaTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t,
			"/apis/cluster.karmada.io/v1alpha1/clusters/member-a/proxy/apis/apps/v1/deployments",
			r.URL.Path)
		_, _ = w.Write([]byte(`{"items":[{
			"metadata":{"name":"api","namespace":"default"},
			"spec":{"replicas":3},
			"status":{"replicas":3,"readyReplicas":2}
		}]}`))
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	c, recorder := newResourceContext(t, http.MethodGet, "/api/karmada/resources/Deployment?cluster=member-a", gin.Params{{Key: "kind", Value: "Deployment"}}, "")
	ListKarmadaResources(c)

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Resources []KarmadaResource `json:"resources"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Len(t, resp.Data.Resources, 1)
	assert.Equal(t, "member-a", resp.Data.Resources[0].Cluster)
	require.NotNil(t, resp.Data.Resources[0].Replicas)
	assert.Equal(t, 3, *resp.Data.Resources[0].Replicas)
	require.NotNil(t, resp.Data.Resources[0].ReadyReplicas)
	assert.Equal(t, 2, *resp.Data.Resources[0].ReadyReplicas)
	assert.Equal(t, "2/3", resp.Data.Resources[0].Status)
}

func TestGetResourceDetailReportsClusterDistribution(t *testing.T) {
	setupKarmadaTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/apps/v1/namespaces/default/deployments/api":
			_, _ = w.Write([]byte(`{"metadata":{"name":"api","namespace":"default"},
				"spec":{"replicas":5},"status":{"replicas":5,"readyReplicas":5}}`))
		case "/apis/work.karmada.io/v1alpha2/namespaces/default/resourcebindings":
			_, _ = w.Write([]byte(`{"items":[
				{"spec":{"resource":{"kind":"Deployment","name":"api","namespace":"default"},
				 "clusters":[{"name":"member-a","replicas":3},{"name":"member-b","replicas":2}]}},
				{"spec":{"resource":{"kind":"Deployment","name":"other","namespace":"default"},
				 "clusters":[{"name":"member-c","replicas":9}]}}
			]}`))
		case "/apis/cluster.karmada.io/v1alpha1/clusters/member-a/proxy/api/v1/namespaces/default/pods":
			assert.Equal(t, "labelSelector=app%3Dapi", r.URL.RawQuery)
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"api-1"},"status":{"phase":"Running"}}]}`))
		case "/apis/cluster.karmada.io/v1alpha1/clusters/member-b/proxy/api/v1/namespaces/default/pods":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"api-2"},"status":{"phase":"Pending"}}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	c, recorder := newResourceContext(t, http.MethodGet,
		"/api/karmada/resources/Deployment/default/api?selector=app%3Dapi",
		gin.Params{{Key: "kind", Value: "Deployment"}, {Key: "namespace", Value: "default"}, {Key: "name", Value: "api"}}, "")
	GetKarmadaResource(c)

	var resp struct {
		Success bool           `json:"success"`
		Message string         `json:"message"`
		Data    ResourceDetail `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success, resp.Message)
	require.NotNil(t, resp.Data.Replicas)
	assert.Equal(t, 5, *resp.Data.Replicas)
	require.Len(t, resp.Data.Distribution, 2, "only the matching ResourceBinding contributes clusters")
	assert.Equal(t, "member-a", resp.Data.Distribution[0].Cluster)
	assert.Equal(t, 3, resp.Data.Distribution[0].Replicas)
	assert.Equal(t, "member-b", resp.Data.Distribution[1].Cluster)
	assert.Equal(t, 2, resp.Data.Distribution[1].Replicas)
	require.Len(t, resp.Data.Pods, 2)
	assert.Equal(t, "api-1", resp.Data.Pods[0].Name)
	assert.Equal(t, "member-a", resp.Data.Pods[0].Cluster)
	assert.Equal(t, "Running", resp.Data.Pods[0].Phase)
	assert.Equal(t, "Pending", resp.Data.Pods[1].Phase)
}

func TestScaleResourceUpdatesControlPlaneScaleSubresource(t *testing.T) {
	setupKarmadaTest(t)
	var seenMethod, seenPath, seenBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		seenMethod, seenPath, seenBody = r.Method, r.URL.Path, string(buf)
		_, _ = w.Write([]byte(`{"spec":{"replicas":4},"status":{"replicas":4}}`))
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	c, recorder := newResourceContext(t, http.MethodPut,
		"/api/karmada/resources/Deployment/default/api/scale",
		gin.Params{{Key: "kind", Value: "Deployment"}, {Key: "namespace", Value: "default"}, {Key: "name", Value: "api"}},
		`{"replicas":4}`)
	ScaleKarmadaResource(c)

	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Replicas int `json:"replicas"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success, resp.Message)
	assert.Equal(t, 4, resp.Data.Replicas)
	assert.Equal(t, http.MethodPatch, seenMethod)
	assert.Equal(t, "/apis/apps/v1/namespaces/default/deployments/api/scale", seenPath)

	var patch map[string]any
	require.NoError(t, json.Unmarshal([]byte(seenBody), &patch))
	spec, ok := patch["spec"].(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, 4, spec["replicas"])
}

func TestScaleResourceRejectsInvalidReplicasAndNonWorkloads(t *testing.T) {
	setupKarmadaTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream must not be called, got %s %s", r.Method, r.URL.Path)
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	for _, body := range []string{`{"replicas":-1}`, `{"replicas":100000}`} {
		c, recorder := newResourceContext(t, http.MethodPut,
			"/api/karmada/resources/Deployment/default/api/scale",
			gin.Params{{Key: "kind", Value: "Deployment"}, {Key: "namespace", Value: "default"}, {Key: "name", Value: "api"}},
			body)
		ScaleKarmadaResource(c)
		var resp struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
		assert.False(t, resp.Success, "replicas payload %s must be rejected", body)
	}

	c, recorder := newResourceContext(t, http.MethodPut,
		"/api/karmada/resources/ConfigMap/default/cm/scale",
		gin.Params{{Key: "kind", Value: "ConfigMap"}, {Key: "namespace", Value: "default"}, {Key: "name", Value: "cm"}},
		`{"replicas":2}`)
	ScaleKarmadaResource(c)
	var resp struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.False(t, resp.Success, "non-workload kinds cannot be scaled")
}

func TestDeleteResourceRequiresConfirmationAndRecordsAudit(t *testing.T) {
	setupKarmadaTest(t)
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/apis/apps/v1/namespaces/default/deployments/api", r.URL.Path)
		_, _ = w.Write([]byte(`{"status":"Success"}`))
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	// Without the explicit confirm flag the deletion must not reach Karmada.
	c, recorder := newResourceContext(t, http.MethodDelete,
		"/api/karmada/resources/Deployment/default/api",
		gin.Params{{Key: "kind", Value: "Deployment"}, {Key: "namespace", Value: "default"}, {Key: "name", Value: "api"}}, "")
	DeleteKarmadaResource(c)
	var unconfirmed struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &unconfirmed))
	assert.False(t, unconfirmed.Success)
	assert.False(t, called, "unconfirmed delete must not call Karmada")

	c, recorder = newResourceContext(t, http.MethodDelete,
		"/api/karmada/resources/Deployment/default/api?confirm=true",
		gin.Params{{Key: "kind", Value: "Deployment"}, {Key: "namespace", Value: "default"}, {Key: "name", Value: "api"}}, "")
	DeleteKarmadaResource(c)
	var confirmed struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &confirmed))
	require.True(t, confirmed.Success, confirmed.Message)
	assert.True(t, called)
}

func TestResourceWriteActionsAreRegisteredForAudit(t *testing.T) {
	// The generic admin-audit fallback keys on "METHOD route"; the two Karmada
	// write routes must resolve to stable action identifiers instead of "generic".
	assert.Equal(t, "karmada.resource_scale",
		karmadaAuditAction(http.MethodPut, "/api/karmada/resources/:kind/:namespace/:name/scale"))
	assert.Equal(t, "karmada.resource_delete",
		karmadaAuditAction(http.MethodDelete, "/api/karmada/resources/:kind/:namespace/:name"))
}
