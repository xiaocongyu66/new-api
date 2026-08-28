package identity

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/i18n"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/internal/identity/manage_tokens"
)

type tokenAutoGroupsInput struct {
	Set    bool
	Groups []string
}

func (input *tokenAutoGroupsInput) UnmarshalJSON(data []byte) error {
	input.Set = true
	if strings.TrimSpace(string(data)) == "null" {
		input.Groups = nil
		return nil
	}
	return common.Unmarshal(data, &input.Groups)
}

type tokenRequest struct {
	Token
	AutoGroups tokenAutoGroupsInput `json:"auto_groups"`
}

type tokenResponse struct {
	*Token
	AutoGroups []string `json:"auto_groups"`
}

func buildMaskedTokenResponse(token *Token) *tokenResponse {
	if token == nil {
		return nil
	}
	maskedToken := *token
	maskedToken.Key = token.GetMaskedKey()
	autoGroups, err := token.GetAutoGroups()
	if err != nil {
		common.SysError(fmt.Sprintf("failed to parse auto groups for token %d: %v", token.Id, err))
		autoGroups = nil
	}
	if len(autoGroups) == 0 {
		autoGroups = nil
	}
	return &tokenResponse{Token: &maskedToken, AutoGroups: autoGroups}
}

func buildMaskedTokenResponses(tokens []*Token) []*tokenResponse {
	maskedTokens := make([]*tokenResponse, 0, len(tokens))
	for _, token := range tokens {
		maskedTokens = append(maskedTokens, buildMaskedTokenResponse(token))
	}
	return maskedTokens
}

func getTokenRequestUserGroup(c contract.Context) (string, error) {
	if userGroup := common.GetCtxKeyString(c, constant.ContextKeyUserGroup); userGroup != "" {
		return userGroup, nil
	}
	if userGroup := c.GetString("group"); userGroup != "" {
		return userGroup, nil
	}
	return GetUserGroup(c.GetInt("id"), false)
}

func setTokenAutoGroups(c contract.Context, token *Token, groups []string) bool {
	if len(groups) == 0 {
		if err := token.SetAutoGroups(nil); err != nil {
			common.CtxApiError(c, err)
			return false
		}
		return true
	}

	maxCount := setting.GetMaxTokenAutoGroups()
	if len(groups) > maxCount {
		common.CtxApiErrorI18n(c, i18n.MsgTokenAutoGroupsTooMany, map[string]any{"Max": maxCount})
		return false
	}

	userGroup, err := getTokenRequestUserGroup(c)
	if err != nil {
		common.CtxApiError(c, err)
		return false
	}
	seen := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		if _, ok := seen[group]; ok {
			common.CtxApiErrorI18n(c, i18n.MsgTokenAutoGroupsDuplicate, map[string]any{"Group": group})
			return false
		}
		seen[group] = struct{}{}
		if !setting.IsUserSelectableGroup(userGroup, group) {
			common.CtxApiErrorI18n(c, i18n.MsgTokenAutoGroupsInvalid, map[string]any{"Group": group})
			return false
		}
	}

	if err := token.SetAutoGroups(groups); err != nil {
		common.CtxApiError(c, err)
		return false
	}
	return true
}

func GetAllTokens(c contract.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	tokens, err := GetAllUserTokens(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	total, _ := CountUserTokens(userId)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(buildMaskedTokenResponses(tokens))
	common.CtxApiSuccess(c, pageInfo)
}

func SearchTokens(c contract.Context) {
	userId := c.GetInt("id")
	keyword := c.Query("keyword")
	token := c.Query("token")

	pageInfo := common.GetPageQuery(c)

	tokens, total, err := SearchUserTokens(userId, keyword, token, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(buildMaskedTokenResponses(tokens))
	common.CtxApiSuccess(c, pageInfo)
}

func GetToken(c contract.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	token, err := GetTokenByIds(id, userId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, buildMaskedTokenResponse(token))
}

func GetTokenAutoGroups(c contract.Context) {
	userGroup, err := getTokenRequestUserGroup(c)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, common.H{
		"groups":    setting.GetUserAutoGroup(userGroup),
		"max_count": setting.GetMaxTokenAutoGroups(),
	})
}

func GetTokenKey(c contract.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	token, err := GetTokenByIds(id, userId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, common.H{
		"key": token.GetFullKey(),
	})
}

func GetTokenStatus(c contract.Context) {
	tokenId := c.GetInt("token_id")
	userId := c.GetInt("id")
	token, err := GetTokenByIds(tokenId, userId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}
	_ = c.JSON(http.StatusOK, common.H{
		"object":          "credit_summary",
		"total_granted":   token.RemainQuota,
		"total_used":      0, // not supported currently
		"total_available": token.RemainQuota,
		"expires_at":      expiredAt * 1000,
	})
}

func GetTokenUsage(c contract.Context) {
	authHeader := c.Header("Authorization")
	if authHeader == "" {
		_ = c.JSON(http.StatusUnauthorized, common.H{
			"success": false,
			"message": "No Authorization header",
		})
		return
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		_ = c.JSON(http.StatusUnauthorized, common.H{
			"success": false,
			"message": "Invalid Bearer token",
		})
		return
	}
	tokenKey := parts[1]

	token, err := GetTokenByKey(strings.TrimPrefix(tokenKey, "sk-"), false)
	if err != nil {
		common.SysError("failed to get token by key: " + err.Error())
		common.CtxApiErrorI18n(c, i18n.MsgTokenGetInfoFailed)
		return
	}

	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}

	_ = c.JSON(http.StatusOK, common.H{
		"code":    true,
		"message": "ok",
		"data": common.H{
			"object":               "token_usage",
			"name":                 token.Name,
			"total_granted":        token.RemainQuota + token.UsedQuota,
			"total_used":           token.UsedQuota,
			"total_available":      token.RemainQuota,
			"unlimited_quota":      token.UnlimitedQuota,
			"model_limits":         token.GetModelLimitsMap(),
			"model_limits_enabled": token.ModelLimitsEnabled,
			"expires_at":           expiredAt,
		},
	})
}

func AddToken(c contract.Context) {
	request := tokenRequest{}
	err := c.BindJSON(&request)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	token := request.Token
	if len(token.Name) > 50 {
		common.CtxApiErrorI18n(c, i18n.MsgTokenNameTooLong)
		return
	}
	// 非无限额度时，检查额度值是否超出有效范围
	if !token.UnlimitedQuota {
		if token.RemainQuota < 0 {
			common.CtxApiErrorI18n(c, i18n.MsgTokenQuotaNegative)
			return
		}
		maxQuotaValue := common.QuotaFromFloat(1000000000 * common.QuotaPerUnit)
		if token.RemainQuota > maxQuotaValue {
			common.CtxApiErrorI18n(c, i18n.MsgTokenQuotaExceedMax, map[string]any{"Max": maxQuotaValue})
			return
		}
	}
	// 检查用户令牌数量是否已达上限
	maxTokens := manage_tokens.GetMaxUserTokens()
	count, err := CountUserTokens(c.GetInt("id"))
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if int(count) >= maxTokens {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": fmt.Sprintf("已达到最大令牌数量限制 (%d)", maxTokens),
		})
		return
	}
	if token.Group == "auto" {
		if !setTokenAutoGroups(c, &token, request.AutoGroups.Groups) {
			return
		}
	} else {
		token.CrossGroupRetry = false
		_ = token.SetAutoGroups(nil)
	}
	key, err := common.GenerateKey()
	if err != nil {
		common.CtxApiErrorI18n(c, i18n.MsgTokenGenerateFailed)
		common.SysLog("failed to generate token key: " + err.Error())
		return
	}
	cleanToken := Token{
		UserId:             c.GetInt("id"),
		Name:               token.Name,
		Key:                key,
		CreatedTime:        common.GetTimestamp(),
		AccessedTime:       common.GetTimestamp(),
		ExpiredTime:        token.ExpiredTime,
		RemainQuota:        token.RemainQuota,
		UnlimitedQuota:     token.UnlimitedQuota,
		ModelLimitsEnabled: token.ModelLimitsEnabled,
		ModelLimits:        token.ModelLimits,
		AllowIps:           token.AllowIps,
		Group:              token.Group,
		CrossGroupRetry:    token.CrossGroupRetry,
		AutoGroups:         token.AutoGroups,
	}
	err = cleanToken.Insert()
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
	})
}

func DeleteToken(c contract.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	err := DeleteTokenById(id, userId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
	})
}

func UpdateToken(c contract.Context) {
	userId := c.GetInt("id")
	statusOnly := c.Query("status_only")
	request := tokenRequest{}
	err := c.BindJSON(&request)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	token := request.Token
	if len(token.Name) > 50 {
		common.CtxApiErrorI18n(c, i18n.MsgTokenNameTooLong)
		return
	}
	if !token.UnlimitedQuota {
		if token.RemainQuota < 0 {
			common.CtxApiErrorI18n(c, i18n.MsgTokenQuotaNegative)
			return
		}
		maxQuotaValue := common.QuotaFromFloat(1000000000 * common.QuotaPerUnit)
		if token.RemainQuota > maxQuotaValue {
			common.CtxApiErrorI18n(c, i18n.MsgTokenQuotaExceedMax, map[string]any{"Max": maxQuotaValue})
			return
		}
	}
	cleanToken, err := GetTokenByIds(token.Id, userId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if token.Status == common.TokenStatusEnabled {
		if cleanToken.Status == common.TokenStatusExpired && cleanToken.ExpiredTime <= common.GetTimestamp() && cleanToken.ExpiredTime != -1 {
			common.CtxApiErrorI18n(c, i18n.MsgTokenExpiredCannotEnable)
			return
		}
		if cleanToken.Status == common.TokenStatusExhausted && cleanToken.RemainQuota <= 0 && !cleanToken.UnlimitedQuota {
			common.CtxApiErrorI18n(c, i18n.MsgTokenExhaustedCannotEable)
			return
		}
	}
	if statusOnly != "" {
		cleanToken.Status = token.Status
	} else {
		// If you add more fields, please also update token.Update()
		cleanToken.Name = token.Name
		cleanToken.ExpiredTime = token.ExpiredTime
		cleanToken.RemainQuota = token.RemainQuota
		cleanToken.UnlimitedQuota = token.UnlimitedQuota
		cleanToken.ModelLimitsEnabled = token.ModelLimitsEnabled
		cleanToken.ModelLimits = token.ModelLimits
		cleanToken.AllowIps = token.AllowIps
		cleanToken.Group = token.Group
		cleanToken.CrossGroupRetry = token.CrossGroupRetry
		if token.Group != "auto" {
			cleanToken.CrossGroupRetry = false
			_ = cleanToken.SetAutoGroups(nil)
		} else if request.AutoGroups.Set {
			if !setTokenAutoGroups(c, cleanToken, request.AutoGroups.Groups) {
				return
			}
		}
	}
	err = cleanToken.Update()
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    buildMaskedTokenResponse(cleanToken),
	})
}

type TokenBatch struct {
	Ids []int `json:"ids"`
}

func DeleteTokenBatch(c contract.Context) {
	tokenBatch := TokenBatch{}
	if err := c.BindJSON(&tokenBatch); err != nil || len(tokenBatch.Ids) == 0 {
		common.CtxApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	count, err := BatchDeleteTokens(tokenBatch.Ids, userId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    count,
	})
}

func GetTokenKeysBatch(c contract.Context) {
	tokenBatch := TokenBatch{}
	if err := c.BindJSON(&tokenBatch); err != nil || len(tokenBatch.Ids) == 0 {
		common.CtxApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(tokenBatch.Ids) > 100 {
		common.CtxApiErrorI18n(c, i18n.MsgBatchTooMany, map[string]any{"Max": 100})
		return
	}
	userId := c.GetInt("id")
	tokens, err := GetTokenKeysByIds(tokenBatch.Ids, userId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	keysMap := make(map[int]string)
	for _, t := range tokens {
		keysMap[t.Id] = t.GetFullKey()
	}
	common.CtxApiSuccess(c, common.H{"keys": keysMap})
}
