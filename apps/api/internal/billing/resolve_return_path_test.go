package billing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/egress"
	"github.com/stretchr/testify/assert"
)

func TestPaymentReturnURLUsesSuppliedDefaultDashboardPath(t *testing.T) {
	previousAddress := egress.ServerAddress
	egress.ServerAddress = "https://dashboard.example.com/"
	t.Cleanup(func() { egress.ServerAddress = previousAddress })

	assert.Equal(t, "https://dashboard.example.com/wallet", PaymentReturnURL("/wallet"))
}
