package ops

import (
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/dbinfra"
	"github.com/QuantumNous/new-api/internal/identity"

	"github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

var errSetupAlreadyDone = errors.New("setup already done")

type Setup struct {
	Status       bool   `json:"status"`
	RootInit     bool   `json:"root_init"`
	DatabaseType string `json:"database_type"`
}

type SetupRequest struct {
	Username           string `json:"username"`
	Password           string `json:"password"`
	ConfirmPassword    string `json:"confirmPassword"`
	SelfUseModeEnabled bool   `json:"SelfUseModeEnabled"`
	DemoSiteEnabled    bool   `json:"DemoSiteEnabled"`
}

func GetSetup(c contract.Context) {
	setup := Setup{
		Status: constant.Setup,
	}
	if constant.Setup {
		_ = c.JSON(http.StatusOK, common.H{
			"success": true,
			"data":    setup,
		})
		return
	}
	setup.RootInit = identity.RootUserExists()
	setup.DatabaseType = string(common.MainDatabaseType())
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data":    setup,
	})
}

func PostSetup(c contract.Context) {
	// Check if setup is already completed
	if constant.Setup {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "系统已经初始化完成",
		})
		return
	}

	// Check if root user already exists
	rootExists := identity.RootUserExists()

	var req SetupRequest
	err := c.BindJSON(&req)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "请求参数有误",
		})
		return
	}

	// Validate username length: max 12 characters to align with identity.User validation
	if !rootExists {
		if len(req.Username) > 12 {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "用户名长度不能超过12个字符",
			})
			return
		}
		// Validate password
		if req.Password != req.ConfirmPassword {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "两次输入的密码不一致",
			})
			return
		}

		if len(req.Password) < 8 {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "密码长度至少为8个字符",
			})
			return
		}
	}

	// Set operation modes
	channel.SelfUseModeEnabled = req.SelfUseModeEnabled
	channel.DemoSiteEnabled = req.DemoSiteEnabled

	// Save operation modes to database for persistence (idempotent upserts,
	// safe to run before the account transaction)
	err = dbinfra.UpdateOption("SelfUseModeEnabled", setupBoolToString(req.SelfUseModeEnabled))
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "保存自用模式设置失败: " + err.Error(),
		})
		return
	}

	err = dbinfra.UpdateOption("DemoSiteEnabled", setupBoolToString(req.DemoSiteEnabled))
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "保存演示站点模式设置失败: " + err.Error(),
		})
		return
	}

	// Complete setup inside one transaction. The fixed-ID (ID=1) setup row
	// insert is the serialization point: a concurrent second request blocks on
	// the primary key and fails with a duplicate-key error, rolling back its
	// half-created root user. The row-count check covers legacy rows.
	setupErr := dbx.DB.Transaction(func(tx *gorm.DB) error {
		var setupCount int64
		if err := tx.Model(&dbinfra.Setup{}).Count(&setupCount).Error; err != nil {
			return err
		}
		if setupCount > 0 {
			return errSetupAlreadyDone
		}
		setup := dbinfra.Setup{
			ID:            1,
			Version:       common.Version,
			InitializedAt: time.Now().Unix(),
		}
		if err := tx.Create(&setup).Error; err != nil {
			return err
		}
		if !rootExists {
			// Create root user
			hashedPassword, err := common.Password2Hash(req.Password)
			if err != nil {
				return err
			}
			rootUser := identity.User{
				Username:    req.Username,
				Password:    hashedPassword,
				Role:        common.RoleRootUser,
				Status:      common.UserStatusEnabled,
				DisplayName: "Root User",
				AccessToken: nil,
				Quota:       100000000,
			}
			return tx.Create(&rootUser).Error
		}
		return nil
	})
	if errors.Is(setupErr, errSetupAlreadyDone) {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "系统已经初始化完成",
		})
		return
	}
	if setupErr != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "创建管理员账号失败: " + setupErr.Error(),
		})
		return
	}

	constant.Setup = true

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "系统初始化成功",
	})
}

func setupBoolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
