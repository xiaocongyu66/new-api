package controller

import (
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// GetAllVendors 获取供应商列表（分页）
func GetAllVendors(c contract.Context) {
	pageInfo := common.GetPageQuery(c)
	vendors, err := model.GetAllVendors(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	var total int64
	model.DB.Model(&model.Vendor{}).Count(&total)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(vendors)
	common.CtxApiSuccess(c, pageInfo)
}

// SearchVendors 搜索供应商
func SearchVendors(c contract.Context) {
	keyword := c.Query("keyword")
	pageInfo := common.GetPageQuery(c)
	vendors, total, err := model.SearchVendors(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(vendors)
	common.CtxApiSuccess(c, pageInfo)
}

// GetVendorMeta 根据 ID 获取供应商
func GetVendorMeta(c contract.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	v, err := model.GetVendorByID(id)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, v)
}

// CreateVendorMeta 新建供应商
func CreateVendorMeta(c contract.Context) {
	var v model.Vendor
	if err := c.BindJSON(&v); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if v.Name == "" {
		common.CtxApiErrorMsg(c, "供应商名称不能为空")
		return
	}
	// 创建前先检查名称
	if dup, err := model.IsVendorNameDuplicated(0, v.Name); err != nil {
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

// UpdateVendorMeta 更新供应商
func UpdateVendorMeta(c contract.Context) {
	var v model.Vendor
	if err := c.BindJSON(&v); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if v.Id == 0 {
		common.CtxApiErrorMsg(c, "缺少供应商 ID")
		return
	}
	// 名称冲突检查
	if dup, err := model.IsVendorNameDuplicated(v.Id, v.Name); err != nil {
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

// DeleteVendorMeta 删除供应商
func DeleteVendorMeta(c contract.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	var existing model.Vendor
	if err := model.DB.First(&existing, id).Error; err != nil {
		common.CtxApiError(c, err)
		return
	}
	if err := existing.Delete(); err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, nil)
}
