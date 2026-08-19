package model

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	rootmodel "github.com/QuantumNous/new-api/model"
)

// 启动期钩子：在 model.InitDB 完成后、根用户存在性检测 + Setup 记录写入。
// 原 model.CheckSetup 把这段逻辑放在 apps/api/model 包里；拆域后改为注册
// 钩子，避免 model 反向 import 业务包形成 import cycle。
func init() {
	rootmodel.RegisterStartupHook(checkSetupHook)
}

func checkSetupHook() {
	setup := rootmodel.GetSetup()
	if setup == nil {
		if RootUserExists() {
			common.SysLog("system is not initialized, but root user exists")
			newSetup := rootmodel.Setup{
				Version:       common.Version,
				InitializedAt: time.Now().Unix(),
			}
			if err := common.DB.Create(&newSetup).Error; err != nil {
				common.SysLog("failed to create setup record: " + err.Error())
			}
			constant.Setup = true
		} else {
			common.SysLog("system is not initialized and no root user exists")
			constant.Setup = false
		}
	} else {
		common.SysLog("system is already initialized at: " + time.Unix(setup.InitializedAt, 0).String())
		constant.Setup = true
	}
}
