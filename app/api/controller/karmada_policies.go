package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// forwardWithBody sends an authenticated request with an optional JSON body.
// GET/DELETE with no body use the package-level forward().
func forwardWithBody(client *Client, method, path string, body []byte) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	headers := http.Header{"Accept": []string{"application/json"}}
	if body != nil {
		headers["Content-Type"] = []string{"application/json"}
	}
	resp, err := client.Do(method, path, bodyReader, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("karmada api returned status %d: %s", resp.StatusCode, truncate(string(respBody), 256))
	}
	return respBody, nil
}

// logKarmadaAction is a placeholder audit hook; the admin audit middleware
// records the structured entry, this keeps a stable call site for future
// per-action content.
// vibekit: no-op audit content, wire to recordManageAudit if policy-specific content is needed
func logKarmadaAction(action string) { _ = action }

type policyTypeInfo struct {
	apiPath      string
	plural       string
	namespaced   bool
	resourceName string
}

var policyTypes = map[string]policyTypeInfo{
	"PropagationPolicy": {
		apiPath:      "/apis/policy.karmada.io/v1alpha1",
		plural:       "propagationpolicies",
		namespaced:   true,
		resourceName: "PropagationPolicy",
	},
	"ClusterPropagationPolicy": {
		apiPath:      "/apis/policy.karmada.io/v1alpha1",
		plural:       "clusterpropagationpolicies",
		namespaced:   false,
		resourceName: "ClusterPropagationPolicy",
	},
	"OverridePolicy": {
		apiPath:      "/apis/policy.karmada.io/v1alpha1",
		plural:       "overridepolicies",
		namespaced:   true,
		resourceName: "OverridePolicy",
	},
	"ClusterOverridePolicy": {
		apiPath:      "/apis/policy.karmada.io/v1alpha1",
		plural:       "clusteroverridepolicies",
		namespaced:   false,
		resourceName: "ClusterOverridePolicy",
	},
}

func resolvePolicyType(typeParam string) (*policyTypeInfo, error) {
	info, ok := policyTypes[typeParam]
	if !ok {
		return nil, fmt.Errorf("unsupported policy type: %s", typeParam)
	}
	return &info, nil
}

// ListKarmadaPolicies returns policies filtered by type and namespace.
// GET /api/karmada/policies?type=<type>&namespace=<ns>
func ListKarmadaPolicies(c *gin.Context) {
	policyType := c.Query("type")
	namespace := c.Query("namespace")

	if policyType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "type parameter required"})
		return
	}

	info, err := resolvePolicyType(policyType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	if info.namespaced && namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "namespace required for namespaced policy types"})
		return
	}

	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	var path string
	if info.namespaced {
		path = fmt.Sprintf("%s/namespaces/%s/%s", info.apiPath, namespace, info.plural)
	} else {
		path = fmt.Sprintf("%s/%s", info.apiPath, info.plural)
	}

	body, err := forward(client, http.MethodGet, path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": list.Items}})
}

// CreateKarmadaPolicy creates a new policy.
// POST /api/karmada/policies
// Body: {"type":"PropagationPolicy","namespace":"default","spec":{...}}
func CreateKarmadaPolicy(c *gin.Context) {
	var req struct {
		Type      string         `json:"type"`
		Namespace string         `json:"namespace"`
		Name      string         `json:"name"`
		Spec      map[string]any `json:"spec"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	info, err := resolvePolicyType(req.Type)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	if info.namespaced && req.Namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "namespace required for namespaced policy types"})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "name required"})
		return
	}

	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	policyObj := map[string]any{
		"apiVersion": "policy.karmada.io/v1alpha1",
		"kind":       info.resourceName,
		"metadata": map[string]any{
			"name": req.Name,
		},
		"spec": req.Spec,
	}
	if info.namespaced {
		policyObj["metadata"].(map[string]any)["namespace"] = req.Namespace
	}

	var path string
	if info.namespaced {
		path = fmt.Sprintf("%s/namespaces/%s/%s", info.apiPath, req.Namespace, info.plural)
	} else {
		path = fmt.Sprintf("%s/%s", info.apiPath, info.plural)
	}

	bodyBytes, err := json.Marshal(policyObj)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	respBody, err := forwardWithBody(client, http.MethodPost, path, bodyBytes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	logKarmadaAction(fmt.Sprintf("create policy %s/%s", req.Type, req.Name))
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// UpdateKarmadaPolicy updates an existing policy.
// PUT /api/karmada/policies/:type/:namespace/:name (namespaced)
// PUT /api/karmada/policies/:type/:name (cluster-scoped)
func UpdateKarmadaPolicy(c *gin.Context) {
	policyType := c.Param("type")
	namespace := c.Param("namespace")
	name := c.Param("name")

	// For cluster-scoped policies, namespace param is actually the name
	info, err := resolvePolicyType(policyType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	if !info.namespaced {
		name = namespace
		namespace = ""
	}

	var req struct {
		Spec map[string]any `json:"spec"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	var path string
	if info.namespaced {
		path = fmt.Sprintf("%s/namespaces/%s/%s/%s", info.apiPath, namespace, info.plural, name)
	} else {
		path = fmt.Sprintf("%s/%s/%s", info.apiPath, info.plural, name)
	}

	// Fetch current policy to merge spec
	currentBody, err := forward(client, http.MethodGet, path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	var current map[string]any
	if err := json.Unmarshal(currentBody, &current); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	current["spec"] = req.Spec

	bodyBytes, err := json.Marshal(current)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	respBody, err := forwardWithBody(client, http.MethodPut, path, bodyBytes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	logKarmadaAction(fmt.Sprintf("update policy %s/%s", policyType, name))
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// DeleteKarmadaPolicy deletes a policy.
// DELETE /api/karmada/policies/:type/:namespace/:name?confirm=true
func DeleteKarmadaPolicy(c *gin.Context) {
	confirm := c.Query("confirm")
	if confirm != "true" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "deletion requires confirm=true"})
		return
	}

	policyType := c.Param("type")
	namespace := c.Param("namespace")
	name := c.Param("name")

	info, err := resolvePolicyType(policyType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	if !info.namespaced {
		name = namespace
		namespace = ""
	}

	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	var path string
	if info.namespaced {
		path = fmt.Sprintf("%s/namespaces/%s/%s/%s", info.apiPath, namespace, info.plural, name)
	} else {
		path = fmt.Sprintf("%s/%s/%s", info.apiPath, info.plural, name)
	}

	_, err = forwardWithBody(client, http.MethodDelete, path, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	logKarmadaAction(fmt.Sprintf("delete policy %s/%s", policyType, name))
	c.JSON(http.StatusOK, gin.H{"success": true})
}
