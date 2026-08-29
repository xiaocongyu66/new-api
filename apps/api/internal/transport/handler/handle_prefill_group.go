package handler

import (
	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"strconv"

	"github.com/QuantumNous/new-api/internal/common"
)

// GetPrefillGroups 获取预填组列表，可通过 ?type=xxx 过滤
func GetPrefillGroups(c contract.Context) {
	groupType := c.Query("type")
	groups, err := channelpkg.GetAllPrefillGroups(groupType)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, groups)
}

// CreatePrefillGroup 创建新的预填组
func CreatePrefillGroup(c contract.Context) {
	var g channelpkg.PrefillGroup
	if err := c.BindJSON(&g); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if g.Name == "" || g.Type == "" {
		common.CtxApiErrorMsg(c, "组名称和类型不能为空")
		return
	}
	// 创建前检查名称
	if dup, err := channelpkg.IsPrefillGroupNameDuplicated(0, g.Name); err != nil {
		common.CtxApiError(c, err)
		return
	} else if dup {
		common.CtxApiErrorMsg(c, "组名称已存在")
		return
	}

	if err := g.Insert(); err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, &g)
}

// UpdatePrefillGroup 更新预填组
func UpdatePrefillGroup(c contract.Context) {
	var g channelpkg.PrefillGroup
	if err := c.BindJSON(&g); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if g.Id == 0 {
		common.CtxApiErrorMsg(c, "缺少组 ID")
		return
	}
	// 名称冲突检查
	if dup, err := channelpkg.IsPrefillGroupNameDuplicated(g.Id, g.Name); err != nil {
		common.CtxApiError(c, err)
		return
	} else if dup {
		common.CtxApiErrorMsg(c, "组名称已存在")
		return
	}

	if err := g.Update(); err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, &g)
}

// DeletePrefillGroup 删除预填组
func DeletePrefillGroup(c contract.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if err := channelpkg.DeletePrefillGroupByID(id); err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, nil)
}
