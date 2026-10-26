//go:build with_wireguard

package egress

import (
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/wireguard"
)

// sing-box v1.14 removed the WireGuard outbound (WireGuard is endpoint-only now).
func registerWireGuardOutbound(*outbound.Registry) {}

func registerWireGuardEndpoint(registry *endpoint.Registry) {
	wireguard.RegisterEndpoint(registry)
}
