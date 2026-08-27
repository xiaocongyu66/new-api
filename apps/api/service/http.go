package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/setting"

	"github.com/QuantumNous/new-api/internal/transport/contract"
)

func CloseResponseBodyGracefully(httpResponse *http.Response) {
	if httpResponse == nil || httpResponse.Body == nil {
		return
	}
	err := httpResponse.Body.Close()
	if err != nil {
		common.SysError("failed to close response body: " + err.Error())
	}
}

// ShouldCopyUpstreamHeader checks whether a given upstream response header
// should be copied to the client response. It returns false for Content-Length
// (managed separately) and X-Oneapi-Request-Id (to preserve the local instance
// ID). When the upstream header is X-Oneapi-Request-Id, the value is captured
// into the Gin context for later logging.
func ShouldCopyUpstreamHeader(c contract.Context, k string, v []string) bool {
	if strings.EqualFold(k, "Content-Length") {
		return false
	}
	if strings.EqualFold(k, common.RequestIdKey) {
		if c != nil && len(v) > 0 {
			c.Set(common.UpstreamRequestIdKey, v[0])
		}
		return false
	}
	return true
}

func IOCopyBytesGracefully(c contract.Context, src *http.Response, data []byte) {
	if c.ResponseWriter() == nil {
		return
	}

	// 输出侧敏感过滤（非流式）：目标域与词库敏感词从响应体中静默切除，
	// 状态码与 Content-Type 保持上游原样，不再替换为 content_filter 错误。
	// ponytail: JSON \uXXXX 转义形式的内容不在折叠范围（上游几乎都发原始 UTF-8）。
	if setting.ShouldCheckCompletionSensitive() {
		if cleaned, labels := SanitizeSensitiveText(string(data)); len(labels) > 0 {
			common.SysError(fmt.Sprintf("non-stream output sanitized by sensitive filter: [%s]", strings.Join(labels, ",")))
			RecordSensitiveBlock(c, "output", "sanitize:"+strings.Join(labels, ","), string(data))
			data = []byte(cleaned)
		}
	} else if cleaned, labels := SanitizeTargetDomains(string(data)); len(labels) > 0 {
		common.SysError(fmt.Sprintf("non-stream output sanitized by target domain filter: [%s]", strings.Join(labels, ",")))
		RecordSensitiveBlock(c, "output", "sanitize:"+strings.Join(labels, ","), string(data))
		data = []byte(cleaned)
	}

	body := io.NopCloser(bytes.NewBuffer(data))

	// We shouldn't set the header before we parse the response body, because the parse part may fail.
	// And then we will have to send an error response, but in this case, the header has already been set.
	// So the httpClient will be confused by the response.
	// For example, Postman will report error, and we cannot check the response at all.
	if src != nil {
		for k, v := range src.Header {
			if !ShouldCopyUpstreamHeader(c, k, v) {
				continue
			}
			c.ResponseWriter().Header().Set(k, v[0])
		}
	}

	// set Content-Length header manually BEFORE calling WriteHeader
	c.ResponseWriter().Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	// Write header with status code (this sends the headers)
	if src != nil {
		c.ResponseWriter().WriteHeader(src.StatusCode)
	} else {
		c.ResponseWriter().WriteHeader(http.StatusOK)
	}

	_, err := io.Copy(c.ResponseWriter(), body)
	if err != nil {
		logger.LogError(c.HTTPRequest().Context(), fmt.Sprintf("failed to copy response body: %s", err.Error()))
	}
	if f, ok := c.ResponseWriter().(http.Flusher); ok {
		f.Flush()
	}
}
