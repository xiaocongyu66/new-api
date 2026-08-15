package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const topologyListLimit = 50

type topologyNode struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Name      string         `json:"name"`
	Namespace string         `json:"namespace,omitempty"`
	Cluster   string         `json:"cluster,omitempty"`
	Status    string         `json:"status"`
	Metadata  map[string]any `json:"metadata"`
}

type topologyEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type topologyObjectList struct {
	Items []map[string]any `json:"items"`
}

// GetKarmadaTopology returns one bounded, server-built propagation chain per
// matching ResourceBinding. It intentionally queries only the caller-selected
// namespace/kind/cluster to keep Work and member API calls proportional to the
// rendered graph rather than every resource in the control plane.
// GET /api/karmada/topology?namespace=<namespace>&kind=<kind>&cluster=<cluster>
func GetKarmadaTopology(c *gin.Context) {
	namespace := strings.TrimSpace(c.Query("namespace"))
	kind := strings.TrimSpace(c.Query("kind"))
	cluster := strings.TrimSpace(c.Query("cluster"))
	if namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "namespace required"})
		return
	}
	if kind == "" {
		kind = "Deployment"
	}
	resourceKind, err := resolveResourceKind(kind)
	if err != nil || !resourceKind.namespaced || !resourceKind.workload {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "topology supports Deployment, StatefulSet, and DaemonSet"})
		return
	}

	client, err := Get()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	query := url.Values{"limit": []string{fmt.Sprint(topologyListLimit)}}.Encode()
	policies, err := topologyList(client, "/apis/policy.karmada.io/v1alpha1/namespaces/"+escapePathSegment(namespace)+"/propagationpolicies?"+query)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	bindings, err := topologyList(client, "/apis/work.karmada.io/v1alpha2/namespaces/"+escapePathSegment(namespace)+"/resourcebindings?"+query)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}

	policyNodes := make(map[string]topologyNode, len(policies)*2)
	for _, policy := range policies {
		metadata := getMap(policy, "metadata")
		name := getString(metadata, "name")
		if name == "" {
			continue
		}
		node := topologyNode{
			ID:        topologyID("policy", namespace, name),
			Type:      "PropagationPolicy",
			Name:      name,
			Namespace: namespace,
			Status:    "healthy",
			Metadata:  metadata,
		}
		policyNodes[name] = node
		if permanentID := topologyPolicyID(policy); permanentID != "" {
			policyNodes[permanentID] = node
		}
	}

	nodes := make([]topologyNode, 0)
	edges := make([]topologyEdge, 0)
	seenNodes := make(map[string]struct{})
	seenEdges := make(map[string]struct{})
	addNode := func(node topologyNode) {
		if _, exists := seenNodes[node.ID]; exists {
			return
		}
		seenNodes[node.ID] = struct{}{}
		nodes = append(nodes, node)
	}
	addEdge := func(from, to string) {
		key := from + "\x00" + to
		if _, exists := seenEdges[key]; exists {
			return
		}
		seenEdges[key] = struct{}{}
		edges = append(edges, topologyEdge{From: from, To: to})
	}

	for _, binding := range bindings {
		bindingSpec := getMap(binding, "spec")
		resource := getMap(bindingSpec, "resource")
		if getString(resource, "kind") != kind || getString(resource, "namespace") != namespace {
			continue
		}
		bindingMetadata := getMap(binding, "metadata")
		bindingName := getString(bindingMetadata, "name")
		resourceName := getString(resource, "name")
		labels := getStringMap(bindingMetadata, "labels")
		policyKey := labels["propagationpolicy.karmada.io/permanent-id"]
		if policyKey == "" {
			policyKey = labels["propagationpolicy.karmada.io/name"]
			if policyNamespace := labels["propagationpolicy.karmada.io/namespace"]; policyNamespace != "" && policyNamespace != namespace {
				continue
			}
		}
		policy, ok := policyNodes[policyKey]
		if !ok || bindingName == "" || resourceName == "" {
			continue
		}
		bindingNode := topologyNode{
			ID:        topologyID("binding", namespace, bindingName),
			Type:      "ResourceBinding",
			Name:      bindingName,
			Namespace: namespace,
			Status:    conditionStatus(getMap(binding, "status"), "Scheduled", "Dispatched"),
			Metadata:  bindingMetadata,
		}
		addNode(policy)
		addNode(bindingNode)
		addEdge(policy.ID, bindingNode.ID)

		permanentID := labels["resourcebinding.karmada.io/permanent-id"]
		for _, target := range topologyClusters(bindingSpec, cluster) {
			works, err := topologyWorks(client, target, permanentID)
			if err != nil {
				common.SysError(fmt.Sprintf("karmada: fetch topology Works from cluster %s: %v", target, err))
				continue
			}
			for _, work := range works {
				workMetadata := getMap(work, "metadata")
				workName := getString(workMetadata, "name")
				if workName == "" {
					continue
				}
				workNode := topologyNode{
					ID:        topologyID("work", target, workName),
					Type:      "Work",
					Name:      workName,
					Namespace: getString(workMetadata, "namespace"),
					Cluster:   target,
					Status:    conditionStatus(getMap(work, "status"), "Applied"),
					Metadata:  workMetadata,
				}
				addNode(workNode)
				addEdge(bindingNode.ID, workNode.ID)

				workload, err := topologyWorkload(client, resourceKind, target, namespace, resourceName)
				if err != nil {
					common.SysError(fmt.Sprintf("karmada: fetch topology %s %s/%s from cluster %s: %v", kind, namespace, resourceName, target, err))
					continue
				}
				workloadNode := topologyNode{
					ID:        topologyID(strings.ToLower(kind), target, namespace, resourceName),
					Type:      kind,
					Name:      resourceName,
					Namespace: namespace,
					Cluster:   target,
					Status:    topologyWorkloadStatus(kind, workload),
					Metadata:  getMap(workload, "metadata"),
				}
				addNode(workloadNode)
				addEdge(workNode.ID, workloadNode.ID)
				for _, pod := range topologyPods(client, target, namespace, selectorFromSpec(getMap(workload, "spec"))) {
					podMetadata := getMap(pod, "metadata")
					podName := getString(podMetadata, "name")
					if podName == "" {
						continue
					}
					podNode := topologyNode{
						ID:        topologyID("pod", target, namespace, podName),
						Type:      "Pod",
						Name:      podName,
						Namespace: namespace,
						Cluster:   target,
						Status:    podStatus(pod),
						Metadata:  podMetadata,
					}
					addNode(podNode)
					addEdge(workloadNode.ID, podNode.ID)
				}
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"nodes": nodes, "edges": edges}})
}

func topologyList(client *Client, path string) ([]map[string]any, error) {
	body, err := forward(client, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	var list topologyObjectList
	if err := common.Unmarshal(body, &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func topologyClusters(binding map[string]any, only string) []string {
	clusters, _ := binding["clusters"].([]any)
	result := make([]string, 0, len(clusters))
	for _, raw := range clusters {
		cluster, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := getString(cluster, "name")
		if name != "" && (only == "" || name == only) {
			result = append(result, name)
		}
	}
	return result
}

func topologyPolicyID(policy map[string]any) string {
	return getStringMap(getMap(policy, "metadata"), "labels")["propagationpolicy.karmada.io/permanent-id"]
}

func topologyWorks(client *Client, cluster, bindingID string) ([]map[string]any, error) {
	query := url.Values{"limit": []string{fmt.Sprint(topologyListLimit)}}
	if bindingID != "" {
		query.Set("labelSelector", "resourcebinding.karmada.io/permanent-id="+bindingID)
	}
	return topologyList(client, "/apis/work.karmada.io/v1alpha1/namespaces/karmada-es-"+escapePathSegment(cluster)+"/works?"+query.Encode())
}

func topologyWorkload(client *Client, kind resourceKind, cluster, namespace, name string) (map[string]any, error) {
	body, err := forward(client, http.MethodGet, buildResourcePath(kind, namespace, name, cluster))
	if err != nil {
		return nil, err
	}
	var workload map[string]any
	if err := common.Unmarshal(body, &workload); err != nil {
		return nil, err
	}
	return workload, nil
}

func topologyPods(client *Client, cluster, namespace, selector string) []map[string]any {
	query := url.Values{"limit": []string{fmt.Sprint(topologyListLimit)}}
	if selector != "" {
		query.Set("labelSelector", selector)
	}
	pods, err := topologyList(client, memberProxyPath(cluster, "/api/v1/namespaces/"+escapePathSegment(namespace)+"/pods")+"?"+query.Encode())
	if err != nil {
		return nil
	}
	return pods
}

func topologyID(parts ...string) string {
	return strings.Join(parts, "/")
}

func conditionStatus(status map[string]any, expected ...string) string {
	conditions, _ := status["conditions"].([]any)
	found := false
	syncing := false
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for _, conditionType := range expected {
			if getString(condition, "type") != conditionType {
				continue
			}
			found = true
			switch getString(condition, "status") {
			case "False":
				return "failed"
			case "True":
			default:
				syncing = true
			}
		}
	}
	if syncing || !found {
		return "syncing"
	}
	return "healthy"
}

func deploymentStatus(deployment map[string]any) string {
	spec := getMap(deployment, "spec")
	status := getMap(deployment, "status")
	desired := getInt(spec, "replicas")
	ready := getInt(status, "readyReplicas")
	if desired == ready {
		return "healthy"
	}
	if ready > desired || getInt(status, "unavailableReplicas") > 0 {
		return "failed"
	}
	return "syncing"
}

func topologyWorkloadStatus(kind string, workload map[string]any) string {
	if kind != "DaemonSet" {
		return deploymentStatus(workload)
	}
	status := getMap(workload, "status")
	desired := getInt(status, "desiredNumberScheduled")
	ready := getInt(status, "numberReady")
	if desired == ready {
		return "healthy"
	}
	if ready > desired || getInt(status, "numberUnavailable") > 0 {
		return "failed"
	}
	return "syncing"
}

func podStatus(pod map[string]any) string {
	status := getMap(pod, "status")
	if getString(status, "phase") == "Failed" {
		return "failed"
	}
	containers, _ := status["containerStatuses"].([]any)
	if len(containers) == 0 {
		return "syncing"
	}
	for _, raw := range containers {
		container, ok := raw.(map[string]any)
		if !ok || container["ready"] != true {
			return "syncing"
		}
	}
	return "healthy"
}
