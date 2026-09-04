package channel

import "github.com/QuantumNous/new-api/internal/common/cachex"

// Test-only bridges for the external channel_test package. The codex-template
// affinity test asserts against relay/common.RelayInfo, and relay/common imports
// this package, so that test must live in channel_test to keep the test import
// closure acyclic. These two helpers are the only unexported symbols it needs.
func BuildChannelAffinityCacheKeySuffixForTest(rule ChannelAffinityRule, modelName, usingGroup, affinityValue string) string {
	return buildChannelAffinityCacheKeySuffix(rule, modelName, usingGroup, affinityValue)
}

func GetChannelAffinityCacheForTest() *cachex.HybridCache[int] {
	return getChannelAffinityCache()
}
