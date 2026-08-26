package channel

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func resetPricingEndpointTestTables(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}))
	model.DB = db
	model.InitDialectColumns()
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	for _, table := range []string{"abilities", "channels", "models", "vendors"} {
		require.NoError(t, model.DB.Exec("DELETE FROM " + table).Error)
	}
	model.InitChannelCache()
	model.InvalidatePricingCache()
	t.Cleanup(func() {
		for _, table := range []string{"abilities", "channels", "models", "vendors"} {
			require.NoError(t, model.DB.Exec("DELETE FROM " + table).Error)
		}
		model.InitChannelCache()
		model.InvalidatePricingCache()
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		model.DB = previousDB
	})
}

func insertPricingEndpointChannel(t *testing.T, channelID int, channelType int, settings dto.ChannelOtherSettings) {
	t.Helper()
	channel := &model.Channel{
		Id:     channelID,
		Type:   channelType,
		Key:    fmt.Sprintf("key-%d", channelID),
		Status: common.ChannelStatusEnabled,
		Name:   fmt.Sprintf("channel-%d", channelID),
	}
	if settings.AdvancedCustom != nil {
		channel.SetOtherSettings(settings)
	}
	require.NoError(t, model.DB.Create(channel).Error)
}

func insertPricingEndpointAbility(t *testing.T, channelID int, modelName string) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Ability{
		Group:     "default",
		Model:     modelName,
		ChannelId: channelID,
		Enabled:   true,
	}).Error)
}

func pricingEndpointAdvancedCustomConfig(routes ...dto.AdvancedCustomRoute) dto.ChannelOtherSettings {
	return dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{
			Routes: routes,
		},
	}
}

func pricingEndpointTypesFromPricing(pricings []model.Pricing) map[string][]constant.EndpointType {
	byModel := make(map[string][]constant.EndpointType)
	for _, pricing := range pricings {
		byModel[pricing.ModelName] = pricing.SupportedEndpointTypes
	}
	return byModel
}

func TestInitChannelCacheInvalidatesStartupPricingBuiltBeforeChannelCache(t *testing.T) {
	resetPricingEndpointTestTables(t)

	insertPricingEndpointChannel(t, 302, constant.ChannelTypeAdvancedCustom, pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/v1/chat/completions",
		},
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/v1beta/models/{model}:generateContent",
			Converter:    "openai_responses_to_gemini_generate_content",
			Models:       []string{"re:^gemini-"},
		},
	))
	insertPricingEndpointAbility(t, 302, "gemini-3.5-flash")

	staleByModel := pricingEndpointTypesFromPricing(model.GetPricing())
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, staleByModel["gemini-3.5-flash"])

	model.InitChannelCache()

	rebuiltByModel := pricingEndpointTypesFromPricing(model.GetPricing())
	assert.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
	}, rebuiltByModel["gemini-3.5-flash"])
}

func TestCacheUpdateChannelSyncsAdvancedCustomConfig(t *testing.T) {
	resetPricingEndpointTestTables(t)

	channel := &model.Channel{
		Id:     401,
		Type:   constant.ChannelTypeAdvancedCustom,
		Key:    "key-401",
		Status: common.ChannelStatusEnabled,
		Name:   "channel-401",
	}
	channel.SetOtherSettings(pricingEndpointAdvancedCustomConfig(dto.AdvancedCustomRoute{
		IncomingPath: "/v1/responses",
		UpstreamPath: "/v1beta/models/{model}:generateContent",
		Converter:    "openai_responses_to_gemini_generate_content",
	}))
	model.CacheUpdateChannel(channel)

	configs, ok := model.LookupAdvancedCustomConfigs([]int{401})
	require.True(t, ok)
	require.NotNil(t, configs[401])
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIResponse}, configs[401].SupportedEndpointTypesForModel("gemini-3.5-flash"))

	channel.SetOtherSettings(pricingEndpointAdvancedCustomConfig(dto.AdvancedCustomRoute{
		IncomingPath: "/v1/chat/completions",
		UpstreamPath: "/v1/chat/completions",
	}))
	model.CacheUpdateChannel(channel)

	configs, ok = model.LookupAdvancedCustomConfigs([]int{401})
	require.True(t, ok)
	require.NotNil(t, configs[401])
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, configs[401].SupportedEndpointTypesForModel("gemini-3.5-flash"))

	channel.Type = constant.ChannelTypeOpenAI
	model.CacheUpdateChannel(channel)

	configs, ok = model.LookupAdvancedCustomConfigs([]int{401})
	if ok {
		assert.Nil(t, configs[401])
	}
}