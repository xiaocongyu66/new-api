package handler

import (
	"strconv"
	"strings"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// jailbreakProfilePayload is the admin API view of a profile: the persisted
// fields plus the flattened channel/model bindings the UI edits together.
type jailbreakProfilePayload struct {
	Id            int      `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Kind          string   `json:"kind"`
	Prompt        string   `json:"prompt"`
	Headers       string   `json:"headers"`
	ChannelModels string   `json:"channel_models"`
	Mode          string   `json:"mode"`
	Priority      int      `json:"priority"`
	Force         bool     `json:"force"`
	Enabled       bool     `json:"enabled"`
	Channels      []int    `json:"channels"`
	Models        []string `json:"models"`
	CreatedTime   int64    `json:"created_time"`
	UpdatedTime   int64    `json:"updated_time"`
}

func GetJailbreakProfiles(c contract.Context) {
	profiles, err := channelpkg.ListJailbreakProfiles()
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	ids := make([]int, 0, len(profiles))
	for _, profile := range profiles {
		ids = append(ids, profile.Id)
	}
	bindings, err := channelpkg.ListJailbreakBindingsByProfileIDs(ids)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	channels := make(map[int][]int, len(profiles))
	models := make(map[int][]string, len(profiles))
	for _, binding := range bindings {
		switch binding.TargetType {
		case channelpkg.JailbreakTargetChannel:
			id, err := strconv.Atoi(binding.TargetKey)
			if err != nil || id <= 0 {
				continue
			}
			channels[binding.ProfileId] = append(channels[binding.ProfileId], id)
		case channelpkg.JailbreakTargetModel:
			models[binding.ProfileId] = append(models[binding.ProfileId], binding.TargetKey)
		}
	}
	items := make([]jailbreakProfilePayload, 0, len(profiles))
	for _, profile := range profiles {
		profileChannels := channels[profile.Id]
		if profileChannels == nil {
			profileChannels = []int{}
		}
		profileModels := models[profile.Id]
		if profileModels == nil {
			profileModels = []string{}
		}
		kind := profile.Kind
		if kind == "" {
			kind = channelpkg.JailbreakKindPrompt
		}
		items = append(items, jailbreakProfilePayload{
			Id:            profile.Id,
			Name:          profile.Name,
			Description:   profile.Description,
			Kind:          kind,
			Prompt:        profile.Prompt,
			Headers:       profile.Headers,
			ChannelModels: profile.ChannelModels,
			Mode:          profile.Mode,
			Priority:      profile.Priority,
			Force:         profile.Force,
			Enabled:       profile.Enabled,
			Channels:      profileChannels,
			Models:        profileModels,
			CreatedTime:   profile.CreatedTime,
			UpdatedTime:   profile.UpdatedTime,
		})
	}
	common.CtxApiSuccess(c, items)
}

func CreateJailbreakProfile(c contract.Context) {
	var payload jailbreakProfilePayload
	if err := c.BindJSON(&payload); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if msg := validateJailbreakPayload(&payload); msg != "" {
		common.CtxApiErrorMsg(c, msg)
		return
	}
	if dup, err := channelpkg.IsJailbreakNameDuplicated(0, payload.Name); err != nil {
		common.CtxApiError(c, err)
		return
	} else if dup {
		common.CtxApiErrorMsg(c, "规则名称已存在")
		return
	}
	if payload.Priority == 0 {
		payload.Priority = 100
	}
	profile := channelpkg.JailbreakProfile{
		Name:          payload.Name,
		Description:   payload.Description,
		Kind:          payload.Kind,
		Prompt:        payload.Prompt,
		Headers:       payload.Headers,
		ChannelModels: payload.ChannelModels,
		Mode:          payload.Mode,
		Priority:      payload.Priority,
		Force:         payload.Force,
		Enabled:       payload.Enabled,
	}
	if err := channelpkg.CreateJailbreakProfile(&profile, payload.Channels, payload.Models); err != nil {
		common.CtxApiError(c, err)
		return
	}
	payload.Id = profile.Id
	payload.CreatedTime = profile.CreatedTime
	payload.UpdatedTime = profile.UpdatedTime
	common.CtxApiSuccess(c, payload)
}

func UpdateJailbreakProfile(c contract.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.CtxApiErrorMsg(c, "缺少规则 ID")
		return
	}
	var payload jailbreakProfilePayload
	if err := c.BindJSON(&payload); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if msg := validateJailbreakPayload(&payload); msg != "" {
		common.CtxApiErrorMsg(c, msg)
		return
	}
	if dup, err := channelpkg.IsJailbreakNameDuplicated(id, payload.Name); err != nil {
		common.CtxApiError(c, err)
		return
	} else if dup {
		common.CtxApiErrorMsg(c, "规则名称已存在")
		return
	}
	profile, err := channelpkg.GetJailbreakProfileById(id)
	if err != nil {
		common.CtxApiErrorMsg(c, "规则不存在")
		return
	}
	profile.Name = payload.Name
	profile.Description = payload.Description
	profile.Kind = payload.Kind
	profile.Prompt = payload.Prompt
	profile.Headers = payload.Headers
	profile.ChannelModels = payload.ChannelModels
	profile.Mode = payload.Mode
	if payload.Priority == 0 {
		payload.Priority = 100
	}
	profile.Priority = payload.Priority
	profile.Force = payload.Force
	profile.Enabled = payload.Enabled
	if err := channelpkg.UpdateJailbreakProfile(profile, payload.Channels, payload.Models); err != nil {
		common.CtxApiError(c, err)
		return
	}
	payload.Id = profile.Id
	payload.CreatedTime = profile.CreatedTime
	payload.UpdatedTime = profile.UpdatedTime
	common.CtxApiSuccess(c, payload)
}

func DeleteJailbreakProfile(c contract.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.CtxApiErrorMsg(c, "缺少规则 ID")
		return
	}
	if err := channelpkg.DeleteJailbreakProfileById(id); err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, nil)
}

// validateJailbreakPayload normalizes the payload in place and returns a
// user-facing error message; empty means valid.
func validateJailbreakPayload(payload *jailbreakProfilePayload) string {
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Description = strings.TrimSpace(payload.Description)
	payload.Kind = strings.TrimSpace(payload.Kind)
	payload.Prompt = strings.TrimSpace(payload.Prompt)
	payload.Mode = strings.TrimSpace(payload.Mode)
	payload.Headers = strings.TrimSpace(payload.Headers)
	payload.ChannelModels = strings.TrimSpace(payload.ChannelModels)
	if payload.ChannelModels == "" {
		payload.ChannelModels = "{}"
	}
	var channelModels map[string][]string
	if err := common.Unmarshal([]byte(payload.ChannelModels), &channelModels); err != nil {
		return "渠道模型绑定必须是合法的 JSON 对象"
	}
	normalized := make(map[string][]string, len(channelModels))
	for key, names := range channelModels {
		id, err := strconv.Atoi(strings.TrimSpace(key))
		if err != nil || id <= 0 {
			continue
		}
		seen := make(map[string]struct{}, len(names))
		clean := make([]string, 0, len(names))
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			clean = append(clean, name)
		}
		if len(clean) > 0 {
			normalized[strconv.Itoa(id)] = clean
		}
	}
	encoded, err := common.Marshal(normalized)
	if err != nil {
		return "渠道模型绑定序列化失败"
	}
	payload.ChannelModels = string(encoded)
	if payload.Name == "" {
		return "规则名称不能为空"
	}
	if len([]rune(payload.Name)) > 64 {
		return "规则名称不能超过 64 个字符"
	}
	if payload.Kind == "" {
		payload.Kind = channelpkg.JailbreakKindPrompt
	}
	switch payload.Kind {
	case channelpkg.JailbreakKindPrompt:
		if payload.Prompt == "" {
			return "系统提示词不能为空"
		}
		switch payload.Mode {
		case channelpkg.JailbreakModePrepend, channelpkg.JailbreakModeAppend, channelpkg.JailbreakModeReplace:
		default:
			return "注入方式不合法"
		}
	case channelpkg.JailbreakKindHeader:
		if payload.Headers == "" {
			return "请求头内容不能为空"
		}
		var headers map[string]interface{}
		if err := common.Unmarshal([]byte(payload.Headers), &headers); err != nil {
			return "请求头必须是合法的 JSON 对象"
		}
		if len(headers) == 0 {
			return "请求头内容不能为空"
		}
	default:
		return "规则类型不合法"
	}
	return ""
}

// jailbreakPreviewPayload simulates one relay resolution: the caller supplies
// the target channel/model, the base prompt layer, and an optional user test
// message. The response shows exactly what the gate would inject.
type jailbreakPreviewPayload struct {
	Kind          string `json:"kind"`
	ChannelID     int    `json:"channel_id"`
	Model         string `json:"model"`
	BasePrompt    string `json:"base_prompt"`
	BaseSource    string `json:"base_source"`
	BaseOverride  bool   `json:"base_override"`
	UserMessage   string `json:"user_message"`
	ExtraKind     string `json:"extra_kind"`
	ExtraMode     string `json:"extra_mode"`
	ExtraPrompt   string `json:"extra_prompt"`
	ExtraHeaders  string `json:"extra_headers"`
	ExtraForce    bool   `json:"extra_force"`
	ExtraName     string `json:"extra_name"`
	ExcludeRuleID int    `json:"exclude_rule_id"`
}

func PreviewJailbreakPrompt(c contract.Context) {
	var payload jailbreakPreviewPayload
	if err := c.BindJSON(&payload); err != nil {
		common.CtxApiError(c, err)
		return
	}
	model := strings.TrimSpace(payload.Model)
	if model == "" {
		common.CtxApiErrorMsg(c, "模型不能为空")
		return
	}
	if payload.BaseSource != "" &&
		payload.BaseSource != relaycommon.SystemPromptSourceChannel &&
		payload.BaseSource != relaycommon.SystemPromptSourceToken {
		common.CtxApiErrorMsg(c, "基础提示词来源不合法")
		return
	}

	if payload.Kind == channelpkg.JailbreakKindHeader {
		extraHeaders := map[string]interface{}{}
		if strings.TrimSpace(payload.ExtraHeaders) != "" {
			if err := common.Unmarshal([]byte(payload.ExtraHeaders), &extraHeaders); err != nil {
				common.CtxApiErrorMsg(c, "请求头必须是合法的 JSON 对象")
				return
			}
		}
		var baseHeaders map[string]interface{}
		if payload.ChannelID > 0 {
			channel, err := channelpkg.CacheGetChannel(payload.ChannelID)
			if err != nil {
				channel, err = channelpkg.GetChannelById(payload.ChannelID, true)
			}
			if err == nil && channel != nil {
				baseHeaders = channel.GetHeaderOverride()
			}
		}
		headers, applied := relaycommon.PreviewEffectiveHeaders(payload.ChannelID, model, baseHeaders, extraHeaders, payload.ExtraName, payload.ExcludeRuleID)
		messages := make([]map[string]string, 0, 1)
		if message := strings.TrimSpace(payload.UserMessage); message != "" {
			messages = append(messages, map[string]string{"role": "user", "content": message})
		}
		common.CtxApiSuccess(c, common.H{
			"kind":     channelpkg.JailbreakKindHeader,
			"rules":    applied,
			"headers":  headers,
			"override": false,
			"messages": messages,
		})
		return
	}

	var extraRule *channelpkg.JailbreakRule
	if strings.TrimSpace(payload.ExtraPrompt) != "" && payload.ExtraKind == channelpkg.JailbreakKindPrompt {
		extraRule = &channelpkg.JailbreakRule{
			Name:   payload.ExtraName,
			Kind:   channelpkg.JailbreakKindPrompt,
			Prompt: strings.TrimSpace(payload.ExtraPrompt),
			Mode:   payload.ExtraMode,
			Force:  payload.ExtraForce,
		}
	}
	effective := relaycommon.PreviewEffectiveSystemPrompt(
		payload.ChannelID,
		model,
		strings.TrimSpace(payload.BasePrompt),
		payload.BaseSource,
		payload.BaseOverride,
		extraRule,
		payload.ExcludeRuleID,
	)
	messages := make([]map[string]string, 0, 2)
	if effective.Prompt != "" {
		messages = append(messages, map[string]string{"role": "system", "content": effective.Prompt})
	}
	if message := strings.TrimSpace(payload.UserMessage); message != "" {
		messages = append(messages, map[string]string{"role": "user", "content": message})
	}
	promptTokens := 0
	if effective.Prompt != "" {
		promptTokens = relaycommon.CountTextToken(effective.Prompt, model)
	}
	common.CtxApiSuccess(c, common.H{
		"kind":          channelpkg.JailbreakKindPrompt,
		"rules":         effective.JailbreakRules,
		"prompt":        effective.Prompt,
		"override":      effective.Override,
		"source":        effective.Source,
		"prompt_tokens": promptTokens,
		"messages":      messages,
	})
}

// jailbreakTestPayload is the real upstream test request: it resolves the
// effective prompt (including unsaved edits) and sends it to the selected
// channel/model, returning the assistant response.
type jailbreakTestPayload struct {
	ChannelID     int    `json:"channel_id"`
	Model         string `json:"model"`
	BasePrompt    string `json:"base_prompt"`
	BaseSource    string `json:"base_source"`
	UserMessage   string `json:"user_message"`
	ExtraKind     string `json:"extra_kind"`
	ExtraMode     string `json:"extra_mode"`
	ExtraPrompt   string `json:"extra_prompt"`
	ExtraForce    bool   `json:"extra_force"`
	ExtraName     string `json:"extra_name"`
	ExcludeRuleID int    `json:"exclude_rule_id"`
}

func TestJailbreakRequest(c contract.Context) {
	var payload jailbreakTestPayload
	if err := c.BindJSON(&payload); err != nil {
		common.CtxApiError(c, err)
		return
	}
	model := strings.TrimSpace(payload.Model)
	if model == "" {
		common.CtxApiErrorMsg(c, "模型不能为空")
		return
	}
	if payload.ChannelID <= 0 {
		common.CtxApiErrorMsg(c, "请选择渠道")
		return
	}
	channel, err := channelpkg.CacheGetChannel(payload.ChannelID)
	if err != nil {
		channel, err = channelpkg.GetChannelById(payload.ChannelID, true)
	}
	if err != nil {
		common.CtxApiErrorMsg(c, "渠道不存在")
		return
	}
	testUserID, err := resolveChannelTestUserID(c)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	var extraRule *channelpkg.JailbreakRule
	if strings.TrimSpace(payload.ExtraPrompt) != "" && payload.ExtraKind == channelpkg.JailbreakKindPrompt {
		extraRule = &channelpkg.JailbreakRule{
			Name:   payload.ExtraName,
			Kind:   channelpkg.JailbreakKindPrompt,
			Prompt: strings.TrimSpace(payload.ExtraPrompt),
			Mode:   payload.ExtraMode,
			Force:  payload.ExtraForce,
		}
	}
	effective := relaycommon.PreviewEffectiveSystemPrompt(
		payload.ChannelID,
		model,
		strings.TrimSpace(payload.BasePrompt),
		payload.BaseSource,
		false,
		extraRule,
		payload.ExcludeRuleID,
	)
	messages := make([]dto.Message, 0, 2)
	if effective.Prompt != "" {
		messages = append(messages, dto.Message{Role: "system", Content: effective.Prompt})
	}
	if message := strings.TrimSpace(payload.UserMessage); message != "" {
		messages = append(messages, dto.Message{Role: "user", Content: message})
	}
	if len(messages) == 0 {
		common.CtxApiErrorMsg(c, "请输入测试用户消息")
		return
	}

	result := testChannel(c.Context(), channel, testUserID, model, "", false, messages)
	if result.localErr != nil {
		common.CtxApiErrorMsg(c, result.localErr.Error())
		return
	}
	if result.newAPIError != nil {
		common.CtxApiErrorMsg(c, result.newAPIError.Error())
		return
	}
	content := ""
	reasoning := ""
	finishReason := ""
	promptTokens := 0
	completionTokens := 0
	var parsed struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if len(result.responseBody) > 0 && common.Unmarshal(result.responseBody, &parsed) == nil && len(parsed.Choices) > 0 {
		content = parsed.Choices[0].Message.Content
		reasoning = parsed.Choices[0].Message.ReasoningContent
		finishReason = parsed.Choices[0].FinishReason
		promptTokens = parsed.Usage.PromptTokens
		completionTokens = parsed.Usage.CompletionTokens
	}
	common.CtxApiSuccess(c, common.H{
		"content":           content,
		"reasoning":         reasoning,
		"finish_reason":     finishReason,
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
		"response":          string(result.responseBody),
	})
}
