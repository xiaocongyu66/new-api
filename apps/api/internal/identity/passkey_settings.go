package identity

type PasskeySettings struct {
	Enabled              bool   `json:"enabled"`
	RPDisplayName        string `json:"rp_display_name"`
	RPID                 string `json:"rp_id"`
	Origins              string `json:"origins"`
	AllowInsecureOrigin  bool   `json:"allow_insecure_origin"`
	UserVerification     string `json:"user_verification"`
	AttachmentPreference string `json:"attachment_preference"`
}

// OnGetPasskeySettings 由 internal/security 在 init() 中注册，用于跨包读取 Passkey 设置。
// identity 只持有类型，设置实例与配置注册仍归 security 所有。
var OnGetPasskeySettings func() *PasskeySettings

// fallbackPasskeySettings 在 hook 未注册时（例如未加载 security 的单元测试）作为持久实例返回，
// 调用方通过返回的指针改写设置的用法因此仍然有效。
var fallbackPasskeySettings PasskeySettings

func GetPasskeySettings() *PasskeySettings {
	if OnGetPasskeySettings != nil {
		return OnGetPasskeySettings()
	}
	return &fallbackPasskeySettings
}
