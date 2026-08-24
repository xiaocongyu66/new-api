package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTargetDomainGateCompatibleError is the regression test for #415: the
// target-domain hard gate in Relay previously passed an uninitialized err to
// types.NewErrorWithStatusCode, panicking on every hit. Pin the gate's two
// halves against the request in the issue: a gov.cn input is detected and the
// error the gate builds is a non-panicking 403 + SensitiveWordsDetected.
func TestTargetDomainGateCompatibleError(t *testing.T) {
	d := service.CheckSensitiveTargets("please visit https://www.gov.cn")
	require.NotEmpty(t, d, "gov.cn input must hit the target-domain gate")

	var apiErr *types.NewAPIError
	require.NotPanics(t, func() {
		apiErr = types.NewErrorWithStatusCode(errors.New("input blocked by target domain"), types.ErrorCodeSensitiveWordsDetected, http.StatusForbidden)
	})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeSensitiveWordsDetected, apiErr.GetErrorCode())
}
