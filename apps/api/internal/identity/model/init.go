package model

import (
	rootmodel "github.com/QuantumNous/new-api/model"
)

// 把所有本域 GORM 实体注册到顶层 model 包的 AutoMigrate 注册表。
// 仅追加实体类型，运行期由 GORM 去重；不要在这里做业务逻辑。
func init() {
	rootmodel.RegisterEntities(
		&User{},
		&Token{},
		&UserSession{},
		&AuthFlow{},
		&ExternalIdentityClaim{},
		&PasskeyCredential{},
		&TwoFA{},
		&TwoFABackupCode{},
		&CustomOAuthProvider{},
		&UserOAuthBinding{},
		&CasbinRule{},
		&AuthzRole{},
	)

	// AutoMigrate 之后的初始化：用户 auth 版本缓存、外部身份绑定初值。
	rootmodel.RegisterPostMigrateHook(func() error {
		if err := InitializeUserAuthVersions(); err != nil {
			return err
		}
		return InitializeExternalIdentityClaims()
	})
}
