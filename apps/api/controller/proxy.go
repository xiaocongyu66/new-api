package controller

import (
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// maskedSecret aliases the administration sentinel so existing package
// tests keep compiling against the moved use case layer.
const maskedSecret = ops.ProxyMaskedSecret

// ProxyConfigRequest aliases the administration request shape.
type ProxyConfigRequest = ops.ProxyConfigRequest

func GetProxyConfig(c contract.Context) {
	ops.GetProxyConfig(c)
}

func UpdateProxyConfig(c contract.Context) {
	ops.UpdateProxyConfig(c)
}

func GenerateProxyConfig(c contract.Context) {
	ops.GenerateProxyConfig(c)
}

func GetProxyStatus(c contract.Context) {
	ops.GetProxyStatus(c)
}

func ReloadProxy(c contract.Context) {
	ops.ReloadProxy(c)
}

func ListProxyNodes(c contract.Context) {
	ops.ListProxyNodes(c)
}

func BatchCreateProxyNodes(c contract.Context) {
	ops.BatchCreateProxyNodes(c)
}

func BatchSetProxyNodesEnabled(c contract.Context) {
	ops.BatchSetProxyNodesEnabled(c)
}

func BatchClearProxyNodeErrors(c contract.Context) {
	ops.BatchClearProxyNodeErrors(c)
}

func GetProxyNodeReport(c contract.Context) {
	ops.GetProxyNodeReport(c)
}

func GetProxyNode(c contract.Context) {
	ops.GetProxyNode(c)
}

func CreateProxyNode(c contract.Context) {
	ops.CreateProxyNode(c)
}

func UpdateProxyNode(c contract.Context) {
	ops.UpdateProxyNode(c)
}

func DeleteProxyNode(c contract.Context) {
	ops.DeleteProxyNode(c)
}

func TestProxyNode(c contract.Context) {
	ops.TestProxyNode(c)
}

func TestAllProxyNodes(c contract.Context) {
	ops.TestAllProxyNodes(c)
}
