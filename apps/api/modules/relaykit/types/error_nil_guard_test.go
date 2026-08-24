package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewErrorWithStatusCodeNilErr is a regression test for the panic that
// occurred when the target-domain hard block in Relay passed an uninitialized
// err variable. The constructor must degrade gracefully instead of crashing.
func TestNewErrorWithStatusCodeNilErr(t *testing.T) {
	err := NewErrorWithStatusCode(nil, ErrorCodeSensitiveWordsDetected, 403)
	require.NotNil(t, err)
	assert.Equal(t, 403, err.StatusCode)
	assert.Equal(t, ErrorCodeSensitiveWordsDetected, err.GetErrorCode())
	assert.NotEmpty(t, err.Error())
}

func TestNewErrorWithStatusCodeNormalErr(t *testing.T) {
	orig := assert.AnError
	err := NewErrorWithStatusCode(orig, ErrorCodeInvalidRequest, 400)
	require.NotNil(t, err)
	assert.Equal(t, 400, err.StatusCode)
	assert.Equal(t, orig, err.Err)
	assert.Equal(t, ErrorCodeInvalidRequest, err.GetErrorCode())
}
