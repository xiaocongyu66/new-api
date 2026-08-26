package controller

import (
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// GetAllVendors 获取供应商列表（分页）
func GetAllVendors(c contract.Context) {
	ops.ListVendors(c)
}

// SearchVendors 搜索供应商
func SearchVendors(c contract.Context) {
	ops.SearchVendors(c)
}

// GetVendorMeta 根据 ID 获取供应商
func GetVendorMeta(c contract.Context) {
	ops.GetVendor(c)
}

// CreateVendorMeta 新建供应商
func CreateVendorMeta(c contract.Context) {
	ops.CreateVendor(c)
}

// UpdateVendorMeta 更新供应商
func UpdateVendorMeta(c contract.Context) {
	ops.UpdateVendor(c)
}

// DeleteVendorMeta 删除供应商
func DeleteVendorMeta(c contract.Context) {
	ops.DeleteVendor(c)
}
