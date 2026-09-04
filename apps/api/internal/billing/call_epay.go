package billing

import (
	"github.com/QuantumNous/new-api/internal/egress"
)

func GetCallbackAddress() string {
	if CustomCallbackAddress == "" {
		return egress.ServerAddress
	}
	return CustomCallbackAddress
}
