package channel

import (
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/dbinfra"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGatewayRoutingOptionAllowlistIsExplicit(t *testing.T) {
	expected := []string{
		"AudioCompletionRatio", "AudioRatio", "AutoGroups", "CacheRatio", "CompletionRatio",
		"CreateCacheRatio", "GroupGroupRatio", "GroupRatio", "ImageRatio", "MaxTokenAutoGroups",
		"ModelPrice", "ModelRatio", "UserUsableGroups",
	}
	assert.Equal(t, expected, dbinfra.GatewayRoutingOptionKeyList())
	assert.True(t, dbinfra.IsGatewayRoutingOptionKey("ModelRatio"))
	assert.False(t, dbinfra.IsGatewayRoutingOptionKey("ModelRatioSecret"))
	assert.False(t, dbinfra.IsGatewayRoutingOptionKey("proxy_config"))
}

func TestGatewayRoutingOptionUpdateCommitsOneRevision(t *testing.T) {
	previousDB := dbx.DB
	previousOptionMap := common.OptionMap
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&dbinfra.Option{}, &GatewayConfigRevision{}, &GatewayConfigOutbox{}))
	dbx.DB = db
	common.OptionMap = make(map[string]string)
	previousMutate := dbinfra.MutateGatewayRoutingFn
	dbinfra.MutateGatewayRoutingFn = MutateGatewayRouting
	t.Cleanup(func() {
		dbx.DB, common.OptionMap = previousDB, previousOptionMap
		dbinfra.MutateGatewayRoutingFn = previousMutate
	})
	require.NoError(t, InitializeGatewayConfigRevision())

	require.NoError(t, dbinfra.UpdateOption("ModelPrice", "{}"))
	assert.Equal(t, int64(2), currentGatewayRevision(t))
	assert.Equal(t, []int64{2}, outboxRevisions(t))
	assert.Equal(t, "{}", requireOptionValue(t, db, "ModelPrice"))
	assert.Equal(t, "{}", common.OptionMap["ModelPrice"])
}

func TestGatewayRoutingOptionBulkCommitsOneRevision(t *testing.T) {
	previousDB := dbx.DB
	previousOptionMap := common.OptionMap
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&dbinfra.Option{}, &GatewayConfigRevision{}, &GatewayConfigOutbox{}))
	dbx.DB = db
	common.OptionMap = make(map[string]string)
	previousMutate := dbinfra.MutateGatewayRoutingFn
	dbinfra.MutateGatewayRoutingFn = MutateGatewayRouting
	t.Cleanup(func() {
		dbx.DB, common.OptionMap = previousDB, previousOptionMap
		dbinfra.MutateGatewayRoutingFn = previousMutate
	})
	require.NoError(t, InitializeGatewayConfigRevision())

	require.NoError(t, dbinfra.UpdateOptionsBulk(map[string]string{"ModelRatio": "{}", "SMTPToken": "synthetic"}))
	assert.Equal(t, int64(2), currentGatewayRevision(t))
	assert.Equal(t, []int64{2}, outboxRevisions(t))
	assert.Equal(t, "synthetic", common.OptionMap["SMTPToken"])
}

func requireOptionValue(t *testing.T, db *gorm.DB, key string) string {
	t.Helper()
	var option dbinfra.Option
	require.NoError(t, db.Where(&dbinfra.Option{Key: key}).First(&option).Error)
	return option.Value
}
