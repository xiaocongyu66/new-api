package handler

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/perfsource"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/upstreamfetch"
)

// 前端编辑已配置的密钥时不会回传明文；__MASKED__ 是历史约定，收到它同样
// 视为“保持原值”。空串也保持原值，所以删除密钥要用 PUT 覆盖而不是清空。
const upstreamAccountMaskedSecret = "__MASKED__"

type upstreamAccountRequest struct {
	Id           int    `json:"id"`
	BaseURL      string `json:"base_url"`
	Kind         string `json:"kind"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	JKGroupID    string `json:"jk_group_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Enabled      *bool  `json:"enabled"`
}

type upstreamAccountTestResult struct {
	OK         bool     `json:"ok"`
	Kind       string   `json:"kind"`
	Message    string   `json:"message"`
	ModelCount int      `json:"model_count"`
	GroupIDs   []string `json:"group_ids"`
}

func GetUpstreamAccounts(c contract.Context) {
	accounts, err := channelpkg.ListUpstreamAccounts()
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	items := make([]channelpkg.UpstreamAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, account.ToResponse())
	}
	common.CtxApiSuccess(c, items)
}

func AddUpstreamAccount(c contract.Context) {
	var request upstreamAccountRequest
	if err := c.BindJSON(&request); err != nil {
		common.CtxApiError(c, err)
		return
	}
	account := &channelpkg.UpstreamAccount{}
	if err := applyUpstreamAccountRequest(account, request); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if request.Enabled == nil {
		account.Enabled = true
	}
	taken, err := channelpkg.IsUpstreamAccountHostTaken(account.Host, 0)
	if err != nil {
		common.SysLog(fmt.Sprintf("upstream account host check failed: %v", err))
		common.CtxApiErrorMsg(c, "failed to check upstream account host")
		return
	}
	if taken {
		common.CtxApiErrorMsg(c, fmt.Sprintf("an upstream account for %s already exists", account.Host))
		return
	}
	if err := account.Insert(); err != nil {
		common.SysLog(fmt.Sprintf("upstream account create failed: %v", err))
		common.CtxApiErrorMsg(c, "failed to create upstream account")
		return
	}
	common.CtxApiSuccess(c, account.ToResponse())
}

func UpdateUpstreamAccount(c contract.Context) {
	var request upstreamAccountRequest
	if err := c.BindJSON(&request); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if request.Id <= 0 {
		common.CtxApiErrorMsg(c, "missing upstream account id")
		return
	}
	account, err := channelpkg.GetUpstreamAccountById(request.Id)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if err := applyUpstreamAccountRequest(account, request); err != nil {
		common.CtxApiError(c, err)
		return
	}
	taken, err := channelpkg.IsUpstreamAccountHostTaken(account.Host, account.Id)
	if err != nil {
		common.SysLog(fmt.Sprintf("upstream account host check failed: id=%d: %v", account.Id, err))
		common.CtxApiErrorMsg(c, "failed to check upstream account host")
		return
	}
	if taken {
		common.CtxApiErrorMsg(c, fmt.Sprintf("an upstream account for %s already exists", account.Host))
		return
	}
	if err := account.Save(); err != nil {
		common.SysLog(fmt.Sprintf("upstream account update failed: id=%d: %v", account.Id, err))
		common.CtxApiErrorMsg(c, "failed to update upstream account")
		return
	}
	common.CtxApiSuccess(c, account.ToResponse())
}

func DeleteUpstreamAccount(c contract.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	account, err := channelpkg.GetUpstreamAccountById(id)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if err := account.Delete(); err != nil {
		common.SysLog(fmt.Sprintf("upstream account delete failed: id=%d: %v", id, err))
		common.CtxApiErrorMsg(c, "failed to delete upstream account")
		return
	}
	common.CtxApiSuccess(c, nil)
}

func TestUpstreamAccount(c contract.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	account, err := channelpkg.GetUpstreamAccountById(id)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	result := runUpstreamAccountTest(c.Context(), account)
	account.RecordTestResult(result.OK, result.Message)
	common.CtxApiSuccess(c, result)
}

// applyUpstreamAccountRequest 把请求合并到已有行上。空密钥与 __MASKED__
// 都表示保持原值，避免前端编辑其他字段时把已存的密码冲掉。
func applyUpstreamAccountRequest(account *channelpkg.UpstreamAccount, request upstreamAccountRequest) error {
	baseURL := strings.TrimRight(strings.TrimSpace(request.BaseURL), "/")
	host, err := channelpkg.NormalizeUpstreamAccountHost(baseURL)
	if err != nil {
		return err
	}
	kind, err := channelpkg.NormalizeUpstreamAccountKind(request.Kind)
	if err != nil {
		return err
	}

	account.BaseURL = baseURL
	account.Host = host
	account.Kind = kind
	account.Username = strings.TrimSpace(request.Username)
	account.JKGroupID = strings.TrimSpace(request.JKGroupID)
	if request.Enabled != nil {
		account.Enabled = *request.Enabled
	}

	if password := request.Password; password != "" && password != upstreamAccountMaskedSecret {
		account.Password = password
	}
	if accessToken := strings.TrimSpace(request.AccessToken); accessToken != "" && accessToken != upstreamAccountMaskedSecret {
		account.AccessToken = accessToken
		// 手工换了 token 时旧过期时间不再可信，按“未知”处理。
		account.TokenExpiresAt = 0
	}
	if refreshToken := strings.TrimSpace(request.RefreshToken); refreshToken != "" && refreshToken != upstreamAccountMaskedSecret {
		account.RefreshToken = refreshToken
	}
	return nil
}

func runUpstreamAccountTest(ctx context.Context, account *channelpkg.UpstreamAccount) upstreamAccountTestResult {
	result := upstreamAccountTestResult{
		Kind:     account.Kind,
		GroupIDs: []string{},
	}
	switch account.Kind {
	case channelpkg.UpstreamAccountKindSub2API:
		probe, err := perfsource.ProbeSub2APIAccount(ctx, account.BaseURL, account.Username, account.Password, account.AccessToken)
		if err != nil {
			result.Message = err.Error()
			return result
		}
		result.OK = true
		result.ModelCount = probe.ModelCount
		result.GroupIDs = probe.GroupIDs
		if probe.ModelCountAvailable {
			result.Message = fmt.Sprintf("ok: %d keys, %d groups, %d models", probe.KeyCount, len(probe.GroupIDs), probe.ModelCount)
		} else {
			result.Message = fmt.Sprintf("ok: %d keys, %d groups", probe.KeyCount, len(probe.GroupIDs))
		}
	case channelpkg.UpstreamAccountKindNewAPI:
		ok, modelCount, groupIDs, message := testNewAPIAccount(ctx, account.BaseURL)
		result.OK = ok
		result.ModelCount = modelCount
		result.GroupIDs = groupIDs
		result.Message = message
	default:
		result.Message = "unsupported upstream account kind"
	}
	return result
}

// testNewAPIAccount 只验证公开接口是否可达：new-api 的账号密码并不用于
// 匿名定价接口，所以不拿它去登录。pricing 优先，status 兜底。
func testNewAPIAccount(ctx context.Context, baseURL string) (bool, int, []string, string) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return false, 0, []string{}, "base url is empty"
	}

	pricingBody, pricingErr := upstreamfetch.Get(ctx, base+"/api/pricing", upstreamfetch.DefaultTimeout, 8<<20, nil)
	if pricingErr == nil {
		var pricing struct {
			Data       []any              `json:"data"`
			GroupRatio map[string]float64 `json:"group_ratio"`
		}
		if err := common.Unmarshal(pricingBody, &pricing); err == nil {
			groups := make([]string, 0, len(pricing.GroupRatio))
			for group := range pricing.GroupRatio {
				groups = append(groups, group)
			}
			sort.Strings(groups)
			return true, len(pricing.Data), groups, fmt.Sprintf("ok: pricing reachable, %d models", len(pricing.Data))
		}
		pricingErr = fmt.Errorf("unparseable pricing response")
	}

	statusBody, statusErr := upstreamfetch.Get(ctx, base+"/api/status", upstreamfetch.DefaultTimeout, 1<<20, nil)
	if statusErr == nil {
		var status struct {
			Success bool `json:"success"`
		}
		if err := common.Unmarshal(statusBody, &status); err == nil {
			return true, 0, []string{}, "ok: status reachable"
		}
		statusErr = fmt.Errorf("unparseable status response")
	}
	return false, 0, []string{}, fmt.Sprintf("pricing: %v; status: %v", pricingErr, statusErr)
}
