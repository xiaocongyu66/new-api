package upstreamfetch

// browser 档：把页面交给一个外部浏览器适配器，拿回结构化结果。
//
// 为什么走外部进程而不是在 Go 里嵌 Playwright：
//   - 主程序不必背一个 Chromium（几百 MB 镜像、每实例几百 MB 内存）
//   - 适配器可以独立升级、独立重启，崩了不影响 API 服务
//   - 需要登录态时，storageState 留在适配器侧，凭证不进主库
//
// 契约（stdin 一份 JSON，stdout 一份 JSON）：
//
//	入参 {"url":"...","headers":{...},"rows":"...","key":"...","values":{...},
//	      "timeout_seconds":15}
//	出参 {"rows":[{"key":"gpt-5.6-sol","values":{"price":"0.038"}}]}
//
// 适配器负责：启动浏览器、必要时恢复 storageState、等页面渲染、按选择器取值。
// 主程序负责：超时、结果校验（必需字段/数值边界）、失败即整次放弃。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
)

// browserJob 是发给适配器的入参。
type browserJob struct {
	URL            string            `json:"url"`
	Headers        map[string]string `json:"headers,omitempty"`
	Rows           string            `json:"rows"`
	Key            string            `json:"key"`
	Values         map[string]string `json:"values"`
	TimeoutSeconds int               `json:"timeout_seconds"`
}

// browserReply 是适配器的出参。
type browserReply struct {
	Rows []Row `json:"rows"`
}

func fetchBrowser(ctx context.Context, channel *channelpkg.Channel, target string, spec Spec) ([]Row, error) {
	timeout := time.Duration(spec.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second // 浏览器比普通 GET 慢得多
	}
	job := browserJob{
		URL:            target,
		Headers:        spec.Headers,
		Rows:           spec.Rows,
		Key:            spec.Key,
		Values:         spec.Values,
		TimeoutSeconds: int(timeout / time.Second),
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return nil, fmt.Errorf("encode browser job: %w", err)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// #nosec G204 -- adapter_cmd 来自运维配置的 Spec，不是用户输入。
	cmd := exec.CommandContext(cmdCtx, spec.AdapterCmd, spec.AdapterArgs...)
	cmd.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		reason := strings.TrimSpace(stderr.String())
		if reason == "" {
			reason = err.Error()
		}
		return nil, fmt.Errorf("browser adapter failed: %s", reason)
	}

	var reply browserReply
	if err := json.Unmarshal(out, &reply); err != nil {
		return nil, fmt.Errorf("browser adapter returned unparseable json: %w", err)
	}
	if len(reply.Rows) == 0 {
		return nil, fmt.Errorf("browser adapter returned no rows")
	}
	return reply.Rows, nil
}
