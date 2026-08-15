package controller

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

const (
	policyAPIVersion      = "policy.karmada.io/v1alpha1"
	maxPolicyRequestBytes = 1 << 20
)

var errPolicyRequestTooLarge = errors.New("policy request body too large")

type policyTypeInfo struct {
	apiPath      string
	plural       string
	namespaced   bool
	resourceName string
}

var policyTypes = map[string]policyTypeInfo{
	"PropagationPolicy": {
		apiPath: policyAPIVersion, plural: "propagationpolicies", namespaced: true, resourceName: "PropagationPolicy",
	},
	"ClusterPropagationPolicy": {
		apiPath: policyAPIVersion, plural: "clusterpropagationpolicies", resourceName: "ClusterPropagationPolicy",
	},
	"OverridePolicy": {
		apiPath: policyAPIVersion, plural: "overridepolicies", namespaced: true, resourceName: "OverridePolicy",
	},
	"ClusterOverridePolicy": {
		apiPath: policyAPIVersion, plural: "clusteroverridepolicies", resourceName: "ClusterOverridePolicy",
	},
}

type policyInput struct {
	Type      string         `json:"type"`
	Namespace string         `json:"namespace"`
	Name      string         `json:"name"`
	Spec      map[string]any `json:"spec"`
	YAML      string         `json:"yaml"`
}

type parsedPolicy struct {
	info      policyTypeInfo
	object    map[string]any
	namespace string
	name      string
}

// forwardPolicyJSON sends a policy JSON document to the Karmada API.
func forwardPolicyJSON(client *Client, method, path string, body []byte) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	headers := http.Header{"Accept": []string{"application/json"}}
	if body != nil {
		headers.Set("Content-Type", "application/json")
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
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("karmada api returned status %d: %s", resp.StatusCode, truncate(string(respBody), 256))
	}
	return respBody, nil
}

func resolvePolicyType(typeParam string) (*policyTypeInfo, error) {
	info, ok := policyTypes[typeParam]
	if !ok {
		return nil, fmt.Errorf("unsupported policy type: %s", typeParam)
	}
	return &info, nil
}

func policyPath(info policyTypeInfo, namespace, name string) string {
	path := "/apis/" + info.apiPath
	if info.namespaced {
		if namespace != "" {
			path += "/namespaces/" + escapePathSegment(namespace)
		}
	}
	path += "/" + info.plural
	if name != "" {
		path += "/" + escapePathSegment(name)
	}
	return path
}

func policyRoute(c *gin.Context) (policyTypeInfo, string, string, error) {
	info, err := resolvePolicyType(c.Param("type"))
	if err != nil {
		return policyTypeInfo{}, "", "", err
	}
	namespace := c.Param("namespace")
	name := c.Param("name")
	if name == "" && !info.namespaced {
		name, namespace = namespace, ""
	}
	if !info.namespaced && namespace != "" {
		return policyTypeInfo{}, "", "", fmt.Errorf("cluster-scoped policy must not specify namespace")
	}
	if name == "" {
		return policyTypeInfo{}, "", "", fmt.Errorf("policy name required")
	}
	if info.namespaced && namespace == "" {
		return policyTypeInfo{}, "", "", fmt.Errorf("namespace required for namespaced policy types")
	}
	return *info, namespace, name, nil
}

func decodePolicyInput(c *gin.Context) (policyInput, error) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxPolicyRequestBytes+1))
	if err != nil {
		return policyInput{}, fmt.Errorf("invalid policy request: %w", err)
	}
	if len(body) > maxPolicyRequestBytes {
		return policyInput{}, errPolicyRequestTooLarge
	}
	var input policyInput
	if err := common.Unmarshal(body, &input); err != nil {
		return policyInput{}, fmt.Errorf("invalid policy request: %w", err)
	}
	return input, nil
}

func parsePolicyYAML(raw string) (map[string]any, error) {
	decoder := yaml.NewDecoder(strings.NewReader(raw))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if document.Kind == 0 {
		return nil, fmt.Errorf("YAML document is empty")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("invalid YAML: %w", err)
		}
		return nil, fmt.Errorf("YAML must contain exactly one document")
	}
	var object map[string]any
	if err := document.Decode(&object); err != nil {
		return nil, fmt.Errorf("YAML must describe an object: %w", err)
	}
	if object == nil {
		return nil, fmt.Errorf("YAML must describe an object")
	}
	return object, nil
}

func parsePolicyObject(object map[string]any) (parsedPolicy, error) {
	apiVersion := getString(object, "apiVersion")
	if apiVersion != policyAPIVersion {
		return parsedPolicy{}, fmt.Errorf("apiVersion must be %s", policyAPIVersion)
	}
	info, err := resolvePolicyType(getString(object, "kind"))
	if err != nil {
		return parsedPolicy{}, err
	}
	metadata := getMap(object, "metadata")
	name := getString(metadata, "name")
	if name == "" {
		return parsedPolicy{}, fmt.Errorf("metadata.name required")
	}
	namespace := getString(metadata, "namespace")
	if info.namespaced && namespace == "" {
		return parsedPolicy{}, fmt.Errorf("metadata.namespace required for namespaced policy types")
	}
	if !info.namespaced && namespace != "" {
		return parsedPolicy{}, fmt.Errorf("cluster-scoped policy must not specify metadata.namespace")
	}
	if _, ok := object["spec"].(map[string]any); !ok {
		return parsedPolicy{}, fmt.Errorf("spec must be an object")
	}
	return parsedPolicy{info: *info, object: object, namespace: namespace, name: name}, nil
}

func createPolicyObject(input policyInput) (parsedPolicy, error) {
	if input.YAML != "" {
		object, err := parsePolicyYAML(input.YAML)
		if err != nil {
			return parsedPolicy{}, err
		}
		parsed, err := parsePolicyObject(object)
		if err != nil {
			return parsedPolicy{}, err
		}
		if input.Type != "" && input.Type != parsed.info.resourceName {
			return parsedPolicy{}, fmt.Errorf("type must match YAML kind")
		}
		return parsed, nil
	}
	info, err := resolvePolicyType(input.Type)
	if err != nil {
		return parsedPolicy{}, err
	}
	if input.Name == "" {
		return parsedPolicy{}, fmt.Errorf("name required")
	}
	if info.namespaced && input.Namespace == "" {
		return parsedPolicy{}, fmt.Errorf("namespace required for namespaced policy types")
	}
	if !info.namespaced && input.Namespace != "" {
		return parsedPolicy{}, fmt.Errorf("cluster-scoped policy must not specify namespace")
	}
	if input.Spec == nil {
		return parsedPolicy{}, fmt.Errorf("spec required")
	}
	metadata := map[string]any{"name": input.Name}
	if info.namespaced {
		metadata["namespace"] = input.Namespace
	}
	return parsedPolicy{
		info: *info, namespace: input.Namespace, name: input.Name,
		object: map[string]any{
			"apiVersion": policyAPIVersion,
			"kind":       info.resourceName,
			"metadata":   metadata,
			"spec":       input.Spec,
		},
	}, nil
}

func updatePolicySpec(input policyInput, info policyTypeInfo, namespace, name string) (map[string]any, error) {
	if input.YAML == "" {
		if input.Spec == nil {
			return nil, fmt.Errorf("spec or YAML required")
		}
		return input.Spec, nil
	}
	object, err := parsePolicyYAML(input.YAML)
	if err != nil {
		return nil, err
	}
	parsed, err := parsePolicyObject(object)
	if err != nil {
		return nil, err
	}
	if parsed.info.resourceName != info.resourceName {
		return nil, fmt.Errorf("YAML kind must match URL type")
	}
	if parsed.name != name || parsed.namespace != namespace {
		return nil, fmt.Errorf("YAML metadata must match the policy URL")
	}
	return getMap(parsed.object, "spec"), nil
}

// ListKarmadaPolicies returns policies filtered by type and optional namespace.
// GET /api/karmada/policies?type=<type>&namespace=<namespace>
func ListKarmadaPolicies(c *gin.Context) {
	info, err := resolvePolicyType(c.Query("type"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	namespace := c.Query("namespace")
	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	body, err := forward(client, http.MethodGet, policyPath(*info, namespace, ""))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := common.Unmarshal(body, &list); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	items := make([]map[string]any, 0, len(list.Items))
	for _, item := range list.Items {
		metadata := getMap(item, "metadata")
		items = append(items, map[string]any{
			"name":      getString(metadata, "name"),
			"type":      info.resourceName,
			"namespace": getString(metadata, "namespace"),
			"createdAt": getString(metadata, "creationTimestamp"),
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": items}})
}

// GetKarmadaPolicy returns policy YAML and live ResourceBinding matches.
func GetKarmadaPolicy(c *gin.Context) {
	info, namespace, name, err := policyRoute(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	body, err := forward(client, http.MethodGet, policyPath(info, namespace, name))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	var policy map[string]any
	if err := common.Unmarshal(body, &policy); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	yamlBody, err := yaml.Marshal(policy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	matches, err := fetchPolicyMatches(client, namespace, info.namespaced, policy)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"policy":            policy,
		"yaml":              string(yamlBody),
		"matched_resources": matches,
	}})
}

// CreateKarmadaPolicy creates a policy from structured JSON or a single YAML object.
func CreateKarmadaPolicy(c *gin.Context) {
	input, err := decodePolicyInput(c)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errPolicyRequestTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	policy, err := createPolicyObject(input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	body, err := common.Marshal(policy.object)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	response, err := forwardPolicyJSON(client, http.MethodPost, policyPath(policy.info, policy.namespace, ""), body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	var result map[string]any
	if err := common.Unmarshal(response, &result); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// UpdateKarmadaPolicy replaces the policy spec while preserving server metadata.
func UpdateKarmadaPolicy(c *gin.Context) {
	info, namespace, name, err := policyRoute(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	input, err := decodePolicyInput(c)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errPolicyRequestTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	spec, err := updatePolicySpec(input, info, namespace, name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	path := policyPath(info, namespace, name)
	currentBody, err := forward(client, http.MethodGet, path)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	var current map[string]any
	if err := common.Unmarshal(currentBody, &current); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	current["spec"] = spec
	body, err := common.Marshal(current)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	response, err := forwardPolicyJSON(client, http.MethodPut, path, body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	var result map[string]any
	if err := common.Unmarshal(response, &result); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// DeleteKarmadaPolicy removes a policy only when confirm matches its name.
func DeleteKarmadaPolicy(c *gin.Context) {
	info, namespace, name, err := policyRoute(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if c.Query("confirm") != name {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "deletion requires confirm=<policy name>"})
		return
	}
	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	if _, err := forwardPolicyJSON(client, http.MethodDelete, policyPath(info, namespace, name), nil); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "policy deleted"})
}

func fetchPolicyMatches(client *Client, namespace string, namespaced bool, policy map[string]any) ([]map[string]any, error) {
	selectors, _ := getMap(policy, "spec")["resourceSelectors"].([]any)
	if len(selectors) == 0 {
		return []map[string]any{}, nil
	}
	basePath := "/apis/work.karmada.io/v1alpha2"
	paths := []string{basePath + "/resourcebindings", basePath + "/clusterresourcebindings"}
	if namespaced {
		paths = []string{basePath + "/namespaces/" + escapePathSegment(namespace) + "/resourcebindings"}
	}

	metadata := getMap(policy, "metadata")
	labels := getStringMap(metadata, "labels")
	labelKey := ""
	switch getString(policy, "kind") {
	case "PropagationPolicy":
		labelKey = "propagationpolicy.karmada.io/permanent-id"
	case "ClusterPropagationPolicy":
		labelKey = "clusterpropagationpolicy.karmada.io/permanent-id"
	}
	permanentID := labels[labelKey]
	if permanentID != "" {
		bindingSelector := url.QueryEscape(labelKey + "=" + permanentID)
		for index := range paths {
			paths[index] += "?labelSelector=" + bindingSelector
		}
	}

	matches := make([]map[string]any, 0)
	seen := make(map[string]struct{})
	for _, path := range paths {
		body, err := forward(client, http.MethodGet, path)
		if err != nil {
			return nil, err
		}
		var list struct {
			Items []map[string]any `json:"items"`
		}
		if err := common.Unmarshal(body, &list); err != nil {
			return nil, err
		}
		for _, binding := range list.Items {
			resource := getMap(getMap(binding, "spec"), "resource")
			matched := permanentID != ""
			for _, rawSelector := range selectors {
				selector, ok := rawSelector.(map[string]any)
				if ok && policySelectorMatches(selector, resource) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			match := map[string]any{
				"apiVersion": getString(resource, "apiVersion"),
				"kind":       getString(resource, "kind"),
				"namespace":  getString(resource, "namespace"),
				"name":       getString(resource, "name"),
			}
			key := fmt.Sprintf("%s/%s/%s/%s", match["apiVersion"], match["kind"], match["namespace"], match["name"])
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				matches = append(matches, match)
			}
		}
	}
	return matches, nil
}

func policySelectorMatches(selector, resource map[string]any) bool {
	for _, key := range []string{"apiVersion", "kind", "namespace"} {
		if expected := getString(selector, key); expected != "" && expected != getString(resource, key) {
			return false
		}
	}
	if name := getString(selector, "name"); name != "" {
		return name == getString(resource, "name")
	}
	labelSelector := getMap(selector, "labelSelector")
	if len(labelSelector) == 0 {
		return true
	}
	labels := getStringMap(resource, "labels")
	for key, value := range getMap(labelSelector, "matchLabels") {
		if labels[key] != fmt.Sprint(value) {
			return false
		}
	}
	matchExpressions, _ := labelSelector["matchExpressions"].([]any)
	for _, rawExpression := range matchExpressions {
		expression, ok := rawExpression.(map[string]any)
		if !ok {
			return false
		}
		key := getString(expression, "key")
		if key == "" {
			return false
		}
		value, exists := labels[key]
		matchesValue := false
		values, _ := expression["values"].([]any)
		for _, rawValue := range values {
			if value == fmt.Sprint(rawValue) {
				matchesValue = true
				break
			}
		}
		switch getString(expression, "operator") {
		case "In":
			if !exists || !matchesValue {
				return false
			}
		case "NotIn":
			if exists && matchesValue {
				return false
			}
		case "Exists":
			if !exists {
				return false
			}
		case "DoesNotExist":
			if exists {
				return false
			}
		default:
			return false
		}
	}
	return true
}
