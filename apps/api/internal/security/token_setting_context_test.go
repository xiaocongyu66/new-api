package security

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/QuantumNous/new-api/relaykit/dto"
	promptpresets "github.com/QuantumNous/new-api/relaykit/dto/prompt_presets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTokenSettingContext() contract.Context {
	cc, _ := fiberadapter.NewSyntheticContext(nil)
	return cc
}

// TestSetupContextForTokenPublishesRelaySettings pins the injection point the
// relay resolver depends on: the parsed token settings must land under
// ContextKeyTokenSetting the same way channel settings do, and an empty
// setting column must still publish the zero value so "absent" and "cleared"
// behave identically.
func TestSetupContextForTokenPublishesRelaySettings(t *testing.T) {
	tests := []struct {
		name    string
		setting string
		want    dto.TokenSettings
	}{
		{
			name:    "empty setting column yields zero settings",
			setting: "",
			want:    dto.TokenSettings{},
		},
		{
			name:    "stored binding is parsed",
			setting: `{"system_prompt_preset":"auto","system_prompt":"text","user_system_prompt_override":true}`,
			want: dto.TokenSettings{
				SystemPromptPreset:       promptpresets.PresetAuto,
				SystemPrompt:             "text",
				UserSystemPromptOverride: true,
			},
		},
		{
			name:    "malformed JSON yields zero settings",
			setting: "not-json",
			want:    dto.TokenSettings{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cc := newTokenSettingContext()
			token := &identity.Token{Id: 1, UserId: 2, Key: "sk-test", Setting: tt.setting}

			require.NoError(t, SetupContextForToken(cc, token))

			value, ok := common.GetContextKeyType[dto.TokenSettings](cc, constant.ContextKeyTokenSetting)
			require.True(t, ok)
			assert.Equal(t, tt.want, value)
		})
	}
}
