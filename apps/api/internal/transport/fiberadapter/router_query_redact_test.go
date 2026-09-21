package fiberadapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactQueryKeys(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"no credentials", "model=gpt-4&stream=true", "model=gpt-4&stream=true"},
		{"key only", "key=sk-abc123", "key=REDACTED"},
		{"key empty value", "key=", "key=REDACTED"},
		{"key among others", "model=gpt-4&key=sk-abc&x=1", "model=gpt-4&key=REDACTED&x=1"},
		{"mj api secret", "mj-api-secret=secret-value", "mj-api-secret=REDACTED"},
		{"repeated key", "key=a&x=1&key=b", "key=REDACTED&x=1&key=REDACTED"},
		{"encoded value untouched for other params", "q=a%20b&key=sk-x", "q=a%20b&key=REDACTED"},
		{"similar names untouched", "session_key=abc&keyboard=1", "session_key=abc&keyboard=1"},
		{"bare key flag without value untouched", "key&x=1", "key&x=1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, redactQueryKeys(tc.raw))
		})
	}
}
