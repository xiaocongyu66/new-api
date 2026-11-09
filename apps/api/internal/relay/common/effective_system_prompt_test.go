package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/QuantumNous/new-api/relaykit/dto"
	promptpresets "github.com/QuantumNous/new-api/relaykit/dto/prompt_presets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newResolverContext(tokenSettings *dto.TokenSettings) contract.Context {
	cc, _ := fiberadapter.NewSyntheticContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	if tokenSettings != nil {
		common.SetCtxKey(cc, constant.ContextKeyTokenSetting, *tokenSettings)
	}
	return cc
}

func TestResolveEffectiveSystemPromptTokenWinsOverChannel(t *testing.T) {
	channelPreset := promptpresets.Get(promptpresets.PresetTavernCard)
	tokenPreset := promptpresets.Get(promptpresets.PresetSillyTavern)
	require.NotEmpty(t, channelPreset)
	require.NotEmpty(t, tokenPreset)

	cc := newResolverContext(&dto.TokenSettings{SystemPromptPreset: promptpresets.PresetSillyTavern})
	info := &RelayInfo{
		OriginModelName: "gpt-4o",
		ChannelMeta: &ChannelMeta{ChannelSetting: dto.ChannelSettings{
			SystemPrompt:             channelPreset,
			UserSystemPromptOverride: true,
		}},
	}

	effective := ResolveEffectiveSystemPrompt(cc, info)
	assert.Equal(t, tokenPreset, effective.Prompt)
	assert.Equal(t, SystemPromptSourceToken, effective.Source)
	assert.Equal(t, promptpresets.PresetSillyTavern, effective.Preset)
	assert.False(t, effective.Override, "token layer with no override must not inherit the channel flag")
	assert.False(t, effective.AutoResolved)
}

func TestResolveEffectiveSystemPromptFallsBackToChannel(t *testing.T) {
	channelPreset := promptpresets.Get(promptpresets.PresetTavernCard)
	require.NotEmpty(t, channelPreset)

	cc := newResolverContext(nil)
	info := &RelayInfo{
		OriginModelName: "gpt-4o",
		ChannelMeta: &ChannelMeta{ChannelSetting: dto.ChannelSettings{
			SystemPromptPreset:       promptpresets.PresetTavernCard,
			UserSystemPromptOverride: true,
		}},
	}

	effective := ResolveEffectiveSystemPrompt(cc, info)
	assert.Equal(t, channelPreset, effective.Prompt)
	assert.Equal(t, SystemPromptSourceChannel, effective.Source)
	assert.Equal(t, promptpresets.PresetTavernCard, effective.Preset)
	assert.True(t, effective.Override)
	assert.False(t, effective.AutoResolved)
}

func TestResolveEffectiveSystemPromptTokenAutoResolvesByModel(t *testing.T) {
	cc := newResolverContext(&dto.TokenSettings{SystemPromptPreset: promptpresets.PresetAuto})
	info := &RelayInfo{
		OriginModelName: "claude-sonnet-4-5",
		ChannelMeta:     &ChannelMeta{},
	}

	effective := ResolveEffectiveSystemPrompt(cc, info)
	assert.Equal(t, promptpresets.Get(promptpresets.ResolveAuto("claude-sonnet-4-5")), effective.Prompt)
	assert.Equal(t, promptpresets.ResolveAuto("claude-sonnet-4-5"), effective.Preset)
	assert.True(t, effective.AutoResolved)
	assert.Equal(t, SystemPromptSourceToken, effective.Source)
}

func TestResolveEffectiveSystemPromptEmptyEverywhere(t *testing.T) {
	cc := newResolverContext(nil)
	info := &RelayInfo{OriginModelName: "gpt-4o", ChannelMeta: &ChannelMeta{}}

	effective := ResolveEffectiveSystemPrompt(cc, info)
	assert.Empty(t, effective.Prompt)
	assert.Empty(t, effective.Source)
}

func TestResolveEffectiveSystemPromptNilMetaFallsBackToContextSetting(t *testing.T) {
	// Mirrors the pre-consume estimate: RelayInfo has no ChannelMeta yet, the
	// channel setting is only in context.
	channelPrompt := promptpresets.Get(promptpresets.PresetTavernCard)
	require.NotEmpty(t, channelPrompt)

	cc := newResolverContext(nil)
	common.SetCtxKey(cc, constant.ContextKeyChannelSetting, dto.ChannelSettings{SystemPrompt: channelPrompt})
	info := &RelayInfo{OriginModelName: "gpt-4o"}

	effective := ResolveEffectiveSystemPrompt(cc, info)
	assert.Equal(t, channelPrompt, effective.Prompt)
	assert.Equal(t, SystemPromptSourceChannel, effective.Source)
}

func TestResolveEffectiveSystemPromptNilInfo(t *testing.T) {
	assert.Equal(t, EffectiveSystemPrompt{}, ResolveEffectiveSystemPrompt(nil, nil))
}
