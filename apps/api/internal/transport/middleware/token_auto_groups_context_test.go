package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTokenAutoGroupsContext() contract.Context {
	ctx, _ := fiberadapter.NewSyntheticContext(nil)
	return ctx
}

func TestSetupContextForTokenPreservesCustomAutoGroupsOrder(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &identity.Token{Id: 1, UserId: 2, AutoGroups: `["vip","default"]`}

	require.NoError(t, security.SetupContextForToken(ctx, token))
	value, ok := common.GetCtxKey(ctx, constant.ContextKeyTokenAutoGroups)
	require.True(t, ok)
	assert.Equal(t, []string{"vip", "default"}, value)
}

func TestSetupContextForTokenTreatsStoredEmptyArrayAsInheritance(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &identity.Token{Id: 1, UserId: 2, AutoGroups: `[]`}

	require.NoError(t, security.SetupContextForToken(ctx, token))
	_, ok := common.GetCtxKey(ctx, constant.ContextKeyTokenAutoGroups)
	assert.False(t, ok)
}

func TestSetupContextForTokenMalformedAutoGroupsFailsClosed(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &identity.Token{Id: 1, UserId: 2, AutoGroups: `not-json`}

	require.NoError(t, security.SetupContextForToken(ctx, token))
	value, ok := common.GetCtxKey(ctx, constant.ContextKeyTokenAutoGroups)
	require.True(t, ok)
	assert.Equal(t, []string{}, value)
}
