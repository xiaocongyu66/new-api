package billing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/egress/fetch_url"
	"github.com/stretchr/testify/assert"
)

func TestPaymentReturnPathUsesDefaultDashboardRoutes(t *testing.T) {
	previousAddress := fetch_url.ServerAddress
	fetch_url.ServerAddress = "https://dashboard.example.com/"
	t.Cleanup(func() { fetch_url.ServerAddress = previousAddress })

	assert.Equal(
		t,
		"https://dashboard.example.com/wallet?pay=success",
		paymentReturnPath("/wallet?pay=success"),
	)
	assert.Equal(
		t,
		"https://dashboard.example.com/usage-logs",
		paymentReturnPath("/usage-logs"),
	)
}
