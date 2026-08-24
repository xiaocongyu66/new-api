package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/administration"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// GetAllVendors 获取供应商列表（分页）
func GetAllVendors(c contract.Context) {
	administration.ListVendors(c)
}

// SearchVendors 搜索供应商
func SearchVendors(c contract.Context) {
	administration.SearchVendors(c)
}

// GetVendorMeta 根据 ID 获取供应商
func GetVendorMeta(c contract.Context) {
	administration.GetVendor(c)
}

// CreateVendorMeta 新建供应商
func CreateVendorMeta(c contract.Context) {
	administration.CreateVendor(c)
}

// UpdateVendorMeta 更新供应商
func UpdateVendorMeta(c contract.Context) {
	administration.UpdateVendor(c)
}

// DeleteVendorMeta 删除供应商
func DeleteVendorMeta(c contract.Context) {
	administration.DeleteVendor(c)
}
