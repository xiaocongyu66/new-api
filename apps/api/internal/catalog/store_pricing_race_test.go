package channel_test

import (
	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPricingCacheConcurrentReadersAndInvalidation pins the invariant that
// GetPricing/GetVendors/GetSupportedEndpointMap never observe a torn pricing
// cache snapshot while invalidation replaces the cache wholesale (what every
// minute channel sync does via InitChannelCache). The writers swap whole slice
// headers, so an unsynchronized reader could splice a new backing-array
// pointer onto a stale length and panic - surfacing as an intermittent HTTP
// 500 on the model plaza. The plain job below exercises the paths; CI runs
// this package with -race, where the pre-fix code fails deterministically.
func TestPricingCacheConcurrentReadersAndInvalidation(t *testing.T) {
	resetPricingEndpointTestTables(t)
	insertPricingEndpointChannel(t, 901, constant.ChannelTypeOpenAI, dto.ChannelOtherSettings{})
	insertPricingEndpointAbility(t, 901, "gpt-cache-race-model")

	require.NotEmpty(t, catalog.GetPricing())

	stop := make(chan struct{})
	var writers sync.WaitGroup
	writers.Add(1)
	go func() {
		defer writers.Done()
		for i := 0; i < 50; i++ {
			catalog.InvalidatePricingCache()
			select {
			case <-stop:
				return
			default:
			}
			// Rebuild so the next invalidation races readers that believe the
			// cache is still fresh.
			catalog.GetPricing()
		}
	}()

	var readers sync.WaitGroup
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for i := 0; i < 200; i++ {
				for _, pricing := range catalog.GetPricing() {
					_ = pricing.ModelName
					_ = pricing.EnableGroup
				}
				_ = catalog.GetVendors()
				_ = catalog.GetSupportedEndpointMap()
			}
		}()
	}

	readers.Wait()
	close(stop)
	writers.Wait()

	assert.NotEmpty(t, catalog.GetPricing(), "pricing cache must repopulate after concurrent invalidation")
}
