package ops

import (
	channel "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"strconv"

	"github.com/QuantumNous/new-api/internal/common"
)

// ---- Vendor Use Cases ----

// ListVendors returns paginated vendor list.
func ListVendors(c contract.Context) {
	pageInfo := common.GetPageQuery(c)
	vendors, err := channel.GetAllVendors(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	var total int64
	dbx.DB.Model(&channel.Vendor{}).Count(&total)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(vendors)
	common.CtxApiSuccess(c, pageInfo)
}

// SearchVendors returns paginated vendor search results.
func SearchVendors(c contract.Context) {
	keyword := c.Query("keyword")
	pageInfo := common.GetPageQuery(c)
	vendors, total, err := channel.SearchVendors(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(vendors)
	common.CtxApiSuccess(c, pageInfo)
}

// GetVendor returns a single vendor by ID.
func GetVendor(c contract.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	v, err := channel.GetVendorByID(id)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, v)
}

// CreateVendor creates a new vendor.
func CreateVendor(c contract.Context) {
	var v channel.Vendor
	if err := c.BindJSON(&v); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if v.Name == "" {
		common.CtxApiErrorMsg(c, "供应商名称不能为空")
		return
	}
	if dup, err := channel.IsVendorNameDuplicated(0, v.Name); err != nil {
		common.CtxApiError(c, err)
		return
	} else if dup {
		common.CtxApiErrorMsg(c, "供应商名称已存在")
		return
	}

	if err := v.Insert(); err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, &v)
}

// UpdateVendor updates an existing vendor.
func UpdateVendor(c contract.Context) {
	var v channel.Vendor
	if err := c.BindJSON(&v); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if v.Id == 0 {
		common.CtxApiErrorMsg(c, "缺少供应商 ID")
		return
	}
	if dup, err := channel.IsVendorNameDuplicated(v.Id, v.Name); err != nil {
		common.CtxApiError(c, err)
		return
	} else if dup {
		common.CtxApiErrorMsg(c, "供应商名称已存在")
		return
	}

	if err := v.Update(); err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, &v)
}

// DeleteVendor deletes a vendor.
func DeleteVendor(c contract.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	var existing channel.Vendor
	if err := dbx.DB.First(&existing, id).Error; err != nil {
		common.CtxApiError(c, err)
		return
	}
	if err := existing.Delete(); err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, nil)
}
