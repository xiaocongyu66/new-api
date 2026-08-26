package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/administration"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// maskedSecret aliases the administration sentinel so existing package
// tests keep compiling against the moved use case layer.
const maskedSecret = administration.ProxyMaskedSecret

// ProxyConfigRequest aliases the administration request shape.
type ProxyConfigRequest = administration.ProxyConfigRequest

func GetProxyConfig(c contract.Context) {
	administration.GetProxyConfig(c)
}

func UpdateProxyConfig(c contract.Context) {
	administration.UpdateProxyConfig(c)
}

func GenerateProxyConfig(c contract.Context) {
	administration.GenerateProxyConfig(c)
}

func GetProxyStatus(c contract.Context) {
	administration.GetProxyStatus(c)
}

func ReloadProxy(c contract.Context) {
	administration.ReloadProxy(c)
}

func ListProxyNodes(c contract.Context) {
	administration.ListProxyNodes(c)
}

func BatchCreateProxyNodes(c contract.Context) {
	administration.BatchCreateProxyNodes(c)
}

func BatchSetProxyNodesEnabled(c contract.Context) {
	administration.BatchSetProxyNodesEnabled(c)
}

func BatchClearProxyNodeErrors(c contract.Context) {
	administration.BatchClearProxyNodeErrors(c)
}

func GetProxyNodeReport(c contract.Context) {
	administration.GetProxyNodeReport(c)
}

func GetProxyNode(c contract.Context) {
	administration.GetProxyNode(c)
}

func CreateProxyNode(c contract.Context) {
	administration.CreateProxyNode(c)
}

func UpdateProxyNode(c contract.Context) {
	administration.UpdateProxyNode(c)
}

func DeleteProxyNode(c contract.Context) {
	administration.DeleteProxyNode(c)
}

func TestProxyNode(c contract.Context) {
	administration.TestProxyNode(c)
}

func TestAllProxyNodes(c contract.Context) {
	administration.TestAllProxyNodes(c)
}
