package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/geoip"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/security"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// geoCountryLookup resolves a client IP to its ISO 3166-1 country code.
// A package variable so tests can simulate a blocked region without shipping
// an MMDB file; production always uses the MaxMind-backed implementation.
var geoCountryLookup = geoip.LookupCountry

// geoGateShellPrefixes are served to everyone, including blocked regions,
// because sign-in and the 404 page cannot work without them:
//   - /assets/* and /favicon* are the SPA bundle and icon (inert static files),
//   - /api/status is the public configuration the SPA boots with,
//   - /api/user/login* and /api/oauth* are the sign-in flows themselves.
//
// The trade-off is deliberate: an administrator inside a blocked region must be
// able to sign in, and identity is unknowable before sign-in, so the sign-in
// surface stays reachable while every data surface stays blocked.
var geoGateShellPrefixes = []string{
	"/assets/",
	"/favicon",
	"/api/user/login",
	"/api/oauth",
}

// geoGateShellExactPaths are the SPA shell routes for logging in and for the
// block landing page, plus the public bootstrap endpoint the SPA boots with
// (exact match: /api/status/test is an admin endpoint and stays blocked).
var geoGateShellExactPaths = map[string]struct{}{
	"/404":        {},
	"/sign-in":    {},
	"/api/status": {},
}

// geoGateAPIPrefixes are answered with a 404 JSON body when blocked; everything
// else is a web page and is redirected to the SPA 404 route.
var geoGateAPIPrefixes = []string{"/api", "/mj", "/pg", "/v1"}

// geoGateVerdictTTL bounds how long an administrator-credential verdict is
// reused. Resolving a credential reads the session/user store (a database
// round-trip when Redis is disabled), and a blocked region can send many
// requests, so the gate remembers the answer briefly instead of resolving on
// every request.
const geoGateVerdictTTL = time.Minute

type geoGateVerdict struct {
	allowed bool
	at      time.Time
}

var (
	geoGateCacheMu sync.RWMutex
	geoGateCache   = make(map[string]geoGateVerdict)
)

// GeoBlock 按请求 IP 的国家执行地理封禁：总开关
// geo_block_setting.enabled 打开且 IP 归属国在 blocked_countries 列表里时拒绝。
// 挂在引擎层（所有路由之前），对 Web、API、静态资源全部生效。
//
// 拒绝是"隐形"的：不告诉访问者"你被地区封禁了"，而是让请求看起来就像访问了
// 一个不存在的页面 —— Web 请求 302 到前端 /404 路由，API 请求 404 JSON。
// （302 而非 404+Location：客户端只在 3xx 上跟随 Location，404 上的 Location 会被忽略。）
//
// geo_block_setting.allow_admin（默认开）让携带后台管理员凭证的请求通过，
// 运营者即使身处被封禁地区也能登录并管理站点；普通用户与访问者不受影响。
//
// 判定依赖本机 MMDB 数据库（GEOIP_DB_PATH 环境变量指定文件路径），
// 数据库不可用（路径未配置或文件损坏）时本中间件对每个请求都直接放行
//（fail-open）：缺库导致全站 404 是不可接受的，而"没拦住"只是损失封禁
// 效果。因此部署方在打开开关前应确认 GEOIP_DB_PATH 指向有效文件。
func GeoBlock() contract.Middleware {
	return func(c contract.Context) {
		setting := geoip.GetGeoBlockSetting()
		if !setting.Enabled {
			c.Next()
			return
		}

		clientIP := c.ClientIP()
		country := geoCountryLookup(clientIP)
		if country == "" || !setting.BlocksCountry(country) {
			c.Next()
			return
		}

		path := c.HTTPRequest().URL.Path

		// 登录流程与 404 页面本身必须可达，否则管理员在被封禁地区无法登录。
		if geoGateShellAllowed(path) {
			c.Next()
			return
		}

		if setting.AllowAdmin && geoGateAdminCredential(c) {
			c.Next()
			return
		}

		rejectGeoBlocked(c, clientIP, path)
	}
}

// geoGateShellAllowed reports whether the path belongs to the sign-in / 404
// shell that stays reachable inside a blocked region.
func geoGateShellAllowed(path string) bool {
	if _, ok := geoGateShellExactPaths[path]; ok {
		return true
	}
	for _, prefix := range geoGateShellPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// geoGateAdminCredential reports whether the request carries an administrator's
// dashboard credential, reusing a short-lived verdict when one exists.
func geoGateAdminCredential(c contract.Context) bool {
	credential := c.Header("Authorization")
	if credential == "" {
		return false
	}
	// The cache key is the credential hash, never the credential itself.
	sum := sha256.Sum256([]byte(credential))
	key := hex.EncodeToString(sum[:])

	now := time.Now()
	geoGateCacheMu.RLock()
	verdict, ok := geoGateCache[key]
	geoGateCacheMu.RUnlock()
	if ok && now.Sub(verdict.at) < geoGateVerdictTTL {
		return verdict.allowed
	}

	allowed := security.GeoGateRole(c) >= common.RoleAdminUser

	geoGateCacheMu.Lock()
	// Overflow is rare (4096-entry cap): drop expired verdicts first, so a
	// blocked region spraying one-shot garbage credentials cannot push long
	// lived hot entries out; a full reset only happens when every entry is
	// still fresh. Lost verdicts just cost one credential resolution on their
	// next request.
	if len(geoGateCache) > 4096 {
		cutoff := now.Add(-geoGateVerdictTTL)
		for k, v := range geoGateCache {
			if v.at.Before(cutoff) {
				delete(geoGateCache, k)
			}
		}
	}
	if len(geoGateCache) > 4096 {
		geoGateCache = map[string]geoGateVerdict{key: {allowed: allowed, at: now}}
	} else {
		geoGateCache[key] = geoGateVerdict{allowed: allowed, at: now}
	}
	geoGateCacheMu.Unlock()
	return allowed
}

// rejectGeoBlocked 以 404 语义拒绝命中地理封禁的请求并留审计日志。
// 审计日志照实记录 "geo block"，管理员可查；访问者看到的只是一次"页面不存在"。
func rejectGeoBlocked(c contract.Context, clientIP, path string) {
	req := c.HTTPRequest()
	logger.LogWarn(c.Context(), fmt.Sprintf(
		"geo block rejected: client_ip=%s path=%s method=%s",
		clientIP, path, req.Method,
	))

	if geoGateAPIPath(path) {
		c.AbortWithStatusJSON(http.StatusNotFound, common.H{
			"success": false,
			"message": "page not found",
		})
		return
	}

	// Web 页面：302 到前端 /404 路由（该路径对封禁地区豁免），
	// 访问者看到站点自己的"页面不存在"页面。
	c.Redirect(http.StatusFound, "/404")
	c.Abort()
}

// geoGateAPIPath classifies a path as an API call (404 JSON) as opposed to a
// web page (redirect to the SPA 404 route).
func geoGateAPIPath(path string) bool {
	for _, prefix := range geoGateAPIPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
