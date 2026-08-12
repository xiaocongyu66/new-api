package karmada

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// ConfigRequest is the JSON shape for POST /api/karmada/config. The kubeconfig
// field carries the raw YAML kubeconfig that the backend encrypts at rest.
type ConfigRequest struct {
	Kubeconfig string `json:"kubeconfig"`
}

// ConfigResponse describes the current Karmada configuration status. It never
// includes the encrypted or plaintext kubeconfig.
type ConfigResponse struct {
	Configured bool   `json:"configured"`
	Server     string `json:"server,omitempty"`
	UpdatedAt  int64  `json:"updated_at,omitempty"`
}

// MemberCluster is the simplified view of a Karmada member cluster returned by
// the /api/karmada/clusters endpoints.
type MemberCluster struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	NodeCount int    `json:"node_count"`
	Version   string `json:"version"`
}

// PostKarmadaConfig validates and encrypts an uploaded kubeconfig, persists it
// in the singleton Karmada config row, and hot-reloads the in-process client.
func PostKarmadaConfig(c *gin.Context) {
	var req ConfigRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	raw := strings.TrimSpace(req.Kubeconfig)
	if raw == "" {
		common.ApiErrorMsg(c, "kubeconfig is required")
		return
	}
	server, err := ServerFromKubeconfig(raw)
	if err != nil {
		common.ApiErrorMsg(c, "invalid kubeconfig: "+err.Error())
		return
	}
	client, err := newClientFromKubeconfig(raw)
	if err != nil {
		common.ApiErrorMsg(c, "invalid kubeconfig: "+err.Error())
		return
	}
	encrypted, err := common.EncryptSecret(raw)
	if err != nil {
		common.ApiErrorMsg(c, "failed to encrypt kubeconfig")
		return
	}
	if err := model.SaveKarmadaConfig(server, encrypted); err != nil {
		common.ApiErrorMsg(c, "failed to persist config: "+err.Error())
		return
	}
	Set(client)
	common.ApiSuccess(c, nil)
}

// GetKarmadaConfig returns whether a Karmada kubeconfig is configured and the
// API server URL. It never exposes the encrypted or plaintext secret.
func GetKarmadaConfig(c *gin.Context) {
	cfg, err := model.GetKarmadaConfig()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if cfg == nil {
		common.ApiSuccess(c, ConfigResponse{Configured: false})
		return
	}
	common.ApiSuccess(c, ConfigResponse{
		Configured: true,
		Server:     cfg.Server,
		UpdatedAt:  cfg.UpdatedAt,
	})
}

// DeleteKarmadaConfig removes the stored Karmada configuration and clears the
// in-process client.
func DeleteKarmadaConfig(c *gin.Context) {
	if err := model.DeleteKarmadaConfig(); err != nil {
		common.ApiError(c, err)
		return
	}
	Set(nil)
	common.ApiSuccess(c, nil)
}

// ListKarmadaClusters returns a simplified view of every Karmada member
// cluster.
func ListKarmadaClusters(c *gin.Context) {
	client, err := Get()
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	items, err := fetchClusterItems(client)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	members := make([]MemberCluster, 0, len(items))
	for _, it := range items {
		members = append(members, it.toMemberCluster())
	}
	common.ApiSuccess(c, gin.H{"clusters": members})
}

// GetKarmadaCluster returns details of a single Karmada member cluster.
func GetKarmadaCluster(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		common.ApiErrorMsg(c, "cluster name is required")
		return
	}
	client, err := Get()
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	body, err := forward(client, http.MethodGet,
		fmt.Sprintf("/apis/cluster.karmada.io/v1alpha1/namespaces/karmada-cluster/clusters/%s", url.PathEscape(name)))
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	var cluster map[string]any
	if err := common.Unmarshal(body, &cluster); err != nil {
		common.ApiErrorMsg(c, "invalid cluster response")
		return
	}
	common.ApiSuccess(c, cluster)
}

// ProxyKarmada forwards any request under /api/karmada/proxy/* to the Karmada
// API server with the admin-set credentials, preserving the method, body,
// status and response headers. It is a raw passthrough, not a wrapped JSON
// response.
func ProxyKarmada(c *gin.Context) {
	client, err := Get()
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	path := c.Param("path")
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	target := strings.TrimRight(client.Server, "/") + path
	if query := c.Request.URL.RawQuery; query != "" {
		target += "?" + query
	}
	req, err := http.NewRequest(c.Request.Method, target, c.Request.Body)
	if err != nil {
		common.ApiErrorMsg(c, "karmada proxy: "+err.Error())
		return
	}
	copyHeaders(req.Header, c.Request.Header, true)
	req.Header.Set("Authorization", client.authHeader)

	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		common.ApiErrorMsg(c, "karmada proxy: "+err.Error())
		return
	}
	defer resp.Body.Close()
	copyHeaders(c.Writer.Header(), resp.Header, false)
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(c.Writer, resp.Body)
}

// forward performs an authenticated request and returns the response body,
// surface non-2xx statuses as errors (with a truncated body excerpt).
func forward(client *Client, method, path string) ([]byte, error) {
	resp, err := client.Do(method, path, nil, http.Header{"Accept": []string{"application/json"}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("karmada api returned status %d: %s", resp.StatusCode, truncate(string(body), 256))
	}
	return body, nil
}

func copyHeaders(dst, src http.Header, stripAuth bool) {
	for k, vs := range src {
		if isHopByHop(k) {
			continue
		}
		if stripAuth && strings.EqualFold(k, "Authorization") {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

func isHopByHop(header string) bool {
	switch strings.ToLower(header) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
		"te", "trailers", "transfer-encoding", "upgrade":
		return true
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// cluster wire shapes.
type clusterList struct {
	Items []clusterItem `json:"items"`
}

type clusterItem struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Status clusterStatus `json:"status"`
}

type clusterStatus struct {
	Conditions        []clusterCondition `json:"conditions"`
	NodeSummary       clusterNodeSummary `json:"nodeSummary"`
	KubernetesVersion string             `json:"kubernetesVersion"`
}

type clusterCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type clusterNodeSummary struct {
	ReadyNodes int `json:"readyNodes"`
}

func fetchClusterItems(client *Client) ([]clusterItem, error) {
	body, err := forward(client, http.MethodGet,
		"/apis/cluster.karmada.io/v1alpha1/namespaces/karmada-cluster/clusters")
	if err != nil {
		return nil, err
	}
	var list clusterList
	if err := common.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("decode cluster list: %w", err)
	}
	return list.Items, nil
}

func (it clusterItem) toMemberCluster() MemberCluster {
	status := "Unknown"
	for _, cond := range it.Status.Conditions {
		if cond.Type == "Ready" {
			if cond.Status != "" {
				status = cond.Status
			}
			break
		}
	}
	return MemberCluster{
		Name:      it.Metadata.Name,
		Status:    status,
		NodeCount: it.Status.NodeSummary.ReadyNodes,
		Version:   it.Status.KubernetesVersion,
	}
}
