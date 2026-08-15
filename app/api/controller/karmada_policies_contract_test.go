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

func writePolicyResponse(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	body, err := common.Marshal(value)
	require.NoError(t, err)
	_, err = w.Write(body)
	require.NoError(t, err)
}

func TestCreatePolicyAcceptsOnePolicyYAMLAndRejectsInvalidKinds(t *testing.T) {
	setupKarmadaTest(t)
	var requests int
	var created map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, common.Unmarshal(body, &created))
		writePolicyResponse(t, w, created)
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	validYAML := `{"yaml":"apiVersion: policy.karmada.io/v1alpha1\nkind: PropagationPolicy\nmetadata:\n  name: yaml-policy\n  namespace: default\nspec:\n  placement:\n    clusterAffinity:\n      clusterNames:\n        - member-a\n"}`
	c, recorder := newPolicyContext(t, http.MethodPost, "/api/karmada/policies", nil, validYAML)
	CreateKarmadaPolicy(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, requests)
	assert.Equal(t, "PropagationPolicy", created["kind"])

	invalidYAML := `{"yaml":"apiVersion: v1\nkind: Pod\nmetadata:\n  name: forbidden\n  namespace: default\nspec: {}\n"}`
	c, recorder = newPolicyContext(t, http.MethodPost, "/api/karmada/policies", nil, invalidYAML)
	CreateKarmadaPolicy(c)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, 1, requests, "invalid policy kinds must not reach Karmada")
}

func TestCreatePolicyRejectsOversizedRequest(t *testing.T) {
	setupKarmadaTest(t)
	yaml := "apiVersion: policy.karmada.io/v1alpha1\nkind: PropagationPolicy\nmetadata:\n  name: oversized\n  namespace: default\nspec: {}\n#" + strings.Repeat("x", 1<<20)
	body, err := common.Marshal(map[string]string{"yaml": yaml})
	require.NoError(t, err)

	c, recorder := newPolicyContext(t, http.MethodPost, "/api/karmada/policies", nil, string(body))
	CreateKarmadaPolicy(c)

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestUpdatePolicyRejectsOversizedRequest(t *testing.T) {
	setupKarmadaTest(t)
	body, err := common.Marshal(map[string]string{"yaml": strings.Repeat("x", 1<<20)})
	require.NoError(t, err)

	c, recorder := newPolicyContext(t, http.MethodPut, "/api/karmada/policies/PropagationPolicy/namespaces/default/oversized", gin.Params{
		{Key: "type", Value: "PropagationPolicy"},
		{Key: "namespace", Value: "default"},
		{Key: "name", Value: "oversized"},
	}, string(body))
	UpdateKarmadaPolicy(c)

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestGetPolicyReturnsYAMLAndMatchedResources(t *testing.T) {
	setupKarmadaTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/policy.karmada.io/v1alpha1/namespaces/default/propagationpolicies/deploy-prod":
			writePolicyResponse(t, w, map[string]any{
				"apiVersion": "policy.karmada.io/v1alpha1",
				"kind":       "PropagationPolicy",
				"metadata":   map[string]any{"name": "deploy-prod", "namespace": "default"},
				"spec": map[string]any{"resourceSelectors": []any{
					map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "web"},
				}},
			})
		case "/apis/work.karmada.io/v1alpha2/namespaces/default/resourcebindings":
			writePolicyResponse(t, w, map[string]any{
				"items": []any{
					map[string]any{"spec": map[string]any{"resource": map[string]any{
						"apiVersion": "apps/v1", "kind": "Deployment", "namespace": "default", "name": "web",
					}}},
					map[string]any{"spec": map[string]any{"resource": map[string]any{
						"apiVersion": "apps/v1", "kind": "Deployment", "namespace": "default", "name": "other",
					}}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	c, recorder := newPolicyContext(t, http.MethodGet,
		"/api/karmada/policies/PropagationPolicy/default/deploy-prod",
		gin.Params{{Key: "type", Value: "PropagationPolicy"}, {Key: "namespace", Value: "default"}, {Key: "name", Value: "deploy-prod"}}, "")
	GetKarmadaPolicy(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			YAML             string           `json:"yaml"`
			MatchedResources []map[string]any `json:"matched_resources"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Contains(t, response.Data.YAML, "kind: PropagationPolicy")
	require.Len(t, response.Data.MatchedResources, 1)
	assert.Equal(t, "web", response.Data.MatchedResources[0]["name"])
}

func TestClusterPolicyUpdateUsesNameOnlyRoute(t *testing.T) {
	setupKarmadaTest(t)
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method == http.MethodGet {
			writePolicyResponse(t, w, map[string]any{
				"apiVersion": "policy.karmada.io/v1alpha1",
				"kind":       "ClusterPropagationPolicy",
				"metadata":   map[string]any{"name": "global-policy", "resourceVersion": "7"},
				"spec":       map[string]any{"placement": map[string]any{}},
			})
			return
		}
		writePolicyResponse(t, w, map[string]any{"metadata": map[string]any{"name": "global-policy"}})
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	c, recorder := newPolicyContext(t, http.MethodPut,
		"/api/karmada/policies/ClusterPropagationPolicy/global-policy",
		gin.Params{{Key: "type", Value: "ClusterPropagationPolicy"}, {Key: "name", Value: "global-policy"}},
		`{"spec":{"placement":{"clusterAffinity":{"clusterNames":["member-a"]}}}}`)
	UpdateKarmadaPolicy(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, []string{
		"/apis/policy.karmada.io/v1alpha1/clusterpropagationpolicies/global-policy",
		"/apis/policy.karmada.io/v1alpha1/clusterpropagationpolicies/global-policy",
	}, paths)
}

func TestClusterPolicyDetailUsesClusterResourceBindings(t *testing.T) {
	setupKarmadaTest(t)

	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/apis/policy.karmada.io/v1alpha1/clusterpropagationpolicies/global-policy":
			writePolicyResponse(t, w, map[string]any{
				"apiVersion": "policy.karmada.io/v1alpha1",
				"kind":       "ClusterPropagationPolicy",
				"metadata":   map[string]any{"name": "global-policy"},
				"spec": map[string]any{"resourceSelectors": []any{
					map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "web"},
				}},
			})
		case "/apis/work.karmada.io/v1alpha2/resourcebindings":
			writePolicyResponse(t, w, map[string]any{"items": []any{}})
		case "/apis/work.karmada.io/v1alpha2/clusterresourcebindings":
			writePolicyResponse(t, w, map[string]any{"items": []any{
				map[string]any{"spec": map[string]any{"resource": map[string]any{
					"apiVersion": "apps/v1", "kind": "Deployment", "name": "web",
				}}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	c, recorder := newPolicyContext(t, http.MethodGet,
		"/api/karmada/policies/ClusterPropagationPolicy/global-policy",
		gin.Params{{Key: "type", Value: "ClusterPropagationPolicy"}, {Key: "name", Value: "global-policy"}}, "")
	GetKarmadaPolicy(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, []string{
		"/apis/policy.karmada.io/v1alpha1/clusterpropagationpolicies/global-policy",
		"/apis/work.karmada.io/v1alpha2/resourcebindings",
		"/apis/work.karmada.io/v1alpha2/clusterresourcebindings",
	}, paths)
}
func TestClusterPolicyRejectsNamespacedRoute(t *testing.T) {
	setupKarmadaTest(t)
	c, recorder := newPolicyContext(t, http.MethodGet,
		"/api/karmada/policies/ClusterPropagationPolicy/namespaces/default/global-policy",
		gin.Params{{Key: "type", Value: "ClusterPropagationPolicy"}, {Key: "namespace", Value: "default"}, {Key: "name", Value: "global-policy"}}, "")

	GetKarmadaPolicy(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestPropagationPolicyDetailUsesPermanentIDBindingSelector(t *testing.T) {
	setupKarmadaTest(t)
	var bindingSelector string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/policy.karmada.io/v1alpha1/namespaces/default/propagationpolicies/deploy-prod":
			writePolicyResponse(t, w, map[string]any{
				"apiVersion": "policy.karmada.io/v1alpha1",
				"kind":       "PropagationPolicy",
				"metadata": map[string]any{
					"name":      "deploy-prod",
					"namespace": "default",
					"labels": map[string]any{
						"propagationpolicy.karmada.io/permanent-id": "policy-42",
					},
				},
				"spec": map[string]any{"resourceSelectors": []any{
					map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "labelSelector": map[string]any{"matchLabels": map[string]any{"app": "web"}}},
				}},
			})
		case "/apis/work.karmada.io/v1alpha2/namespaces/default/resourcebindings":
			bindingSelector = r.URL.Query().Get("labelSelector")
			writePolicyResponse(t, w, map[string]any{"items": []any{
				map[string]any{"spec": map[string]any{"resource": map[string]any{
					"apiVersion": "apps/v1", "kind": "Deployment", "namespace": "default", "name": "web",
				}}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	c, recorder := newPolicyContext(t, http.MethodGet,
		"/api/karmada/policies/PropagationPolicy/default/deploy-prod",
		gin.Params{{Key: "type", Value: "PropagationPolicy"}, {Key: "namespace", Value: "default"}, {Key: "name", Value: "deploy-prod"}}, "")
	GetKarmadaPolicy(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "propagationpolicy.karmada.io/permanent-id=policy-42", bindingSelector)
	var response struct {
		Data struct {
			MatchedResources []map[string]any `json:"matched_resources"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data.MatchedResources, 1)
}

func TestClusterPropagationPolicyDetailIncludesBothBindingScopes(t *testing.T) {
	setupKarmadaTest(t)
	var paths []string
	var bindingSelectors []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/apis/policy.karmada.io/v1alpha1/clusterpropagationpolicies/global-policy":
			writePolicyResponse(t, w, map[string]any{
				"apiVersion": "policy.karmada.io/v1alpha1",
				"kind":       "ClusterPropagationPolicy",
				"metadata": map[string]any{
					"name": "global-policy",
					"labels": map[string]any{
						"clusterpropagationpolicy.karmada.io/permanent-id": "policy-42",
					},
				},
				"spec": map[string]any{"resourceSelectors": []any{
					map[string]any{"apiVersion": "apps/v1", "kind": "Deployment"},
				}},
			})
		case "/apis/work.karmada.io/v1alpha2/resourcebindings":
			bindingSelectors = append(bindingSelectors, r.URL.Query().Get("labelSelector"))
			writePolicyResponse(t, w, map[string]any{"items": []any{
				map[string]any{"spec": map[string]any{"resource": map[string]any{
					"apiVersion": "apps/v1", "kind": "Deployment", "namespace": "default", "name": "namespaced-web",
				}}},
			}})
		case "/apis/work.karmada.io/v1alpha2/clusterresourcebindings":
			bindingSelectors = append(bindingSelectors, r.URL.Query().Get("labelSelector"))
			writePolicyResponse(t, w, map[string]any{"items": []any{
				map[string]any{"spec": map[string]any{"resource": map[string]any{
					"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRole", "name": "cluster-reader",
				}}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	c, recorder := newPolicyContext(t, http.MethodGet,
		"/api/karmada/policies/ClusterPropagationPolicy/global-policy",
		gin.Params{{Key: "type", Value: "ClusterPropagationPolicy"}, {Key: "name", Value: "global-policy"}}, "")
	GetKarmadaPolicy(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, []string{
		"/apis/policy.karmada.io/v1alpha1/clusterpropagationpolicies/global-policy",
		"/apis/work.karmada.io/v1alpha2/resourcebindings",
		"/apis/work.karmada.io/v1alpha2/clusterresourcebindings",
	}, paths)
	assert.Equal(t, []string{
		"clusterpropagationpolicy.karmada.io/permanent-id=policy-42",
		"clusterpropagationpolicy.karmada.io/permanent-id=policy-42",
	}, bindingSelectors)
}

func TestPolicySelectorNameOverridesLabelSelector(t *testing.T) {
	assert.True(t, policySelectorMatches(
		map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"name":       "web",
			"labelSelector": map[string]any{
				"matchLabels": map[string]any{"app": "other"},
			},
		},
		map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "namespace": "default", "name": "web"},
	))
}

func TestPolicySelectorMatchesLabelExpressions(t *testing.T) {
	selector := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"labelSelector": map[string]any{
			"matchLabels": map[string]any{"app": "web"},
			"matchExpressions": []any{
				map[string]any{"key": "tier", "operator": "In", "values": []any{"edge", "worker"}},
				map[string]any{"key": "debug", "operator": "DoesNotExist"},
			},
		},
	}
	resource := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"labels": map[string]any{
			"app": "web", "tier": "edge",
		},
	}

	assert.True(t, policySelectorMatches(selector, resource))
}
