package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// resourceKind defines the Kubernetes API structure for each allowed resource kind.
type resourceKind struct {
	apiRoot    string
	plural     string
	namespaced bool
	workload   bool // supports scale subresource
}

// Only kinds from issue #111 are exposed; no path-injection allowed.
var allowedResourceKinds = map[string]resourceKind{
	"Deployment": {
		apiRoot:    "/apis/apps/v1",
		plural:     "deployments",
		namespaced: true,
		workload:   true,
	},
	"StatefulSet": {
		apiRoot:    "/apis/apps/v1",
		plural:     "statefulsets",
		namespaced: true,
		workload:   true,
	},
	"DaemonSet": {
		apiRoot:    "/apis/apps/v1",
		plural:     "daemonsets",
		namespaced: true,
		workload:   true,
	},
	"Service": {
		apiRoot:    "/api/v1",
		plural:     "services",
		namespaced: true,
	},
	"ConfigMap": {
		apiRoot:    "/api/v1",
		plural:     "configmaps",
		namespaced: true,
	},
	"Secret": {
		apiRoot:    "/api/v1",
		plural:     "secrets",
		namespaced: true,
	},
	"Namespace": {
		apiRoot:    "/api/v1",
		plural:     "namespaces",
		namespaced: false,
	},
}

func resolveResourceKind(kind string) (resourceKind, error) {
	info, ok := allowedResourceKinds[kind]
	if !ok {
		return resourceKind{}, fmt.Errorf("resource kind %q not allowed", kind)
	}
	return info, nil
}

type KarmadaResource struct {
	Name          string            `json:"name"`
	Kind          string            `json:"kind"`
	Namespace     string            `json:"namespace,omitempty"`
	Cluster       string            `json:"cluster,omitempty"`
	Status        string            `json:"status"`
	Replicas      *int              `json:"replicas,omitempty"`
	ReadyReplicas *int              `json:"readyReplicas,omitempty"`
	ClusterIP     string            `json:"clusterIP,omitempty"`
	Type          string            `json:"type,omitempty"`
	CreatedAt     string            `json:"createdAt"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type ResourceDetail struct {
	Name         string                `json:"name"`
	Kind         string                `json:"kind"`
	Namespace    string                `json:"namespace,omitempty"`
	Cluster      string                `json:"cluster,omitempty"`
	Status       string                `json:"status"`
	Replicas     *int                  `json:"replicas,omitempty"`
	Distribution []ClusterDistribution `json:"distribution,omitempty"`
	Pods         []ResourcePod         `json:"pods,omitempty"`
	Spec         map[string]any        `json:"spec,omitempty"`
	Labels       map[string]string     `json:"labels,omitempty"`
	Annotations  map[string]string     `json:"annotations,omitempty"`
	CreatedAt    string                `json:"createdAt"`
}

type ClusterDistribution struct {
	Cluster  string `json:"cluster"`
	Replicas int    `json:"replicas"`
}

type ResourcePod struct {
	Name     string `json:"name"`
	Cluster  string `json:"cluster"`
	Phase    string `json:"phase"`
	Ready    string `json:"ready"`
	Restarts int    `json:"restarts"`
	Age      string `json:"age"`
}

// ListKarmadaResources returns a list of resources of a given kind.
// GET /api/karmada/resources/:kind?namespace=<ns>&cluster=<cluster>
func ListKarmadaResources(c *gin.Context) {
	kind := c.Param("kind")
	namespace := c.Query("namespace")
	cluster := c.Query("cluster")

	info, err := resolveResourceKind(kind)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	if info.namespaced && namespace == "" && cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "namespace required for control-plane namespaced resources"})
		return
	}

	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	path := buildResourcePath(info, namespace, "", cluster)
	body, err := forward(client, http.MethodGet, path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := common.Unmarshal(body, &list); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	resources := make([]KarmadaResource, 0, len(list.Items))
	for _, item := range list.Items {
		resources = append(resources, parseResourceItem(item, kind, cluster))
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"resources": resources}})
}

// GetKarmadaResource returns detail for a single resource.
// GET /api/karmada/resources/:kind/:namespace/:name?cluster=<cluster>&selector=<label>
func GetKarmadaResource(c *gin.Context) {
	kind := c.Param("kind")
	namespace := c.Param("namespace")
	name := c.Param("name")
	cluster := c.Query("cluster")
	selector := c.Query("selector")

	info, err := resolveResourceKind(kind)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	path := buildResourcePath(info, namespace, name, cluster)
	body, err := forward(client, http.MethodGet, path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	var raw map[string]any
	if err := common.Unmarshal(body, &raw); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	detail := parseResourceDetail(raw, kind, cluster)

	// For control-plane workloads, fetch distribution and pods.
	if cluster == "" && info.workload {
		distribution, err := fetchDistribution(client, namespace, name, kind)
		if err == nil {
			detail.Distribution = distribution
		}
		if selector != "" {
			pods, err := fetchDistributedPods(client, namespace, selector, distribution)
			if err == nil {
				detail.Pods = pods
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": detail})
}

// ScaleKarmadaResource scales a workload resource at the control plane.
// PUT /api/karmada/resources/:kind/:namespace/:name/scale
func ScaleKarmadaResource(c *gin.Context) {
	kind := c.Param("kind")
	namespace := c.Param("namespace")
	name := c.Param("name")

	info, err := resolveResourceKind(kind)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if !info.workload {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "resource kind does not support scaling"})
		return
	}

	var req struct {
		Replicas int `json:"replicas"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Replicas < 0 || req.Replicas > 10000 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "replicas must be between 0 and 10000"})
		return
	}

	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	scalePath := fmt.Sprintf("%s/namespaces/%s/%s/%s/scale", info.apiRoot, namespace, info.plural, name)
	patch := map[string]any{"spec": map[string]any{"replicas": req.Replicas}}
	patchBody, _ := json.Marshal(patch)

	body, err := forwardWithBody(client, http.MethodPatch, scalePath, patchBody)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	var result struct {
		Spec struct {
			Replicas int `json:"replicas"`
		} `json:"spec"`
	}
	if err := common.Unmarshal(body, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"replicas": result.Spec.Replicas}})
}

// DeleteKarmadaResource deletes a resource at the control plane after confirmation.
// DELETE /api/karmada/resources/:kind/:namespace/:name?confirm=true
func DeleteKarmadaResource(c *gin.Context) {
	kind := c.Param("kind")
	namespace := c.Param("namespace")
	name := c.Param("name")
	confirm := c.Query("confirm") == "true"

	if !confirm {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "deletion requires explicit confirm=true query parameter"})
		return
	}

	info, err := resolveResourceKind(kind)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	path := buildResourcePath(info, namespace, name, "")
	_, err = forward(client, http.MethodDelete, path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "resource deleted"})
}

func buildResourcePath(info resourceKind, namespace, name, cluster string) string {
	var path strings.Builder
	if cluster != "" {
		path.WriteString("/apis/cluster.karmada.io/v1alpha1/clusters/")
		path.WriteString(cluster)
		path.WriteString("/proxy")
	}
	path.WriteString(info.apiRoot)
	if info.namespaced && namespace != "" {
		path.WriteString("/namespaces/")
		path.WriteString(namespace)
	}
	path.WriteString("/")
	path.WriteString(info.plural)
	if name != "" {
		path.WriteString("/")
		path.WriteString(name)
	}
	return path.String()
}

func parseResourceItem(item map[string]any, kind, cluster string) KarmadaResource {
	meta := getMap(item, "metadata")
	spec := getMap(item, "spec")
	status := getMap(item, "status")

	res := KarmadaResource{
		Name:      getString(meta, "name"),
		Kind:      kind,
		Namespace: getString(meta, "namespace"),
		Cluster:   cluster,
		CreatedAt: getString(meta, "creationTimestamp"),
		Labels:    getStringMap(meta, "labels"),
	}

	// Redact Secret data fields.
	if kind == "Secret" {
		delete(item, "data")
		delete(item, "stringData")
	}

	replicas := getIntPtr(spec, "replicas")
	readyReplicas := getIntPtr(status, "readyReplicas")
	actualReplicas := getIntPtr(status, "replicas")

	res.Replicas = replicas
	res.ReadyReplicas = readyReplicas

	if readyReplicas != nil && actualReplicas != nil {
		res.Status = fmt.Sprintf("%d/%d", *readyReplicas, *actualReplicas)
	} else if kind == "Service" {
		res.ClusterIP = getString(spec, "clusterIP")
		res.Type = getString(spec, "type")
		res.Status = res.Type
	} else {
		res.Status = "—"
	}

	return res
}

func parseResourceDetail(item map[string]any, kind, cluster string) ResourceDetail {
	meta := getMap(item, "metadata")
	spec := getMap(item, "spec")
	status := getMap(item, "status")

	detail := ResourceDetail{
		Name:        getString(meta, "name"),
		Kind:        kind,
		Namespace:   getString(meta, "namespace"),
		Cluster:     cluster,
		CreatedAt:   getString(meta, "creationTimestamp"),
		Labels:      getStringMap(meta, "labels"),
		Annotations: getStringMap(meta, "annotations"),
		Spec:        spec,
	}

	if kind == "Secret" {
		delete(item, "data")
		delete(item, "stringData")
		detail.Spec = nil
	}

	replicas := getIntPtr(spec, "replicas")
	readyReplicas := getIntPtr(status, "readyReplicas")
	actualReplicas := getIntPtr(status, "replicas")

	detail.Replicas = replicas

	if readyReplicas != nil && actualReplicas != nil {
		detail.Status = fmt.Sprintf("%d/%d", *readyReplicas, *actualReplicas)
	} else if kind == "Service" {
		detail.Status = getString(spec, "type")
	} else {
		detail.Status = "—"
	}

	return detail
}

func fetchDistribution(client *Client, namespace, name, kind string) ([]ClusterDistribution, error) {
	path := fmt.Sprintf("/apis/work.karmada.io/v1alpha2/namespaces/%s/resourcebindings", namespace)
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
		spec := getMap(binding, "spec")
		resource := getMap(spec, "resource")
		if getString(resource, "kind") == kind &&
			getString(resource, "name") == name &&
			getString(resource, "namespace") == namespace {
			clusters, ok := spec["clusters"].([]any)
			if !ok {
				continue
			}
			distribution := make([]ClusterDistribution, 0, len(clusters))
			for _, c := range clusters {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				distribution = append(distribution, ClusterDistribution{
					Cluster:  getString(cm, "name"),
					Replicas: getInt(cm, "replicas"),
				})
			}
			return distribution, nil
		}
	}
	return nil, nil
}

func fetchDistributedPods(client *Client, namespace, selector string, distribution []ClusterDistribution) ([]ResourcePod, error) {
	var pods []ResourcePod
	for _, dist := range distribution {
		path := fmt.Sprintf("/apis/cluster.karmada.io/v1alpha1/clusters/%s/proxy/api/v1/namespaces/%s/pods?labelSelector=%s",
			dist.Cluster, namespace, selector)
		body, err := forward(client, http.MethodGet, path)
		if err != nil {
			continue
		}
		var list struct {
			Items []map[string]any `json:"items"`
		}
		if err := common.Unmarshal(body, &list); err != nil {
			continue
		}
		for _, item := range list.Items {
			meta := getMap(item, "metadata")
			status := getMap(item, "status")
			pods = append(pods, ResourcePod{
				Name:    getString(meta, "name"),
				Cluster: dist.Cluster,
				Phase:   getString(status, "phase"),
				Ready:   "—", // container readiness would need deeper status parsing
				Age:     "—",
			})
		}
	}
	return pods, nil
}

func forwardWithBody(client *Client, method, path string, body []byte) ([]byte, error) {
	resp, err := client.Do(method, path, bytes.NewReader(body), http.Header{
		"Content-Type": []string{"application/merge-patch+json"},
		"Accept":       []string{"application/json"},
	})
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

func getMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return make(map[string]any)
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]any, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

func getIntPtr(m map[string]any, key string) *int {
	if v, ok := m[key].(float64); ok {
		i := int(v)
		return &i
	}
	return nil
}

func getStringMap(m map[string]any, key string) map[string]string {
	result := make(map[string]string)
	if v, ok := m[key].(map[string]any); ok {
		for k, val := range v {
			if s, ok := val.(string); ok {
				result[k] = s
			}
		}
	}
	return result
}

func karmadaAuditAction(method, route string) string {
	switch {
	case method == http.MethodPut && strings.HasSuffix(route, "/scale"):
		return "karmada.resource_scale"
	case method == http.MethodDelete && strings.Contains(route, "/resources/"):
		return "karmada.resource_delete"
	default:
		return "generic"
	}
}
