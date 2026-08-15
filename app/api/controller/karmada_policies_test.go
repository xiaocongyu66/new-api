package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPolicyContext builds a Gin context for policy handlers.
func newPolicyContext(t *testing.T, method, path string, params gin.Params, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	c.Request = httptest.NewRequest(method, path, reader)
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	c.Params = params
	return c, recorder
}

func TestListPoliciesSupportsTypeAndNamespaceFilters(t *testing.T) {
	setupKarmadaTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/policy.karmada.io/v1alpha1/")
		writePolicyResponse(t, w, map[string]any{
			"items": []map[string]any{
				{"metadata": map[string]any{"name": "deploy-prod", "namespace": "default", "creationTimestamp": "2026-08-14T00:00:00Z"}},
			},
		})
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	c, recorder := newPolicyContext(t, http.MethodGet,
		"/api/karmada/policies?type=PropagationPolicy&namespace=default",
		nil, "")
	c.Request.URL.RawQuery = "type=PropagationPolicy&namespace=default"
	ListKarmadaPolicies(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	assert.Len(t, resp.Data.Items, 1)
}

func TestCreatePolicyValidatesYAMLAndCallsKarmadaAPI(t *testing.T) {
	setupKarmadaTest(t)
	var seenBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/policy.karmada.io/v1alpha1/")
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, r.Body)
		seenBody = buf.String()
		writePolicyResponse(t, w, map[string]any{"metadata": map[string]any{"name": "new-policy"}})
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	payload := `{"type":"PropagationPolicy","namespace":"default","name":"new-policy","spec":{"placement":{"clusterAffinity":{"clusterNames":["member-a"]}}}}`
	c, recorder := newPolicyContext(t, http.MethodPost,
		"/api/karmada/policies",
		nil, payload)
	CreateKarmadaPolicy(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	assert.Contains(t, seenBody, "clusterNames")
}

func TestUpdatePolicyMergesSpecAndPreservesMetadata(t *testing.T) {
	setupKarmadaTest(t)
	var seenMethod, seenPath, seenBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenPath = r.URL.Path
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, r.Body)
		seenBody = buf.String()
		writePolicyResponse(t, w, map[string]any{"metadata": map[string]any{"name": "deploy-prod"}})
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	payload := `{"spec":{"placement":{"clusterAffinity":{"clusterNames":["member-b"]}}}}`
	c, recorder := newPolicyContext(t, http.MethodPut,
		"/api/karmada/policies/PropagationPolicy/default/deploy-prod",
		gin.Params{{Key: "type", Value: "PropagationPolicy"}, {Key: "namespace", Value: "default"}, {Key: "name", Value: "deploy-prod"}},
		payload)
	UpdateKarmadaPolicy(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, http.MethodPut, seenMethod)
	assert.Contains(t, seenPath, "/propagationpolicies/deploy-prod")
	assert.Contains(t, seenBody, "member-b")
}

func TestDeletePolicyRequiresConfirmationAndRecordsAudit(t *testing.T) {
	setupKarmadaTest(t)
	var seenMethod, seenPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	c, recorder := newPolicyContext(t, http.MethodDelete,
		"/api/karmada/policies/OverridePolicy/default/env-override?confirm=env-override",
		gin.Params{{Key: "type", Value: "OverridePolicy"}, {Key: "namespace", Value: "default"}, {Key: "name", Value: "env-override"}},
		"")
	c.Request.URL.RawQuery = "confirm=env-override"
	DeleteKarmadaPolicy(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, http.MethodDelete, seenMethod)
	assert.Contains(t, seenPath, "/overridepolicies/env-override")
}

func TestPolicyTypeAllowlistRejectsUnknownTypes(t *testing.T) {
	setupKarmadaTest(t)
	c, recorder := newPolicyContext(t, http.MethodGet,
		"/api/karmada/policies?type=UnknownPolicy",
		nil, "")
	c.Request.URL.RawQuery = "type=UnknownPolicy"
	ListKarmadaPolicies(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	var resp struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.False(t, resp.Success)
}
