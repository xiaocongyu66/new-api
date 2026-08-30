package egress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
)

// WorkerRequest Worker请求的数据结构
type WorkerRequest struct {
	URL     string            `json:"url"`
	Key     string            `json:"key"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// DoWorkerRequest 通过Worker发送请求
func DoWorkerRequest(req *WorkerRequest) (*http.Response, error) {
	if !EnableWorker() {
		return nil, fmt.Errorf("worker not enabled")
	}
	if !WorkerAllowHttpImageRequestEnabled && !strings.HasPrefix(req.URL, "https") {
		return nil, fmt.Errorf("only support https url")
	}

	// SSRF防护：验证请求URL
	fetchSetting := GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(req.URL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, fmt.Errorf("request reject: %v", err)
	}

	workerUrl := WorkerUrl
	if !strings.HasSuffix(workerUrl, "/") {
		workerUrl += "/"
	}

	// 序列化worker请求数据
	workerPayload, err := common.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal worker payload: %v", err)
	}

	return GetHttpClient().Post(workerUrl, "application/json", bytes.NewBuffer(workerPayload))
}

func DoDownloadRequest(originUrl string, reason ...string) (resp *http.Response, err error) {
	if EnableWorker() {
		common.SysLog(fmt.Sprintf("downloading file from worker: %s, reason: %s", originUrl, strings.Join(reason, ", ")))
		req := &WorkerRequest{
			URL: originUrl,
			Key: WorkerValidKey,
		}
		return DoWorkerRequest(req)
	} else {
		// SSRF防护：验证请求URL（非Worker模式）
		if err := ValidateSSRFProtectedFetchURL(originUrl); err != nil {
			return nil, fmt.Errorf("request reject: %v", err)
		}

		common.SysLog(fmt.Sprintf("downloading from origin: %s, reason: %s", common.MaskSensitiveInfo(originUrl), strings.Join(reason, ", ")))
		return GetSSRFProtectedHTTPClient().Get(originUrl)
	}
}
