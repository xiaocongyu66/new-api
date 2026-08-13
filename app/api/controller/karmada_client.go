package controller

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gopkg.in/yaml.v3"
)

// kubeconfig is the subset of a kubeconfig YAML document that the proxy needs:
// the current context, its cluster (server + CA) and its user (token). Only
// token-based auth is supported, which is the default Karmada kubeconfig
// layout. Values can be inline (base64) or, for the CA, raw PEM.
type kubeconfig struct {
	CurrentContext string              `yaml:"current-context"`
	Clusters       []kubeconfigCluster `yaml:"clusters"`
	Users          []kubeconfigUser    `yaml:"users"`
	Contexts       []kubeconfigContext `yaml:"contexts"`
}

type kubeconfigCluster struct {
	Name    string                `yaml:"name"`
	Cluster kubeconfigClusterData `yaml:"cluster"`
}

type kubeconfigClusterData struct {
	Server                   string `yaml:"server"`
	CertificateAuthorityData string `yaml:"certificate-authority-data"`
	CertificateAuthority     string `yaml:"certificate-authority"`
	InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
}

type kubeconfigUser struct {
	Name string             `yaml:"name"`
	User kubeconfigUserData `yaml:"user"`
}

type kubeconfigUserData struct {
	Token string `yaml:"token"`
}

type kubeconfigContext struct {
	Name    string                `yaml:"name"`
	Context kubeconfigContextData `yaml:"context"`
}

type kubeconfigContextData struct {
	Cluster string `yaml:"cluster"`
	User    string `yaml:"user"`
}

// Client is a thin authenticated HTTP client for the Karmada API server. It
// is constructed from the decrypted kubeconfig of the singleton Karmada
// config row and is hot-reloaded whenever that row changes.
type Client struct {
	Server     string
	HTTPClient *http.Client
	authHeader string
}

var (
	mu      sync.RWMutex
	current *Client
)

// ErrNoConfig is returned when a request needs an active client but the admin
// has not yet stored a kubeconfig.
var ErrNoConfig = errors.New("karmada: no configuration stored")

// Init loads the singleton Karmada config from the database and rebuilds the
// in-process client. It is safe to call at startup and again after every
// config change (hot reload). A cleared or missing row clears the client.
func Init() error {
	cfg, err := model.GetKarmadaConfig()
	if err != nil {
		return fmt.Errorf("karmada: load config: %w", err)
	}
	if cfg == nil {
		Set(nil)
		return nil
	}
	kubeconfigRaw, err := common.DecryptSecret(cfg.EncryptedKubeconfig)
	if err != nil {
		return fmt.Errorf("karmada: decrypt kubeconfig: %w", err)
	}
	client, err := newClientFromKubeconfig(kubeconfigRaw)
	if err != nil {
		return fmt.Errorf("karmada: build client: %w", err)
	}
	Set(client)
	return nil
}

// Set atomically replaces the singleton client. Passing nil clears it.
func Set(c *Client) {
	mu.Lock()
	defer mu.Unlock()
	current = c
}

// Get returns the current client, or ErrNoConfig when none is configured.
func Get() (*Client, error) {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return nil, ErrNoConfig
	}
	return current, nil
}

// ServerFromKubeconfig returns the API server URL declared by the current
// context of a kubeconfig. It is used to validate and store the server URL in
// plaintext during POST /api/karmada/config without decrypting the blob.
func ServerFromKubeconfig(raw string) (string, error) {
	name, server, err := parseKubeconfigNameServer(raw)
	if err != nil {
		return "", err
	}
	if server == "" {
		return "", fmt.Errorf("kubeconfig context %q has no server", name)
	}
	return server, nil
}

func newClientFromKubeconfig(raw string) (*Client, error) {
	var kc kubeconfig
	if err := yaml.Unmarshal([]byte(raw), &kc); err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	ctxName, server, err := resolveContext(&kc)
	if err != nil {
		return nil, err
	}
	token, err := resolveToken(&kc, ctxName)
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{InsecureSkipVerify: kc.insecureFor(ctxName)} // #nosec G402 — operator-controlled kubeconfig flag.
	caData := kc.caDataFor(ctxName)
	if caData != "" && !kc.insecureFor(ctxName) {
		pool := x509.NewCertPool()
		trimmed := strings.TrimSpace(caData)
		appended := pool.AppendCertsFromPEM([]byte(trimmed))
		if !appended {
			if decoded, derr := base64.StdEncoding.DecodeString(trimmed); derr == nil {
				appended = pool.AppendCertsFromPEM(decoded)
			}
		}
		if !appended {
			return nil, errors.New("kubeconfig has invalid CA data")
		}
		tlsConfig.RootCAs = pool
	}
	httpClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
	}
	return &Client{
		Server:     strings.TrimRight(server, "/"),
		HTTPClient: httpClient,
		authHeader: "Bearer " + token,
	}, nil
}

// resolveContext returns the current context name and its cluster server.
func resolveContext(kc *kubeconfig) (string, string, error) {
	name := kc.CurrentContext
	if name == "" && len(kc.Contexts) > 0 {
		name = kc.Contexts[0].Name
	}
	if name == "" {
		return "", "", errors.New("kubeconfig has no current context")
	}
	for _, c := range kc.Contexts {
		if c.Name != name {
			continue
		}
		for _, cl := range kc.Clusters {
			if cl.Name == c.Context.Cluster {
				if strings.TrimSpace(cl.Cluster.Server) == "" {
					return name, "", errors.New("kubeconfig context has no server")
				}
				return name, cl.Cluster.Server, nil
			}
		}
		return name, "", errors.New("kubeconfig context references unknown cluster")
	}
	return name, "", errors.New("kubeconfig has no matching context")
}

func resolveToken(kc *kubeconfig, ctxName string) (string, error) {
	var userName string
	for _, c := range kc.Contexts {
		if c.Name == ctxName {
			userName = c.Context.User
			break
		}
	}
	for _, u := range kc.Users {
		if u.Name == userName {
			if strings.TrimSpace(u.User.Token) == "" {
				return "", errors.New("kubeconfig user has no token")
			}
			return strings.TrimSpace(u.User.Token), nil
		}
	}
	return "", errors.New("kubeconfig context references unknown user")
}

func (kc *kubeconfig) caDataFor(ctxName string) string {
	for _, c := range kc.Contexts {
		if c.Name != ctxName {
			continue
		}
		for _, cl := range kc.Clusters {
			if cl.Name == c.Context.Cluster {
				data := cl.Cluster.CertificateAuthorityData
				if data == "" {
					data = cl.Cluster.CertificateAuthority
				}
				return data
			}
		}
	}
	return ""
}

func (kc *kubeconfig) insecureFor(ctxName string) bool {
	for _, c := range kc.Contexts {
		if c.Name != ctxName {
			continue
		}
		for _, cl := range kc.Clusters {
			if cl.Name == c.Context.Cluster {
				return cl.Cluster.InsecureSkipTLSVerify
			}
		}
	}
	return false
}

// parseKubeconfigNameServer returns the current context name and its server
// URL, mirroring resolveContext for ServerFromKubeconfig's error messages.
func parseKubeconfigNameServer(raw string) (string, string, error) {
	var kc kubeconfig
	if err := yaml.Unmarshal([]byte(raw), &kc); err != nil {
		return "", "", fmt.Errorf("parse kubeconfig: %w", err)
	}
	return resolveContext(&kc)
}

// Do performs an authenticated request against the Karmada API. path must
// start with "/". Headers passed via headers are copied (Authorization from
// the client is always set, overriding any caller-supplied value).
func (c *Client) Do(method, path string, body io.Reader, headers http.Header) (*http.Response, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	full := strings.TrimRight(c.Server, "/") + path
	req, err := http.NewRequest(method, full, body)
	if err != nil {
		return nil, err
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Authorization", c.authHeader)
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	return c.HTTPClient.Do(req)
}
