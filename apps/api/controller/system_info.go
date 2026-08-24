package controller

import (
	"github.com/QuantumNous/new-api/internal/capabilities/administration"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func ListSystemInstances(c contract.Context) {
	instances, err := administration.ListSystemInstances()
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	now := common.GetTimestamp()
	responses := make([]model.SystemInstanceResponse, 0, len(instances))
	for _, instance := range instances {
		responses = append(responses, instance.ToResponse(now))
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    responses,
	})
}

func DeleteStaleSystemInstances(c contract.Context) {
	deletedCount, err := administration.DeleteStaleSystemInstances(common.GetTimestamp())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	common.CtxApiSuccess(c, common.H{
		"deleted_count": deletedCount,
	})
}

func DeleteStaleSystemInstance(c contract.Context) {
	nodeName := c.Param("node_name")
	if strings.TrimSpace(nodeName) == "" {
		common.CtxApiErrorMsg(c, "node name is required")
		return
	}

	deleted, err := administration.DeleteStaleSystemInstance(nodeName, common.GetTimestamp())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if !deleted {
		common.CtxApiErrorMsg(c, "instance is not stale or no longer exists")
		return
	}

	common.CtxApiSuccess(c, common.H{
		"deleted_count": 1,
	})
}
