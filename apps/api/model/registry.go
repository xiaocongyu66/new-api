package model

import (
	"fmt"
	"sync"

	"gorm.io/gorm"
)

// AutoMigrate 实体注册表。每个 internal/<domain>/model 包在自己的 init()
// 中通过 RegisterEntities 把实体注册进来，避免 model/main.go 反向 import
// 各 domain 包（那会形成 import cycle）。

// 实体注册表分两组：
//   - 主库实体（主数据库迁移时使用）
//   - 日志库实体（log database 迁移时使用，可能与主库不同的方言，例如 ClickHouse）
var (
	registryMu     sync.Mutex
	registeredEnts []any
	logRegistryMu  sync.Mutex
	logEnts        []any
)

// RegisterEntities 把需要 AutoMigrate 的主库 GORM 实体追加到全局注册表。
func RegisterEntities(entities ...any) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registeredEnts = append(registeredEnts, entities...)
}

// RegisteredEntities 返回当前已注册的主库实体快照。
func RegisteredEntities() []any {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := make([]any, len(registeredEnts))
	copy(out, registeredEnts)
	return out
}

// RegisteredEntitiesCapacity 返回已注册主库实体的数量，供迁移切片预分配。
func RegisteredEntitiesCapacity() int {
	registryMu.Lock()
	defer registryMu.Unlock()
	return len(registeredEnts)
}

// RegisterLogEntities 把需要 AutoMigrate 的日志库实体追加到日志库注册表。
// 例如使用 ClickHouse 作为日志库时，业务实体仍走主库注册表，仅 Log 等日志
// 表进入日志库。
func RegisterLogEntities(entities ...any) {
	logRegistryMu.Lock()
	defer logRegistryMu.Unlock()
	logEnts = append(logEnts, entities...)
}

// RegisteredLogEntities 返回当前已注册的日志库实体快照。
func RegisteredLogEntities() []any {
	logRegistryMu.Lock()
	defer logRegistryMu.Unlock()
	out := make([]any, len(logEnts))
	copy(out, logEnts)
	return out
}

// 启动期钩子分两层：

//   - postMigrateHooks：在 model.InitDB → migrateDB 末尾、AutoMigrate 完成后
//     同步执行。适合依赖 schema 已有版本的初始化（默认账号、auth 缓存等）。
//   - startupHooks：在 model.CheckSetup 触发，按注册顺序串行执行。适合依赖
//     postMigrate 数据的逻辑（setup 记录写入等）。
//
// 两层都允许 panic recover 兜底，避免单个钩子失败中断启动。

var (
	postMigrateMu    sync.Mutex
	postMigrateHooks []func() error
	startupMu        sync.Mutex
	startupHooks     []func()
)

// RegisterPostMigrateHook 注册一个 AutoMigrate 完成后的钩子。返回错误会
// 中断迁移流程并向上传播。
func RegisterPostMigrateHook(hook func() error) {
	postMigrateMu.Lock()
	defer postMigrateMu.Unlock()
	postMigrateHooks = append(postMigrateHooks, hook)
}

// RunPostMigrateHooks 按注册顺序执行所有迁移后钩子。任一钩子返回错误立即
// 终止并返回该错误。
func RunPostMigrateHooks() error {
	postMigrateMu.Lock()
	hooks := append([]func() error{}, postMigrateHooks...)
	postMigrateMu.Unlock()
	for _, hook := range hooks {
		if err := safeRunPost(hook); err != nil {
			return err
		}
	}
	return nil
}

func safeRunPost(hook func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("post-migrate hook panic: %v", r)
		}
	}()
	return hook()
}

var preMigrateMu sync.Mutex
var preMigrateHooks []func() error

// RegisterPreMigrateHook 注册一个 AutoMigrate 之前的钩子（适合处理
// 不属于 AutoMigrate 范畴的列类型微调，例如 MySQL decimal/PostgreSQL text）。
func RegisterPreMigrateHook(hook func() error) {
	preMigrateMu.Lock()
	defer preMigrateMu.Unlock()
	preMigrateHooks = append(preMigrateHooks, hook)
}

// RunPreMigrateHooks 按注册顺序执行所有迁移前钩子。任一钩子返回错误立即
// 终止并返回该错误。
func RunPreMigrateHooks() error {
	preMigrateMu.Lock()
	hooks := append([]func() error{}, preMigrateHooks...)
	preMigrateMu.Unlock()
	for _, hook := range hooks {
		if err := safeRunPost(hook); err != nil {
			return err
		}
	}
	return nil
}

// RegisterStartupHook 注册一个 CheckSetup 阶段的钩子。domain 包在 init()
// 中调用，避免 model 反向依赖具体业务包。
func RegisterStartupHook(hook func()) {
	startupMu.Lock()
	defer startupMu.Unlock()
	startupHooks = append(startupHooks, hook)
}

// 批量更新处理器接口：domain 包在 init() 中通过 RegisterBatchUpdater 提供
// 具体实现，model.utils 中的 batchUpdate 只负责调度，避免顶层 model 反向
// 依赖具体业务包。
type BatchUpdater interface {
	IncreaseTokenQuota(id int, delta int) error
	UpdateChannelUsedQuota(id int, delta int)
	UpdateUserQuotaUsedQuotaAndRequestCount(userId int, quota int, usedQuota int, requestCount int) error
}

var batchUpdater BatchUpdater
// GatewayRoutingMutatorFn 描述 gateway routing 表的变更入口。catalog/model
// 在 init() 中赋值；model.option.go 调用它把 options 表的写入纳入 gateway
// revision 事务。
type GatewayRoutingMutatorFn func(fn func(tx *gorm.DB) error) error

var gatewayRoutingMutator GatewayRoutingMutatorFn
// billing_setting 变更后清空定价缓存。
type PricingCacheInvalidatorFn func()

var pricingCacheInvalidator PricingCacheInvalidatorFn

// SetGatewayRoutingMutator 设置 gateway routing 变更入口（由
// internal/catalog/model 调用）。
func SetGatewayRoutingMutator(fn GatewayRoutingMutatorFn) {
	gatewayRoutingMutator = fn
}

// SetPricingCacheInvalidator 设置定价缓存失效入口（由
// internal/catalog/model 调用）。
func SetPricingCacheInvalidator(fn PricingCacheInvalidatorFn) {
	pricingCacheInvalidator = fn
}

// RegisterBatchUpdater 注册一个批量更新处理器。多次注册以最后一次为准。
func RegisterBatchUpdater(u BatchUpdater) {
	batchUpdater = u
}

// BatchUpdaterOf 返回当前已注册的批量更新处理器；未注册时返回 nil。
func BatchUpdaterOf() BatchUpdater {
	return batchUpdater
}

// UserResolver 用与 identity/model 在 init() 中赋值；供跨域包
// （usage/model）查询用户名 / 用户设置时使用，避免反向依赖。
type UserResolver interface {
	GetUsernameById(id int, fromDB bool) (string, error)
	GetUserSetting(id int, fromDB bool) (map[string]any, error)
}

var userResolver UserResolver

// RegisterUserResolver 注册一个用户信息查询器。多次注册以最后一次为准。
func RegisterUserResolver(u UserResolver) {
	userResolver = u
}

// UserResolverOf 返回当前已注册的用户信息查询器；未注册时返回 nil。
func UserResolverOf() UserResolver {
	return userResolver
}

// TokenResolver 由 identity/model 在 init() 中赋值；供跨域包
// （usage/model）按 id 查询 token 时使用，避免反向依赖。
type TokenResolver interface {
	GetTokenById(id int) (name string, ok bool)
}

var tokenResolver TokenResolver

// RegisterTokenResolver 注册 token 查询器；多次注册以最后一次为准。
func RegisterTokenResolver(t TokenResolver) { tokenResolver = t }

// TokenResolverOf 返回当前已注册的 token 查询器；未注册时返回 nil。
func TokenResolverOf() TokenResolver { return tokenResolver }

// ChannelResolver 由 catalog/model 在 init() 中赋值；供跨域包
// （usage/model）按 id 查询 channel 时使用，避免反向依赖。
type ChannelResolver interface {
	CacheGetChannel(id int) (name string, ok bool)
}

var channelResolver ChannelResolver

// RegisterChannelResolver 注册 channel 查询器；多次注册以最后一次为准。
func RegisterChannelResolver(c ChannelResolver) { channelResolver = c }

// ChannelResolverOf 返回当前已注册的 channel 查询器；未注册时返回 nil。
func ChannelResolverOf() ChannelResolver { return channelResolver }

func RunStartupHooks() {
	startupMu.Lock()
	hooks := append([]func(){}, startupHooks...)
	startupMu.Unlock()
	for _, hook := range hooks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("[startup hook] panic recovered: %v\n", r)
				}
			}()
			hook()
		}()
	}
}
