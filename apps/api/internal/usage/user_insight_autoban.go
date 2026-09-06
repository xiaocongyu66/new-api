package usage

import (
	"fmt"
	"html"
	"sync"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
)

// 本文件实现"破甲 + 写代码双风险自动封禁"。
//
// 为什么要求两个条件同时成立：单看破甲分，角色扮演用户使用共享预设时
// 很容易命中 suspect 档，直接封会大面积误伤；而"既在破甲又在写代码"
// 这个组合在真实用户里极少出现，更符合"拿站点资源做自动化滥用"的特征。
// 这仍然是启发式判断，因此封禁后会发邮件告知申诉渠道，且管理员可解封。

// autoBanOnce 保证同一用户在进程生命周期内只触发一次自动封禁流程，
// 避免封禁瞬间并发的请求重复发信。
var autoBanOnce sync.Map

// 自动封禁的命中原因。邮件正文与系统日志都按原因区分措辞——
// 用户收到"检测到破甲"和"用量结构异常"两种通知时，申诉方向完全不同。
const (
	autoBanReasonJailbreakCode = "jailbreak_code"
	autoBanReasonCodeRatio     = "code_ratio"
)

// EvaluateInsightAutoBan 在画像更新后检查是否达到自动封禁条件。
// 该函数由 attachInsight 在写完画像后调用，失败只记录日志不影响计费。
func EvaluateInsightAutoBan(userId int, username string) {
	setting := GetUserInsightSetting()
	if !setting.AutoBanEnabled && !setting.AutoBanCodeRatioEnabled {
		return
	}
	if userId <= 0 {
		return
	}
	if _, loaded := autoBanOnce.LoadOrStore(userId, struct{}{}); loaded {
		return
	}

	profile, err := GetUserInsightProfile(userId)
	if err != nil || profile == nil {
		// 读不到画像时把标记撤回，下次请求再判，避免因一次抖动永久跳过。
		autoBanOnce.Delete(userId)
		return
	}
	reason := insightAutoBanReason(profile, setting)
	if reason == "" {
		autoBanOnce.Delete(userId)
		return
	}

	user, err := identity.GetUserById(userId, false)
	if err != nil || user == nil {
		autoBanOnce.Delete(userId)
		return
	}
	// 管理员与根用户不自动封禁：误判的代价远大于收益。
	if user.Role >= common.RoleAdminUser {
		return
	}
	if user.Status != common.UserStatusEnabled {
		return
	}

	if err := dbx.DB.Model(&identity.User{}).Where("id = ?", userId).
		Update("status", common.UserStatusDisabled).Error; err != nil {
		common.SysError(fmt.Sprintf("insight auto-ban failed to disable user %d: %s", userId, err.Error()))
		autoBanOnce.Delete(userId)
		return
	}
	// 令牌缓存里仍有该用户的有效令牌，必须失效否则封禁不会立即生效。
	if err := identity.InvalidateUserTokensCache(userId); err != nil {
		common.SysError(fmt.Sprintf("insight auto-ban failed to invalidate tokens for user %d: %s", userId, err.Error()))
	}

	common.SysLog(fmt.Sprintf(
		"insight auto-ban: user=%d username=%s reason=%s risk=%s max_score=%d total_requests=%d code_requests=%d jailbreak(confirmed=%d likely=%d suspect=%d)",
		userId, username, reason, profile.RiskLevel(), profile.JailbreakMaxScore,
		profile.TotalRequests, profile.CodeRequests,
		profile.JailbreakConfirmed, profile.JailbreakLikely, profile.JailbreakSuspect))

	notifyInsightAutoBan(user, reason)
}

// insightAutoBanReason 返回命中的封禁规则，未命中任何规则时返回空串。
//
// 两条规则是 OR 关系且各自有独立开关：
//   - jailbreak_code：破甲风险达阈值 **且** 有代码请求（原有规则）；
//   - code_ratio：写代码请求占比超阈值 **且** 总请求数达门槛，
//     不要求任何破甲信号。
//
// 破甲规则优先判定：两条同时成立时，破甲是更强的处置依据，
// 邮件里也应该告知这个更具体的原因。
func insightAutoBanReason(profile *UserInsightProfile, setting *UserInsightSetting) string {
	if profile == nil || setting == nil {
		return ""
	}
	if setting.AutoBanEnabled && insightAutoBanQualified(profile, setting.AutoBanRiskLevel()) {
		return autoBanReasonJailbreakCode
	}
	if setting.AutoBanCodeRatioEnabled && insightCodeRatioQualified(profile, setting) {
		return autoBanReasonCodeRatio
	}
	return ""
}

// insightAutoBanQualified 判断画像是否同时满足"破甲"与"写代码"两个风险面。
func insightAutoBanQualified(profile *UserInsightProfile, minLevel string) bool {
	if profile == nil {
		return false
	}
	if riskSeverity(profile.RiskLevel()) < riskSeverity(minLevel) {
		return false
	}
	// 写代码这一面要求真的有 code 请求，而不是只看主类别：
	// 破甲用户的角色扮演请求通常远多于代码请求，主类别会是 roleplay。
	return profile.CodeRequests > 0
}

// insightCodeRatioQualified 判断写代码请求占比是否超过阈值。
//
// 用整数乘法比较而不是浮点除法：code/total >= pct/100 等价于
// code*100 >= total*pct，避免浮点比较在边界值（正好等于阈值）上抖动。
func insightCodeRatioQualified(profile *UserInsightProfile, setting *UserInsightSetting) bool {
	if profile == nil || setting == nil {
		return false
	}
	if profile.TotalRequests < setting.CodeRatioMinRequests() {
		return false
	}
	if profile.TotalRequests <= 0 || profile.CodeRequests <= 0 {
		return false
	}
	return profile.CodeRequests*100 >= profile.TotalRequests*setting.CodeRatioThreshold()
}

// notifyInsightAutoBan 给被封禁用户发信。
// 邮箱缺失时只记日志：用户可能是通过 OAuth 注册且未绑定邮箱。
func notifyInsightAutoBan(user *identity.User, reason string) {
	if user == nil || user.Email == "" {
		if user != nil {
			common.SysLog(fmt.Sprintf("insight auto-ban: user %d has no email, skip notification", user.Id))
		}
		return
	}
	subject := fmt.Sprintf("[%s] 账号已被暂停", common.SystemName)
	content := buildAutoBanEmail(user.DisplayName, user.Username, reason)
	if err := common.SendEmail(subject, user.Email, content); err != nil {
		common.SysError(fmt.Sprintf("insight auto-ban failed to send email to user %d: %s", user.Id, err.Error()))
	}
}

// buildAutoBanEmail 生成封禁通知邮件正文。
//
// 样式约束：邮件客户端普遍不支持外链 CSS、CSS 变量与现代色彩函数（oklch），
// 因此这里把站点主题色手写成等价的十六进制内联样式，
// 并用 table 布局保证在 Outlook / QQ 邮箱下也不错版。
func buildAutoBanEmail(displayName, username, reason string) string {
	name := displayName
	if name == "" {
		name = username
	}
	// 用户名会进入 HTML，必须转义，否则昵称里的尖括号可以注入标签。
	safeName := html.EscapeString(name)
	safeSystem := html.EscapeString(common.SystemName)
	// 正文按命中原因分开措辞：告诉用户"检测到破甲"却实际是占比触发，
	// 会让用户无从申诉，也会让管理员收到答不上来的质疑。
	explanation := "我们的预警机制检测到您的账号进行了破甲操作，出于安全考虑，我们暂停了您的账号。如有疑问，请在官方群向管理员申请解封。"
	if reason == autoBanReasonCodeRatio {
		explanation = "我们的预警机制检测到您的账号用量结构异常（代码类请求占比显著偏高），出于站点资源公平使用的考虑，我们暂停了您的账号。如果这是正常使用，请在官方群向管理员说明情况申请解封。"
	}

	return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + safeSystem + ` 账号状态通知</title>
</head>
<body style="margin:0;padding:0;background-color:#f5f7fa;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Hiragino Sans GB','Microsoft YaHei',sans-serif;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background-color:#f5f7fa;padding:24px 12px;">
<tr>
<td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:560px;background-color:#ffffff;border-radius:12px;overflow:hidden;box-shadow:0 2px 8px rgba(16,24,40,0.06);">

<tr>
<td style="background-color:#3b9ae1;padding:28px 32px;">
<div style="color:#ffffff;font-size:20px;font-weight:600;letter-spacing:0.5px;">` + safeSystem + `</div>
<div style="color:#e3f0fb;font-size:13px;margin-top:6px;">账号安全通知</div>
</td>
</tr>

<tr>
<td style="padding:32px;">
<p style="margin:0 0 18px;color:#101828;font-size:16px;font-weight:600;">亲爱的用户 ` + safeName + `，您好</p>
<p style="margin:0 0 16px;color:#475467;font-size:14px;line-height:1.75;">
` + explanation + `
</p>

<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:24px 0 8px;background-color:#f0f7fd;border-left:4px solid #3b9ae1;border-radius:6px;">
<tr>
<td style="padding:14px 16px;color:#3a6b8f;font-size:13px;line-height:1.7;">
账号当前状态：<strong style="color:#1d4d70;">已暂停</strong><br>
恢复方式：在官方群联系管理员申请解封
</td>
</tr>
</table>
</td>
</tr>

<tr>
<td style="padding:20px 32px 28px;border-top:1px solid #eaecf0;">
<p style="margin:0;color:#98a2b3;font-size:12px;line-height:1.6;">
本邮件由系统自动发送，请勿直接回复。<br>
` + html.EscapeString(common.Footer) + `
</p>
</td>
</tr>

</table>
</td>
</tr>
</table>
</body>
</html>`
}

// ResetInsightAutoBanMark 在管理员解封用户时清除标记，
// 否则用户解封后即便再次触发也不会重新封禁。
func ResetInsightAutoBanMark(userId int) {
	autoBanOnce.Delete(userId)
}
