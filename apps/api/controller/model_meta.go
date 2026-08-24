package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/administration"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// GetAllModelsMeta 获取模型列表（分页）
func GetAllModelsMeta(c contract.Context) {
	administration.ListModels(c)
}

// SearchModelsMeta 搜索模型列表
func SearchModelsMeta(c contract.Context) {
	administration.SearchModels(c)
}

// GetModelMeta 根据 ID 获取单条模型信息
func GetModelMeta(c contract.Context) {
	administration.GetModel(c)
}

// CreateModelMeta 新建模型
func CreateModelMeta(c contract.Context) {
	administration.CreateModel(c)
}

// UpdateModelMeta 更新模型
func UpdateModelMeta(c contract.Context) {
	administration.UpdateModel(c)
}

// DeleteModelMeta 删除模型
func DeleteModelMeta(c contract.Context) {
	administration.DeleteModel(c)
}
