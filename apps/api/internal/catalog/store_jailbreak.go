package channel

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"gorm.io/gorm"
)

// Rule kinds. Prompt rules rewrite the system prompt; header rules rewrite
// the upstream request headers (same JSON shape as the channel
// header_override setting).
const (
	JailbreakKindPrompt = "prompt"
	JailbreakKindHeader = "header"
)

// Jailbreak binding targets. A profile can target channels, models, or both;
// the same profile id is deduplicated at match time so binding it twice still
// injects the prompt once.
const (
	JailbreakTargetChannel = "channel"
	JailbreakTargetModel   = "model"
)

// Injection modes. Prepend/append compose with the existing system prompt;
// replace discards it, mirroring the channel setting's override flag.
const (
	JailbreakModePrepend = "prepend"
	JailbreakModeAppend  = "append"
	JailbreakModeReplace = "replace"
)

// JailbreakProfile is the operator-managed prompt override. Bindings live in
// JailbreakBinding so the relay hot path can index by channel/model.
type JailbreakProfile struct {
	Id            int            `json:"id"`
	Name          string         `json:"name" gorm:"size:64;not null;uniqueIndex:uk_jailbreak_name,where:deleted_at IS NULL"`
	Description   string         `json:"description,omitempty" gorm:"type:varchar(255)"`
	Kind          string         `json:"kind" gorm:"size:16"`
	Prompt        string         `json:"prompt" gorm:"type:text"`
	Headers       string         `json:"headers" gorm:"type:text"`
	ChannelModels string         `json:"channel_models" gorm:"type:text"`
	Mode          string         `json:"mode" gorm:"size:16;not null"`
	Priority      int            `json:"priority" gorm:"not null"`
	Force         bool           `json:"force" gorm:"not null"`
	Enabled       bool           `json:"enabled" gorm:"not null"`
	CreatedTime   int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime   int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt     gorm.DeletedAt `json:"-" gorm:"index"`
}

func (JailbreakProfile) TableName() string {
	return "jailbreak_profiles"
}

type JailbreakBinding struct {
	Id         int    `json:"id"`
	ProfileId  int    `json:"profile_id" gorm:"not null;uniqueIndex:uk_jailbreak_binding,priority:1"`
	TargetType string `json:"target_type" gorm:"size:16;not null;uniqueIndex:uk_jailbreak_binding,priority:2"`
	TargetKey  string `json:"target_key" gorm:"size:128;not null;uniqueIndex:uk_jailbreak_binding,priority:3"`
}

func (JailbreakBinding) TableName() string {
	return "jailbreak_bindings"
}

func IsJailbreakNameDuplicated(id int, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	var cnt int64
	err := dbx.DB.Model(&JailbreakProfile{}).
		Where("name = ? AND id <> ?", name, id).
		Count(&cnt).Error
	return cnt > 0, err
}

func ListJailbreakProfiles() ([]JailbreakProfile, error) {
	var profiles []JailbreakProfile
	err := dbx.DB.Order("updated_time DESC, id DESC").Find(&profiles).Error
	return profiles, err
}

func GetJailbreakProfileById(id int) (*JailbreakProfile, error) {
	var profile JailbreakProfile
	if err := dbx.DB.First(&profile, id).Error; err != nil {
		return nil, err
	}
	return &profile, nil
}

func ListJailbreakBindings() ([]JailbreakBinding, error) {
	var bindings []JailbreakBinding
	err := dbx.DB.Order("id ASC").Find(&bindings).Error
	return bindings, err
}

func ListJailbreakBindingsByProfileIDs(ids []int) ([]JailbreakBinding, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var bindings []JailbreakBinding
	err := dbx.DB.Where("profile_id IN ?", ids).Order("id ASC").Find(&bindings).Error
	return bindings, err
}

// CreateJailbreakProfile inserts the profile and its bindings atomically and
// drops the resolver snapshot so the next relay request sees the change.
func CreateJailbreakProfile(profile *JailbreakProfile, channels []int, models []string) error {
	now := common.GetTimestamp()
	profile.CreatedTime = now
	profile.UpdatedTime = now
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(profile).Error; err != nil {
			return err
		}
		return replaceJailbreakBindings(tx, profile.Id, channels, models)
	})
	if err == nil {
		InvalidateJailbreakRules()
	}
	return err
}

func UpdateJailbreakProfile(profile *JailbreakProfile, channels []int, models []string) error {
	profile.UpdatedTime = common.GetTimestamp()
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(profile).Error; err != nil {
			return err
		}
		return replaceJailbreakBindings(tx, profile.Id, channels, models)
	})
	if err == nil {
		InvalidateJailbreakRules()
	}
	return err
}

func DeleteJailbreakProfileById(id int) error {
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("profile_id = ?", id).Delete(&JailbreakBinding{}).Error; err != nil {
			return err
		}
		return tx.Delete(&JailbreakProfile{}, id).Error
	})
	if err == nil {
		InvalidateJailbreakRules()
	}
	return err
}

func replaceJailbreakBindings(tx *gorm.DB, profileID int, channels []int, models []string) error {
	if err := tx.Where("profile_id = ?", profileID).Delete(&JailbreakBinding{}).Error; err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(channels)+len(models))
	bindings := make([]JailbreakBinding, 0, len(channels)+len(models))
	add := func(targetType, key string) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		if targetType == JailbreakTargetChannel {
			id, err := strconv.Atoi(key)
			if err != nil || id <= 0 {
				return
			}
		}
		dedupeKey := targetType + ":" + key
		if _, ok := seen[dedupeKey]; ok {
			return
		}
		seen[dedupeKey] = struct{}{}
		bindings = append(bindings, JailbreakBinding{
			ProfileId:  profileID,
			TargetType: targetType,
			TargetKey:  key,
		})
	}
	for _, id := range channels {
		add(JailbreakTargetChannel, strconv.Itoa(id))
	}
	for _, model := range models {
		add(JailbreakTargetModel, model)
	}
	if len(bindings) == 0 {
		return nil
	}
	return tx.Create(&bindings).Error
}

// JailbreakRule is the resolver's immutable view of one matched profile.
type JailbreakRule struct {
	Id       int
	Name     string
	Kind     string
	Prompt   string
	Headers  map[string]interface{}
	Mode     string
	Priority int
	Force    bool
}

type jailbreakSnapshot struct {
	channelRules      map[int][]JailbreakRule
	modelRules        map[string][]JailbreakRule
	channelModelRules map[int]map[string]JailbreakRule
	expiresAt         time.Time
}

const jailbreakSnapshotTTL = 30 * time.Second

var jailbreakRulesCache struct {
	mu   sync.RWMutex
	snap *jailbreakSnapshot
}

// MatchJailbreakRules returns the enabled profiles bound to the given channel
// or model, ordered by priority (highest first). A profile bound to both is
// returned once. The snapshot is cached briefly so the relay hot path never
// queries the database per request; CRUD invalidates it immediately.
func MatchJailbreakRules(channelID int, modelName string) []JailbreakRule {
	snap := loadJailbreakSnapshot()
	if snap == nil {
		return nil
	}
	matched := make(map[int]JailbreakRule)
	if channelID > 0 {
		for _, rule := range snap.channelRules[channelID] {
			matched[rule.Id] = rule
		}
	}
	if modelName != "" {
		for _, rule := range snap.modelRules[modelName] {
			matched[rule.Id] = rule
		}
		if byModel, ok := snap.channelModelRules[channelID]; ok {
			if rule, ok := byModel[modelName]; ok {
				matched[rule.Id] = rule
			}
		}
	}
	if len(matched) == 0 {
		return nil
	}
	rules := make([]JailbreakRule, 0, len(matched))
	for _, rule := range matched {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		return rules[i].Id < rules[j].Id
	})
	return rules
}

// MatchJailbreakHeaderRules returns header rules for the channel/model in
// ascending priority order, so callers that apply rules in sequence let the
// highest priority win.
func MatchJailbreakHeaderRules(channelID int, modelName string) []JailbreakRule {
	rules := MatchJailbreakRules(channelID, modelName)
	headerRules := make([]JailbreakRule, 0, len(rules))
	for _, rule := range rules {
		if rule.Kind == JailbreakKindHeader {
			headerRules = append(headerRules, rule)
		}
	}
	// MatchJailbreakRules sorts descending; reverse to ascending.
	for i, j := 0, len(headerRules)-1; i < j; i, j = i+1, j-1 {
		headerRules[i], headerRules[j] = headerRules[j], headerRules[i]
	}
	return headerRules
}

// InvalidateJailbreakRules drops the cached snapshot after a write.
func InvalidateJailbreakRules() {
	jailbreakRulesCache.mu.Lock()
	jailbreakRulesCache.snap = nil
	jailbreakRulesCache.mu.Unlock()
}

func loadJailbreakSnapshot() *jailbreakSnapshot {
	now := time.Now()
	jailbreakRulesCache.mu.RLock()
	snap := jailbreakRulesCache.snap
	jailbreakRulesCache.mu.RUnlock()
	if snap != nil && now.Before(snap.expiresAt) {
		return snap
	}

	jailbreakRulesCache.mu.Lock()
	defer jailbreakRulesCache.mu.Unlock()
	if jailbreakRulesCache.snap != nil && time.Now().Before(jailbreakRulesCache.snap.expiresAt) {
		return jailbreakRulesCache.snap
	}
	built, err := buildJailbreakSnapshot()
	if err != nil {
		common.SysError("jailbreak: load rules: " + err.Error())
		return jailbreakRulesCache.snap
	}
	jailbreakRulesCache.snap = built
	return built
}

func buildJailbreakSnapshot() (*jailbreakSnapshot, error) {
	var profiles []JailbreakProfile
	if err := dbx.DB.Where("enabled = ?", true).Find(&profiles).Error; err != nil {
		return nil, err
	}
	snap := &jailbreakSnapshot{
		channelRules:      make(map[int][]JailbreakRule),
		modelRules:        make(map[string][]JailbreakRule),
		channelModelRules: make(map[int]map[string]JailbreakRule),
		expiresAt:         time.Now().Add(jailbreakSnapshotTTL),
	}
	if len(profiles) == 0 {
		return snap, nil
	}
	byID := make(map[int]JailbreakProfile, len(profiles))
	ids := make([]int, 0, len(profiles))
	for _, profile := range profiles {
		byID[profile.Id] = profile
		ids = append(ids, profile.Id)
	}
	for _, profile := range profiles {
		if strings.TrimSpace(profile.ChannelModels) == "" {
			continue
		}
		var perChannel map[string][]string
		if err := common.Unmarshal([]byte(profile.ChannelModels), &perChannel); err != nil {
			common.SysError("jailbreak: invalid channel_models JSON for rule " + profile.Name + ": " + err.Error())
			continue
		}
		rule := JailbreakRule{
			Id:       profile.Id,
			Name:     profile.Name,
			Kind:     profile.Kind,
			Prompt:   profile.Prompt,
			Mode:     profile.Mode,
			Priority: profile.Priority,
			Force:    profile.Force,
		}
		if rule.Kind == "" {
			rule.Kind = JailbreakKindPrompt
		}
		if rule.Kind == JailbreakKindHeader {
			headers := map[string]interface{}{}
			if strings.TrimSpace(profile.Headers) != "" {
				if err := common.Unmarshal([]byte(profile.Headers), &headers); err != nil {
					common.SysError("jailbreak: invalid headers JSON for rule " + profile.Name + ": " + err.Error())
					continue
				}
			}
			rule.Headers = headers
		}
		for channelKey, names := range perChannel {
			id, err := strconv.Atoi(strings.TrimSpace(channelKey))
			if err != nil || id <= 0 {
				continue
			}
			for _, name := range names {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				if snap.channelModelRules[id] == nil {
					snap.channelModelRules[id] = make(map[string]JailbreakRule)
				}
				snap.channelModelRules[id][name] = rule
			}
		}
	}

	bindings, err := ListJailbreakBindingsByProfileIDs(ids)
	if err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		profile, ok := byID[binding.ProfileId]
		if !ok {
			continue
		}
		kind := profile.Kind
		if kind == "" {
			kind = JailbreakKindPrompt
		}
		rule := JailbreakRule{
			Id:       profile.Id,
			Name:     profile.Name,
			Kind:     kind,
			Prompt:   profile.Prompt,
			Mode:     profile.Mode,
			Priority: profile.Priority,
			Force:    profile.Force,
		}
		if kind == JailbreakKindHeader {
			headers := map[string]interface{}{}
			if strings.TrimSpace(profile.Headers) != "" {
				if err := common.Unmarshal([]byte(profile.Headers), &headers); err != nil {
					common.SysError("jailbreak: invalid headers JSON for rule " + profile.Name + ": " + err.Error())
					continue
				}
			}
			rule.Headers = headers
		}
		switch binding.TargetType {
		case JailbreakTargetChannel:
			id, err := strconv.Atoi(binding.TargetKey)
			if err != nil || id <= 0 {
				continue
			}
			snap.channelRules[id] = append(snap.channelRules[id], rule)
		case JailbreakTargetModel:
			snap.modelRules[binding.TargetKey] = append(snap.modelRules[binding.TargetKey], rule)
		}
	}
	for id := range snap.channelRules {
		sortJailbreakRules(snap.channelRules[id])
	}
	for name := range snap.modelRules {
		sortJailbreakRules(snap.modelRules[name])
	}
	return snap, nil
}

func sortJailbreakRules(rules []JailbreakRule) {
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		return rules[i].Id < rules[j].Id
	})
}
