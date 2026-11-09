package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/dto/prompt_presets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyTokenSystemPromptForTest wires a token binding into a synthetic relay
// context and returns the context, mirroring SetupContextForToken.
func applyTokenSystemPromptForTest(t *testing.T, tokenSettings dto.TokenSettings) contract.Context {
	t.Helper()
	cc, _ := fiberadapter.NewSyntheticContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	common.SetCtxKey(cc, constant.ContextKeyTokenSetting, tokenSettings)
	return cc
}

func TestTokenSystemPromptReplacesChannelPrompt(t *testing.T) {
	tokenPreset := promptpresets.Get(promptpresets.PresetSillyTavern)
	channelPreset := promptpresets.Get(promptpresets.PresetTavernCard)
	require.NotEmpty(t, tokenPreset)
	require.NotEmpty(t, channelPreset)

	cc := applyTokenSystemPromptForTest(t, dto.TokenSettings{SystemPromptPreset: promptpresets.PresetSillyTavern})
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4o",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{
			SystemPrompt: channelPreset,
		}},
	}
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "system", Content: "USER"}, {Role: "user", Content: "hi"}},
	}

	applySystemPromptIfNeeded(cc, info, request)

	require.Len(t, request.Messages, 2)
	assert.Equal(t, tokenPreset+"\nUSER", request.Messages[0].StringContent(),
		"the token prompt replaces the channel prompt, not stacks on it")
	require.NotNil(t, info.SystemPromptAudit)
	assert.Equal(t, relaycommon.SystemPromptSourceToken, info.SystemPromptAudit.Source)
	assert.Equal(t, promptpresets.PresetSillyTavern, info.SystemPromptAudit.Preset)
	assert.False(t, info.SystemPromptAudit.AutoResolved)
	assert.False(t, common.GetCtxKeyBool(cc, constant.ContextKeyUserSystemPromptOverridden))
}

func TestTokenSystemPromptOverrideDropsCallerSystemPrompt(t *testing.T) {
	tokenPreset := promptpresets.Get(promptpresets.PresetTavernCard)
	require.NotEmpty(t, tokenPreset)

	cc := applyTokenSystemPromptForTest(t, dto.TokenSettings{
		SystemPromptPreset:       promptpresets.PresetTavernCard,
		UserSystemPromptOverride: true,
	})
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4o",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "system", Content: "USER"}, {Role: "user", Content: "hi"}},
	}

	applySystemPromptIfNeeded(cc, info, request)

	require.Len(t, request.Messages, 2)
	assert.Equal(t, tokenPreset, request.Messages[0].StringContent())
	assert.True(t, common.GetCtxKeyBool(cc, constant.ContextKeyUserSystemPromptOverridden))
	require.NotNil(t, info.SystemPromptAudit)
	assert.True(t, info.SystemPromptAudit.Override)
}

func TestTokenEmptyBindingFallsBackToChannelPrompt(t *testing.T) {
	channelPreset := promptpresets.Get(promptpresets.PresetTavernCard)
	require.NotEmpty(t, channelPreset)

	cc := applyTokenSystemPromptForTest(t, dto.TokenSettings{})
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4o",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{
			SystemPromptPreset: promptpresets.PresetTavernCard,
		}},
	}
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "system", Content: "USER"}, {Role: "user", Content: "hi"}},
	}

	applySystemPromptIfNeeded(cc, info, request)

	assert.Equal(t, channelPreset+"\nUSER", request.Messages[0].StringContent())
	require.NotNil(t, info.SystemPromptAudit)
	assert.Equal(t, relaycommon.SystemPromptSourceChannel, info.SystemPromptAudit.Source)
	assert.Equal(t, promptpresets.PresetTavernCard, info.SystemPromptAudit.Preset)
}

func TestTokenAutoBindingResolvesPerRequestModel(t *testing.T) {
	cc := applyTokenSystemPromptForTest(t, dto.TokenSettings{SystemPromptPreset: promptpresets.PresetAuto})
	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-sonnet-4-5",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "hi"}},
	}

	applySystemPromptIfNeeded(cc, info, request)

	want := promptpresets.Get(promptpresets.ResolveAuto("claude-sonnet-4-5"))
	assert.Equal(t, want, request.Messages[0].StringContent())
	require.NotNil(t, info.SystemPromptAudit)
	assert.True(t, info.SystemPromptAudit.AutoResolved)
	assert.Equal(t, promptpresets.ResolveAuto("claude-sonnet-4-5"), info.SystemPromptAudit.Preset)
}
