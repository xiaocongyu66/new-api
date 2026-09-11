package usage

import (
	"errors"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/dbinfra"
	"github.com/QuantumNous/new-api/internal/settings"
)

// 本文件实现"客户端封禁"的存储与判定。
//
// 请求来自哪个客户端由画像模块按请求头识别（User-Agent + 工具专属头，
// 见 insight.DetectClient）。封禁分两级，都只在 user_insight_setting 的
// client_ban_enabled 总开关打开时生效，执行点在 relay 中间件：
//   - 全局：user_insight_setting.blocked_clients，封掉全站该客户端的请求；
//   - 单用户：user_insight_client_bans 表，只封某个用户的该客户端，
//     该用户换其它客户端仍可正常使用。
//
// 单用户档独立成表而不是挂在画像聚合行上：清除画像不该顺带撤销封禁
// 指令，且没有画像行的用户也要能被封。

// UserInsightClientBan 记录"某用户禁用的客户端"。
// (user_id, client) 联合主键保证同一对用户至多一行，启用/撤销都是幂等操作。
type UserInsightClientBan struct {
	UserId    int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Client    string `json:"client" gorm:"size:64;primaryKey"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;default:0"`
}

func (UserInsightClientBan) TableName() string {
	return "user_insight_client_bans"
}

// maxBlockedClients 限制全局封禁列表的长度：这是运营方配置项而不是
// 用户输入，上限防止误配置把每个请求变成对超大列表的全扫描。
const maxBlockedClients = 200

// maxClientIDLength 与数据库列宽一致：超长串既放不下也永远匹配不上。
const maxClientIDLength = 64

// blockedClientsLock 保护 userInsightSetting.BlockedClients 的读改写：
// options 保存链路（管理后台）与本文件的 SetGlobalClientBan 会并发替换该切片。
var blockedClientsLock sync.RWMutex

// SanitizeClientID 归一化客户端标识：去首尾空白，拒绝空串与超长值。
// 标识会参与每请求匹配，只接受短串。
func SanitizeClientID(client string) (string, error) {
	client = strings.TrimSpace(client)
	if client == "" {
		return "", errors.New("client is empty")
	}
	if len(client) > maxClientIDLength {
		return "", errors.New("client is too long")
	}
	return client, nil
}

// GetBlockedClientList 返回全局封禁列表的拷贝，供接口回显。
func GetBlockedClientList() []string {
	blockedClientsLock.RLock()
	defer blockedClientsLock.RUnlock()
	list := make([]string, len(userInsightSetting.BlockedClients))
	copy(list, userInsightSetting.BlockedClients)
	return list
}

// SetGlobalClientBan 增删全局封禁列表里的一个客户端。
// 内存更新统一走 settings.ApplyOption → OnApplyUserInsightSetting 钩子
// （在 blockedClientsLock 内应用），与 admin 后台保存、option 周期同步共用
// 同一条加锁写路径——否则反射直写会与 relay 热路径上的 CheckClientBan 竞争
// （slice header 撕裂，-race 可复现）。
// 注意：本函数不把锁保持到 UpdateOption 全程（那会让 relay 每请求的
// CheckClientBan 为一次 DB 写阻塞），因此并发管理员互斥切换是
// last-write-wins，多实例经 option 同步收敛——管理员操作低频，可接受。
// 入口归一化不依赖调用方：这个函数也会被测试与非 handler 代码直接调用。
func SetGlobalClientBan(client string, ban bool) error {
	client, err := SanitizeClientID(client)
	if err != nil {
		return err
	}
	blockedClientsLock.RLock()
	current := userInsightSetting.BlockedClients
	blockedClientsLock.RUnlock()

	list := make([]string, 0, len(current)+1)
	seen := make(map[string]bool, len(current)+1)
	for _, existing := range current {
		if existing == client || seen[existing] {
			continue
		}
		seen[existing] = true
		list = append(list, existing)
	}
	if ban && len(list) < maxBlockedClients {
		list = append(list, client)
	}

	value, err := common.Marshal(list)
	if err != nil {
		return err
	}
	// 写库成功后 ApplyOption → 钩子持锁应用内存；写库失败时钩子不运行，
	// 内存保持原状，无需回滚（比"先改内存再回滚"更不易漂移）。
	if err := dbinfra.UpdateOption("user_insight_setting.blocked_clients", string(value)); err != nil {
		return err
	}
	return nil
}

// applyUserInsightSetting 处理 settings.ApplyOption 派发的
// user_insight_setting.<key> 分层配置。只有 blocked_clients 需要拦截：
// 它在 relay 热路径上被 CheckClientBan 在 blockedClientsLock 下读取，
// 通用反射写（settings.updateConfigFromMap）不持该锁，会造成数据竞争。
// 返回 true 表示已处理；其它键返回 false 走通用反射路径。
func applyUserInsightSetting(configKey, value string) bool {
	if configKey != "blocked_clients" {
		return false
	}
	var list []string
	if err := common.Unmarshal([]byte(value), &list); err != nil {
		return false // 解析失败：交给通用路径（同样会失败/跳过）
	}
	blockedClientsLock.Lock()
	userInsightSetting.BlockedClients = list
	blockedClientsLock.Unlock()
	return true
}

func init() {
	settings.OnApplyUserInsightSetting = applyUserInsightSetting
}

// ToggleUserClientBan 启用/撤销单用户档的客户端封禁。
// 入口归一化不依赖调用方：这个函数也会被测试与非 handler 代码直接调用。
func ToggleUserClientBan(userId int, client string, ban bool) error {
	if userId <= 0 {
		return errors.New("invalid userId")
	}
	client, err := SanitizeClientID(client)
	if err != nil {
		return err
	}
	if ban {
		record := UserInsightClientBan{UserId: userId, Client: client, CreatedAt: common.GetTimestamp()}
		err := dbx.DB.Where("user_id = ? AND client = ?", userId, client).
			FirstOrCreate(&record).Error
		if err != nil && !isDuplicateKeyError(err) {
			return err
		}
	} else {
		if err := dbx.DB.Where("user_id = ? AND client = ?", userId, client).
			Delete(&UserInsightClientBan{}).Error; err != nil {
			return err
		}
	}
	invalidateUserClientBanCache(userId)
	return nil
}

// CheckClientBan 返回一次请求命中的封禁级别：
// "" 未封禁；"global" 命中全局封禁列表；"user" 命中单用户封禁。
// client 为空（请求头识别不出客户端）时不封：封禁名单匹配的是
// 识别结果，识别不出就不该动。
// 全局列表读锁只覆盖切片头的拷贝——SetGlobalClientBan 每次都整体
// 替换切片而不是原地改写，拷贝出的切片头在锁外遍历是安全的。
func CheckClientBan(userId int, client string) string {
	if client == "" {
		return ""
	}
	blockedClientsLock.RLock()
	globalList := userInsightSetting.BlockedClients
	blockedClientsLock.RUnlock()
	for _, blocked := range globalList {
		if blocked == client {
			return "global"
		}
	}
	if userId > 0 && GetUserClientBanSet(userId)[client] {
		return "user"
	}
	return ""
}

// GetUserClientBansByUsers 批量返回单用户封禁列表，供看板视图补齐
// "已禁用客户端"标记。无记录的用户不出现在结果里。
func GetUserClientBansByUsers(ids []int) map[int][]string {
	if len(ids) == 0 {
		return nil
	}
	var rows []UserInsightClientBan
	if err := dbx.DB.Where("user_id IN ?", ids).Find(&rows).Error; err != nil {
		return nil
	}
	result := make(map[int][]string, len(rows))
	for _, row := range rows {
		result[row.UserId] = append(result[row.UserId], row.Client)
	}
	return result
}

// userClientBanCacheTTL：单用户封禁状态只在管理员操作时变化，
// 最多 30 秒的生效延迟可接受，换来每请求免一次数据库点查。
// 单位必须是秒——过期判定拿它去加 common.GetTimestamp()（秒），
// 写成 time.Second 的数值会把过期时间推到天文数字，缓存永不失效，
// 多实例部署时封禁变更无法收敛到其他实例。
const userClientBanCacheTTL = int64(30)

type userClientBanCacheEntry struct {
	set       map[string]bool
	expiresAt int64
}

var (
	userClientBanCache     = make(map[int]*userClientBanCacheEntry)
	userClientBanCacheLock sync.Mutex
)

// GetUserClientBanSet 返回某用户被禁用的客户端集合，走 TTL 缓存读穿。
// 没有记录的用户返回空集合，缓存住这个事实避免反复查库。
// 查询失败时沿用旧缓存（没有旧缓存则放行）：封禁是风控手段，
// 数据库抖动时宁可漏封不可把全站 403。
func GetUserClientBanSet(userId int) map[string]bool {
	if userId <= 0 {
		return map[string]bool{}
	}
	now := common.GetTimestamp()
	userClientBanCacheLock.Lock()
	defer userClientBanCacheLock.Unlock()

	entry := userClientBanCache[userId]
	if entry != nil && entry.expiresAt > now {
		return entry.set
	}
	set, err := loadUserClientBanSet(userId)
	if err != nil {
		if entry != nil {
			return entry.set
		}
		set = map[string]bool{}
	}
	userClientBanCache[userId] = &userClientBanCacheEntry{set: set, expiresAt: now + userClientBanCacheTTL}
	// 缓存表随活跃用户数增长，触顶整体重建防止无界膨胀。
	if len(userClientBanCache) >= 4096 {
		rebuilt := make(map[int]*userClientBanCacheEntry, 1)
		rebuilt[userId] = userClientBanCache[userId]
		userClientBanCache = rebuilt
	}
	return set
}

func loadUserClientBanSet(userId int) (map[string]bool, error) {
	var rows []UserInsightClientBan
	if err := dbx.DB.Where("user_id = ?", userId).Find(&rows).Error; err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(rows))
	for _, row := range rows {
		set[row.Client] = true
	}
	return set, nil
}

// invalidateUserClientBanCache 在封禁状态变更后立即失效，下一请求即生效。
func invalidateUserClientBanCache(userId int) {
	userClientBanCacheLock.Lock()
	delete(userClientBanCache, userId)
	userClientBanCacheLock.Unlock()
}

// isDuplicateKeyError 按错误消息关键字识别三种数据库的主键/唯一键冲突：
// MySQL "Duplicate entry"、PostgreSQL "duplicate key"、SQLite "UNIQUE constraint failed"。
// 幂等写入遇到并发重复时按成功处理。
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "Duplicate entry") ||
		strings.Contains(message, "duplicate key") ||
		strings.Contains(message, "UNIQUE constraint failed")
}
