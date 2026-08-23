package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
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
func ShouldCopyUpstreamHeader(c *gin.Context, k string, v []string) bool {
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

func IOCopyBytesGracefully(c *gin.Context, src *http.Response, data []byte) {
	if c.Writer == nil {
		return
	}

	// 输出侧敏感检测（非流式）：目标域名无条件终止；其余敏感词按开关（默认开）。
	// 命中即响应体替换为 content_filter 错误，状态码换 403。用户要求输出默认 block。
	if d := CheckSensitiveTargets(string(data)); d != "" {
		common.SysError(fmt.Sprintf("non-stream output blocked by target domain: [%s]", d))
		data = []byte(`{"error":{"message":"output blocked by content filter","type":"content_filter","param":null,"code":"content_filter"}}`)
		src = &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": []string{"application/json"}}}
	} else if setting.ShouldCheckCompletionSensitive() {
		if hit, label := CheckSensitiveOutput(string(data)); hit {
			common.SysError(fmt.Sprintf("non-stream output blocked by sensitive filter: [%s]", label))
			data = []byte(`{"error":{"message":"output blocked by content filter","type":"content_filter","param":null,"code":"content_filter"}}`)
			// 显式携带 Content-Type：header 复制循环会从 src.Header 覆盖式设置，
			// 不带的话上游的 text/plain 等会盖掉 json 声明。
			src = &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": []string{"application/json"}}}
		}
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
			c.Writer.Header().Set(k, v[0])
		}
	}

	// set Content-Length header manually BEFORE calling WriteHeader
	c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	// Write header with status code (this sends the headers)
	if src != nil {
		c.Writer.WriteHeader(src.StatusCode)
	} else {
		c.Writer.WriteHeader(http.StatusOK)
	}

	_, err := io.Copy(c.Writer, body)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("failed to copy response body: %s", err.Error()))
	}
	c.Writer.Flush()
}
