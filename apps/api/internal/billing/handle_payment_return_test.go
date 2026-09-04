package billing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/egress"
	"github.com/stretchr/testify/assert"
)

func TestPaymentReturnPathUsesDefaultDashboardRoutes(t *testing.T) {
	previousAddress := egress.ServerAddress
	egress.ServerAddress = "https://dashboard.example.com/"
	t.Cleanup(func() { egress.ServerAddress = previousAddress })

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
