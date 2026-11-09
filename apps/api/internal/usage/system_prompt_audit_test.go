package usage

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	promptpresets "github.com/QuantumNous/new-api/relaykit/dto/prompt_presets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateTextOtherInfoSystemPromptAudit pins the admin_info.system_prompt
// audit shape: which layer supplied the injected prompt, the preset name that
// produced it, and whether auto matching picked it. Nesting under admin_info
// keeps the record admin-only because non-admin log views strip that object.
func TestGenerateTextOtherInfoSystemPromptAudit(t *testing.T) {
	tests := []struct {
		name        string
		audit       *relaycommon.EffectiveSystemPrompt
		wantSource  string
		wantPreset  string
		wantAuto    bool
		wantPresent bool
	}{
		{
			name:        "token fixed preset",
			audit:       &relaycommon.EffectiveSystemPrompt{Prompt: "p", Source: relaycommon.SystemPromptSourceToken, Preset: promptpresets.PresetTavernCard},
			wantSource:  "token",
			wantPreset:  promptpresets.PresetTavernCard,
			wantPresent: true,
		},
		{
			name:        "token auto binding",
			audit:       &relaycommon.EffectiveSystemPrompt{Prompt: "p", Source: relaycommon.SystemPromptSourceToken, Preset: promptpresets.PresetSillyTavern, AutoResolved: true},
			wantSource:  "token",
			wantPreset:  promptpresets.PresetSillyTavern,
			wantAuto:    true,
			wantPresent: true,
		},
		{
			name:        "channel plain text",
			audit:       &relaycommon.EffectiveSystemPrompt{Prompt: "p", Source: relaycommon.SystemPromptSourceChannel},
			wantSource:  "channel",
			wantPresent: true,
		},
		{
			name:        "no injection records nothing",
			audit:       nil,
			wantPresent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cc, _ := fiberadapter.NewSyntheticContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
			relayInfo := &relaycommon.RelayInfo{
				StartTime:         time.Now(),
				FirstResponseTime: time.Now(),
				// GenerateTextOtherInfo reads promoted ChannelMeta fields
				// (UpstreamModelName), so the meta pointer must be set.
				ChannelMeta:       &relaycommon.ChannelMeta{},
				SystemPromptAudit: tt.audit,
			}
			other := GenerateTextOtherInfo(cc, relayInfo, 1, 1, 1, 0, 1, 0, 1)

			adminInfo, ok := other["admin_info"].(map[string]interface{})
			require.True(t, ok)
			record, present := adminInfo["system_prompt"]
			assert.Equal(t, tt.wantPresent, present)
			if !tt.wantPresent {
				return
			}
			fields, ok := record.(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, tt.wantSource, fields["source"])
			assert.Equal(t, tt.wantPreset, fields["preset"])
			assert.Equal(t, tt.wantAuto, fields["auto_resolved"])
		})
	}
}
