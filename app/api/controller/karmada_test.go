package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupKarmadaTest(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.KarmadaConfig{}))
	model.DB = db
	Set(nil)
	t.Cleanup(func() {
		model.DB = previousDB
		Set(nil)
	})
}

func makeKubeconfig(server, token string) string {
	return `apiVersion: v1
kind: Config
current-context: karmada
clusters:
- name: karmada
  cluster:
    server: ` + server + `
users:
- name: karmada-user
  user:
    token: ` + token + `
contexts:
- name: karmada
  context:
    cluster: karmada
    user: karmada-user
`
}

func marshalConfigRequest(t *testing.T, body string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"kubeconfig": body})
	require.NoError(t, err)
	return string(b)
}

func doPostConfig(t *testing.T, kubeconfig string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/karmada/config", strings.NewReader(marshalConfigRequest(t, kubeconfig)))
	c.Request.Header.Set("Content-Type", "application/json")
	PostKarmadaConfig(c)
	return recorder
}

func TestServerFromKubeconfig(t *testing.T) {
	server, err := ServerFromKubeconfig(makeKubeconfig("https://karmada.example:5443", "tok"))
	require.NoError(t, err)
	assert.Equal(t, "https://karmada.example:5443", server)
}

func TestServerFromKubeconfigRejectsMissing(t *testing.T) {
	_, err := ServerFromKubeconfig("not-yaml: [")
	require.Error(t, err)
	_, err = ServerFromKubeconfig("apiVersion: v1\ncurrent-context: none")
	require.Error(t, err)
}

func TestPersistenceStoresCiphertextNotPlaintext(t *testing.T) {
	setupKarmadaTest(t)
	plaintext := makeKubeconfig("https://karmada.example:5443", "super-secret-token")
	encrypted, err := common.EncryptSecret(plaintext)
	require.NoError(t, err)
	require.NotContains(t, encrypted, "super-secret-token")
	require.NoError(t, model.SaveKarmadaConfig("https://karmada.example:5443", encrypted))

	var stored model.KarmadaConfig
	require.NoError(t, model.DB.Where("id = ?", 1).First(&stored).Error)
	assert.NotEqual(t, plaintext, stored.EncryptedKubeconfig)
	assert.NotContains(t, stored.EncryptedKubeconfig, "super-secret-token")
	decrypted, err := common.DecryptSecret(stored.EncryptedKubeconfig)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestClientBuildsAuthHeaderFromKubeconfig(t *testing.T) {
	client, err := newClientFromKubeconfig(makeKubeconfig("https://karmada.example:5443", "abc-token"))
	require.NoError(t, err)
	assert.Equal(t, "https://karmada.example:5443", client.Server)
	assert.Equal(t, "Bearer abc-token", client.authHeader)
}

func TestClientRejectsMissingToken(t *testing.T) {
	kc := strings.Replace(makeKubeconfig("https://x:5443", "t"), "    token: t", "    client-certificate-data: abc", 1)
	_, err := newClientFromKubeconfig(kc)
	require.Error(t, err)
}

func TestClientHonorsInsecureSkipTLSVerify(t *testing.T) {
	kc := strings.Replace(
		makeKubeconfig("https://x:5443", "tok"),
		"    server: https://x:5443",
		"    server: https://x:5443\n    insecure-skip-tls-verify: true",
		1,
	)
	client, err := newClientFromKubeconfig(kc)
	require.NoError(t, err)
	assert.Equal(t, "Bearer tok", client.authHeader)
	assert.Equal(t, "https://x:5443", client.Server)
}

func TestClientRejectsBadCACertificate(t *testing.T) {
	kc := strings.Replace(
		makeKubeconfig("https://x:5443", "tok"),
		"    server: https://x:5443",
		"    server: https://x:5443\n    certificate-authority-data: not-base64-or-pem",
		1,
	)
	_, err := newClientFromKubeconfig(kc)
	require.Error(t, err)
}

func TestPostGetDeleteConfigLifecycle(t *testing.T) {
	setupKarmadaTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	// POST valid kubeconfig -> stored
	recorder := doPostConfig(t, makeKubeconfig(upstream.URL, "tok1"))
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success, "POST config: %s", resp.Message)

	// GET -> configured with server, secret never exposed
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/karmada/config", nil)
	GetKarmadaConfig(c)
	var cfgResp struct {
		Success bool `json:"success"`
		Data    struct {
			Configured bool   `json:"configured"`
			Server     string `json:"server"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &cfgResp))
	assert.True(t, cfgResp.Success)
	assert.True(t, cfgResp.Data.Configured)
	assert.Equal(t, upstream.URL, cfgResp.Data.Server)
	assert.NotContains(t, rec.Body.String(), "tok1")

	// POST with empty kubeconfig -> error
	rec2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(rec2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/karmada/config", strings.NewReader(`{"kubeconfig":""}`))
	PostKarmadaConfig(c2)
	var emptyResp struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(rec2.Body.Bytes(), &emptyResp))
	assert.False(t, emptyResp.Success)

	// DELETE -> removed and client cleared
	rec3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(rec3)
	c3.Request = httptest.NewRequest(http.MethodDelete, "/api/karmada/config", nil)
	DeleteKarmadaConfig(c3)
	cfg, err := model.GetKarmadaConfig()
	require.NoError(t, err)
	assert.Nil(t, cfg)
	_, err = Get()
	assert.ErrorIs(t, err, ErrNoConfig)
}

func TestPostKarmadaConfigRejectsInvalidReplacementWithoutOverwritingActiveConfig(t *testing.T) {
	setupKarmadaTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	recorder := doPostConfig(t, makeKubeconfig(upstream.URL, "tok1"))
	var initial struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &initial))
	require.True(t, initial.Success)

	before, err := model.GetKarmadaConfig()
	require.NoError(t, err)
	require.NotNil(t, before)

	invalid := strings.Replace(makeKubeconfig(upstream.URL, "tok2"), "token: tok2", "token: ", 1)
	rejected := doPostConfig(t, invalid)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(rejected.Body.Bytes(), &response))
	require.False(t, response.Success)

	after, err := model.GetKarmadaConfig()
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, before.EncryptedKubeconfig, after.EncryptedKubeconfig)

	current, err := Get()
	require.NoError(t, err)
	assert.Equal(t, upstream.URL, current.Server)
}

func TestListClustersMapsMemberClusters(t *testing.T) {
	setupKarmadaTest(t)
	t.Setenv("PROMETHEUS_URL", "")
	upstreamBody := `{"items":[
		{"metadata":{"name":"member-a"},"status":{"conditions":[{"type":"Ready","status":"Ready"}],"nodeSummary":{"readyNum":3,"totalNum":3},"kubernetesVersion":"v1.27.0"}},
		{"metadata":{"name":"member-b"},"status":{"conditions":[{"type":"Ready","status":"True"}],"nodeSummary":{"readyNum":1,"totalNum":1},"kubernetesVersion":"v1.26.3"}}
	]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamBody))
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
	assert.Equal(t, "member-a", resp.Data.Clusters[0].Name)
	assert.Equal(t, "Ready", resp.Data.Clusters[0].Status)
	assert.Equal(t, 3, resp.Data.Clusters[0].ReadyNodes)
	assert.Equal(t, 3, resp.Data.Clusters[0].TotalNodes)
	assert.Equal(t, "v1.27.0", resp.Data.Clusters[0].Version)
	assert.Equal(t, "member-b", resp.Data.Clusters[1].Name)
}

func TestGetKarmadaClusterEscapesName(t *testing.T) {
	setupKarmadaTest(t)
	t.Setenv("PROMETHEUS_URL", "")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() == "/apis/cluster.karmada.io/v1alpha1/clusters/member%2Fcluster" {
			_, _ = w.Write([]byte(`{"metadata":{"name":"member/cluster"}}`))
			return
		}
		// Member-cluster proxy queries keep the escaped cluster name in the path.
		assert.Contains(t, r.URL.EscapedPath(), "/clusters/member%2Fcluster/proxy/")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/karmada/clusters/member%2Fcluster", nil)
	c.Params = gin.Params{{Key: "name", Value: "member/cluster"}}
	GetKarmadaCluster(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestListClustersRequiresConfig(t *testing.T) {
	setupKarmadaTest(t)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/karmada/clusters", nil)
	ListKarmadaClusters(c)
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "no configuration")
}

func TestProxyForwardsRawResponseAndQuery(t *testing.T) {
	setupKarmadaTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/apis/v1/node/summary", r.URL.Path)
		assert.Equal(t, "resourceVersion=1&limit=2", r.URL.RawQuery)
		assert.Equal(t, "Bearer tok1", r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("Cookie"))
		w.Header().Set("Set-Cookie", "upstream-session=leak; Path=/")
		w.Header().Set("X-Karmada", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"raw":"value"}`))
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/karmada/proxy/apis/v1/node/summary?resourceVersion=1&limit=2", nil)
	c.Request.Header.Set("Cookie", "new_api_refresh=secret")
	c.Params = gin.Params{{Key: "path", Value: "/apis/v1/node/summary"}}
	ProxyKarmada(c)

	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Empty(t, recorder.Header().Get("Set-Cookie"))
	assert.Equal(t, "yes", recorder.Header().Get("X-Karmada"))
	assert.Equal(t, `{"raw":"value"}`, recorder.Body.String())
}

func TestProxyRejectsDisallowedPath(t *testing.T) {
	setupKarmadaTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called for disallowed path")
	}))
	defer upstream.Close()
	client, err := newClientFromKubeconfig(makeKubeconfig(upstream.URL, "tok1"))
	require.NoError(t, err)
	Set(client)

	disallowed := []string{
		"/metrics",
		"/debug/pprof",
		"/healthz",
		"/api/v2/secrets",
		"/apisix",
	}
	for _, p := range disallowed {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/karmada/proxy"+p, nil)
		c.Params = gin.Params{{Key: "path", Value: p}}
		ProxyKarmada(c)
		var resp map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp), "path %s", p)
		assert.False(t, resp["success"].(bool), "path %s should be rejected", p)
	}
}
