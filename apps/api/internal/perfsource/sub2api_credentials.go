package perfsource

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// sub2api 凭据解析：渠道 settings 优先，其次按 base_url 的 host 复用
// upstream_accounts 里的全局账号。
//
// 渠道只要配置了任意一项凭据就整份采用渠道的，不与账号表逐字段混用：
// 续期得到的新 token 必须写回它来自的那一份，混用会把 token 存到错误的
// 归属上，下一次取数就会拿到别人账号的会话。
type sub2apiCredentialSource int

const (
	sub2apiCredentialSourceNone sub2apiCredentialSource = iota
	sub2apiCredentialSourceChannel
	sub2apiCredentialSourceAccount
)

type sub2apiCredentialSet struct {
	BaseURL        string
	AccessToken    string
	RefreshToken   string
	Username       string
	Password       string
	TokenExpiresAt int64
	Source         sub2apiCredentialSource
	AccountId      int
}

func sub2apiCredentials(channel *channelpkg.Channel) (sub2apiCredentialSet, error) {
	if channel == nil {
		return sub2apiCredentialSet{}, fmt.Errorf("sub2api channel is nil")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")

	// 鉴权与取数必须打同一个 origin。用户的 base_url 常常带着面板路径
	// （https://host/v1 之类），而上游的 /api/v1/* 永远挂在根路径上；
	// 两边各算各的就会出现「登录成功、取数 404」。
	origin := baseURL
	if resolved, err := channelpkg.UpstreamAccountOrigin(baseURL); err == nil {
		origin = resolved
	}

	settings := channel.GetOtherSettings()
	if sub2apiChannelHasCredentials(settings) {
		return sub2apiCredentialSet{
			BaseURL:        origin,
			AccessToken:    strings.TrimSpace(settings.Sub2APIAccessToken),
			RefreshToken:   strings.TrimSpace(settings.Sub2APIRefreshToken),
			Username:       strings.TrimSpace(settings.Sub2APIUsername),
			Password:       settings.Sub2APIPassword,
			TokenExpiresAt: settings.Sub2APITokenExpiresAt,
			Source:         sub2apiCredentialSourceChannel,
		}, nil
	}

	host, err := channelpkg.NormalizeUpstreamAccountHost(baseURL)
	if err != nil {
		return sub2apiCredentialSet{}, fmt.Errorf("sub2api channel has no usable base url: %w", err)
	}
	account, err := channelpkg.FindUpstreamAccountByHost(host)
	if err != nil {
		return sub2apiCredentialSet{}, fmt.Errorf("sub2api upstream account lookup failed for %s: %w", host, err)
	}
	if account == nil || account.Kind != channelpkg.UpstreamAccountKindSub2API {
		return sub2apiCredentialSet{}, fmt.Errorf("sub2api credentials not configured: need an access token, refresh token, or username/password")
	}

	accountBaseURL, originErr := channelpkg.UpstreamAccountOrigin(account.BaseURL)
	if originErr != nil {
		accountBaseURL = origin
	}
	return sub2apiCredentialSet{
		BaseURL:        accountBaseURL,
		AccessToken:    strings.TrimSpace(account.AccessToken),
		RefreshToken:   strings.TrimSpace(account.RefreshToken),
		Username:       strings.TrimSpace(account.Username),
		Password:       account.Password,
		TokenExpiresAt: account.TokenExpiresAt,
		Source:         sub2apiCredentialSourceAccount,
		AccountId:      account.Id,
	}, nil
}

// sub2apiChannelHasCredentials 判断渠道是否“自己配了凭据”。用户名和密码
// 必须成对出现：只填用户名通常是编辑到一半，应继续用账号表，而不是拿
// 半个渠道账号把登录打到上游。
func sub2apiChannelHasCredentials(settings dto.ChannelOtherSettings) bool {
	if strings.TrimSpace(settings.Sub2APIAccessToken) != "" ||
		strings.TrimSpace(settings.Sub2APIRefreshToken) != "" {
		return true
	}
	return strings.TrimSpace(settings.Sub2APIUsername) != "" && settings.Sub2APIPassword != ""
}

// Sub2APIAccountProbe 是「测试连接」对 sub2api 账号的观测结果。
// 只暴露数量与分组 ID，不携带任何 token 或响应体。
type Sub2APIAccountProbe struct {
	KeyCount   int
	ModelCount int
	GroupIDs   []string
	// ModelCountAvailable 为 false 表示登录与密钥读取成功，但模型广场不可用，
	// 此时 ModelCount 没有意义，调用方不应把它当作 0 个模型展示。
	ModelCountAvailable bool
}

// ProbeSub2APIAccount 用该账号的凭据真实请求一次，再读密钥分组与公开模型
// 广场。用户名密码优先；只配置了 access token 的账号直接用 token，否则
// 前端「测试连接」对这类账号永远报错。它只用于管理端测试，不参与探活取数。
func ProbeSub2APIAccount(ctx context.Context, baseURL, username, password, accessToken string) (Sub2APIAccountProbe, error) {
	base, err := channelpkg.UpstreamAccountOrigin(baseURL)
	if err != nil {
		return Sub2APIAccountProbe{}, err
	}
	bearer := strings.TrimSpace(accessToken)
	if strings.TrimSpace(username) != "" && password != "" {
		token, err := sub2apiLogin(ctx, base, username, password)
		if err != nil {
			return Sub2APIAccountProbe{}, fmt.Errorf("login failed: %w", err)
		}
		bearer = token.AccessToken
	} else if bearer == "" {
		return Sub2APIAccountProbe{}, fmt.Errorf("username/password or access token is required for the test")
	}

	probe := Sub2APIAccountProbe{GroupIDs: []string{}}
	groupSet := make(map[string]struct{})
	const keysPageSize = 50
	for page := 1; page <= 10; page++ {
		target := fmt.Sprintf("%s/api/v1/keys?page=%d&page_size=%d", base, page, keysPageSize)
		body, err := getWithBearer(ctx, target, bearer)
		if err != nil {
			return probe, fmt.Errorf("list keys failed: %w", err)
		}
		var pageDoc usagePage[sub2apiKeyItem]
		if err := common.Unmarshal(body, &pageDoc); err != nil {
			return probe, fmt.Errorf("unparseable keys response: %w", err)
		}
		probe.KeyCount += len(pageDoc.Data.Items)
		for _, item := range pageDoc.Data.Items {
			if groupID := strings.TrimSpace(string(item.GroupID)); groupID != "" {
				groupSet[groupID] = struct{}{}
			}
		}
		if len(pageDoc.Data.Items) == 0 {
			break
		}
		lastPage := false
		switch {
		case pageDoc.Data.Pages > 0:
			// 上游给了总页数，按它停。
			lastPage = page >= pageDoc.Data.Pages
		case pageDoc.Data.Total > 0:
			// 有的版本只回 total，用累计条数判断是否已取完。
			lastPage = int64(probe.KeyCount) >= pageDoc.Data.Total
		default:
			// 既没有 pages 也没有 total 时，短页即为最后一页。
			lastPage = len(pageDoc.Data.Items) < keysPageSize
		}
		if lastPage {
			break
		}
	}
	for groupID := range groupSet {
		probe.GroupIDs = append(probe.GroupIDs, groupID)
	}
	sortGroupIDs(probe.GroupIDs)

	if count, ok := countSub2APIPlazaModels(ctx, base, bearer, groupSet); ok {
		probe.ModelCount = count
		probe.ModelCountAvailable = true
	}
	return probe, nil
}

// sub2apiGroupID 兼容 group_id 既可能是数字也可能是字符串的上游版本。
type sub2apiGroupID string

func (id *sub2apiGroupID) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "null" || text == `""` {
		*id = ""
		return nil
	}
	*id = sub2apiGroupID(strings.Trim(text, `"`))
	return nil
}

type sub2apiKeyItem struct {
	GroupID sub2apiGroupID `json:"group_id"`
}

type sub2apiPlaza struct {
	Data struct {
		Groups []struct {
			Id     int64 `json:"id"`
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		} `json:"groups"`
	} `json:"data"`
}

// countSub2APIPlazaModels 统计账号所属分组的模型数。分组来自账号的密钥；
// 一个密钥都没有时退回全站模型数，至少让“这个上游有多少模型”可见。
// 模型广场失败不视为登录失败：测试连接的核心是凭据是否可用。
func countSub2APIPlazaModels(ctx context.Context, base, token string, groupSet map[string]struct{}) (int, bool) {
	body, err := getWithBearer(ctx, base+"/api/v1/model-plaza", token)
	if err != nil {
		return 0, false
	}
	var plaza sub2apiPlaza
	if err := common.Unmarshal(body, &plaza); err != nil || len(plaza.Data.Groups) == 0 {
		return 0, false
	}

	models := make(map[string]struct{})
	matchedGroup := false
	for _, group := range plaza.Data.Groups {
		if len(groupSet) > 0 {
			if _, ok := groupSet[strconv.FormatInt(group.Id, 10)]; !ok {
				continue
			}
			matchedGroup = true
		}
		for _, model := range group.Models {
			if name := strings.TrimSpace(model.Name); name != "" {
				models[name] = struct{}{}
			}
		}
	}
	// 密钥分组不在公开广场里（私有组）时无法按组统计，此时不给数字，
	// 让调用方显示“模型数不可用”，而不是误报 0。
	if len(groupSet) > 0 && !matchedGroup {
		return 0, false
	}
	return len(models), true
}

func sortGroupIDs(ids []string) {
	sort.Slice(ids, func(i, j int) bool {
		left, leftErr := strconv.Atoi(ids[i])
		right, rightErr := strconv.Atoi(ids[j])
		if leftErr == nil && rightErr == nil {
			return left < right
		}
		return ids[i] < ids[j]
	})
}
