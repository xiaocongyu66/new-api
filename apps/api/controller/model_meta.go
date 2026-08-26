package controller

import (
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// GetAllModelsMeta 获取模型列表（分页）
func GetAllModelsMeta(c contract.Context) {
	ops.ListModels(c)
}

// SearchModelsMeta 搜索模型列表
func SearchModelsMeta(c contract.Context) {
	ops.SearchModels(c)
}

// GetModelMeta 根据 ID 获取单条模型信息
func GetModelMeta(c contract.Context) {
	ops.GetModel(c)
}

// CreateModelMeta 新建模型
func CreateModelMeta(c contract.Context) {
	ops.CreateModel(c)
}

// UpdateModelMeta 更新模型
func UpdateModelMeta(c contract.Context) {
	ops.UpdateModel(c)
}

// DeleteModelMeta 删除模型
func DeleteModelMeta(c contract.Context) {
	ops.DeleteModel(c)
}
