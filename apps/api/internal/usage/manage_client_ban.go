package usage

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/i18n"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage/insight"
)

// clientBanRequest 是"封禁 / 解封客户端"的入参，全局档与单用户档共用。
type clientBanRequest struct {
	Client string `json:"client"`
	Ban    bool   `json:"ban"`
}

// decodeClientBanRequest 解析并归一化请求体，返回可直接使用的客户端 ID。
// 请求体必须为 JSON 且 client 非空，否则按非法参数处理。
func decodeClientBanRequest(c contract.Context) (clientBanRequest, error) {
	rawBody, err := c.RawBody()
	if err != nil {
		return clientBanRequest{}, err
	}
	var req clientBanRequest
	if err := common.Unmarshal(rawBody, &req); err != nil {
		return clientBanRequest{}, err
	}
	req.Client, err = SanitizeClientID(req.Client)
	if err != nil {
		return clientBanRequest{}, err
	}
	return req, nil
}

// HandleGetClientCatalog 返回封禁选项目录：内置识别规则 + 从真实流量里
// 自动发现的客户端，各自带累计请求数与用户数。
//
// 之所以要把观测数据并进来：内置规则是发布期常量，新工具只会以
// 自动发现标识（ua:xxx）出现在画像里，不并进目录就永远无法勾选封禁。
// 请求量同时给运营方判断依据——封一个日均几万请求的客户端和封一个
// 只出现过 3 次的客户端，风险完全不同。
func HandleGetClientCatalog(c contract.Context) {
	entries := insight.ClientCatalog()
	views := make([]clientCatalogView, 0, len(entries))
	seen := make(map[string]int, len(entries))
	for _, entry := range entries {
		seen[entry.ID] = len(views)
		views = append(views, clientCatalogView{
			ID: entry.ID, Name: entry.Name, Kind: entry.Kind, BuiltIn: true,
		})
	}

	// 观测数据来自画像聚合行的 clients_json，是全量累计值（非采样）。
	observed, err := AggregateClientUsage()
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	for _, stat := range observed {
		if index, ok := seen[stat.Client]; ok {
			views[index].Requests = stat.Requests
			views[index].Users = stat.Users
			continue
		}
		// 画像里出现过但目录里没有：自动发现的客户端，或规则被删后
		// 遗留的历史 ID。两者都要能勾选——后者仍可能有存量流量。
		views = append(views, clientCatalogView{
			ID:       stat.Client,
			Name:     insight.DiscoveredClientName(stat.Client),
			Kind:     insight.KindDiscovered,
			Requests: stat.Requests,
			Users:    stat.Users,
		})
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    views,
	})
}

// clientCatalogView 是封禁选择器的一个选项。
type clientCatalogView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	// BuiltIn 区分"内置识别规则"与"从流量里自动发现"，前端分组展示。
	BuiltIn bool `json:"built_in"`
	// Requests / Users 是累计观测量，0 表示站内还没见过这个客户端。
	Requests int64 `json:"requests"`
	Users    int   `json:"users"`
}

// HandleSetGlobalClientBan 封禁 / 解封一个客户端（全站生效）。
// 存到 user_insight_setting.blocked_clients，随标准 option 写路径落库。
func HandleSetGlobalClientBan(c contract.Context) {
	req, err := decodeClientBanRequest(c)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := SetGlobalClientBan(req.Client, req.Ban); err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.SysLog("insight global client ban: operator=" + strconv.Itoa(c.GetInt("id")) +
		" client=" + req.Client + " ban=" + strconv.FormatBool(req.Ban))
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    common.H{"client": req.Client, "ban": req.Ban, "blocked_clients": GetBlockedClientList()},
	})
}

// HandleToggleUserClientBan 封禁 / 解封某用户的某个客户端（仅该用户生效）。
// 写 user_insight_client_bans 表，不影响该用户的其它客户端。
func HandleToggleUserClientBan(c contract.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	req, err := decodeClientBanRequest(c)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := ToggleUserClientBan(userId, req.Client, req.Ban); err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.SysLog("insight user client ban: operator=" + strconv.Itoa(c.GetInt("id")) +
		" user=" + strconv.Itoa(userId) + " client=" + req.Client + " ban=" + strconv.FormatBool(req.Ban))
	disabled := GetUserClientBansByUsers([]int{userId})[userId]
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    common.H{"user_id": userId, "client": req.Client, "ban": req.Ban, "disabled_clients": disabled},
	})
}
