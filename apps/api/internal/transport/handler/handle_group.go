package handler

import (
	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/catalog/resolve_group"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"

	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
)

func GetGroups(c contract.Context) {
	groupNames := make([]string, 0)
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		groupNames = append(groupNames, groupName)
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    groupNames,
	})
}

func GetUserGroups(c contract.Context) {
	usableGroups := make(map[string]map[string]interface{})
	userGroup := ""
	userId := c.GetInt("id")
	userGroup, _ = identity.GetUserGroup(userId, false)
	userUsableGroups := resolve_group.GetUserUsableGroups(userGroup)
	for groupName, _ := range ratio_setting.GetGroupRatioCopy() {
		// UserUsableGroups contains the groups that the user can use
		if desc, ok := userUsableGroups[groupName]; ok {
			usableGroups[groupName] = map[string]interface{}{
				"ratio": channelpkg.GetUserGroupRatio(userGroup, groupName),
				"desc":  desc,
			}
		}
	}
	if _, ok := userUsableGroups["auto"]; ok {
		usableGroups["auto"] = map[string]interface{}{
			"ratio": "自动",
			"desc":  resolve_group.GetUsableGroupDescription("auto"),
		}
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    usableGroups,
	})
}
