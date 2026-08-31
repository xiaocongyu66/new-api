package channel

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/logger"
)

// CacheGetRandomSatisfiedChannel is the production implementation behind
// gateway/SelectChannel: pick a channel that satisfies group, model,
// and retry constraints from the in-memory channel cache.
// 尝试获取一个满足要求的随机渠道。
//
// For "auto" tokenGroup with cross-group Retry enabled:
// 对于启用了跨分组重试的 "auto" tokenGroup：
//
//   - Each group will exhaust all its priorities before moving to the next group.
//     每个分组会用完所有优先级后才会切换到下一个分组。
//
//   - Uses ContextKeyAutoGroupIndex to track current group index.
//     使用 ContextKeyAutoGroupIndex 跟踪当前分组索引。
//
//   - Uses ContextKeyAutoGroupRetryIndex to track the global Retry count when current group started.
//     使用 ContextKeyAutoGroupRetryIndex 跟踪当前分组开始时的全局重试次数。
//
//   - priorityRetry = Retry - startRetryIndex, represents the priority level within current group.
//     priorityRetry = Retry - startRetryIndex，表示当前分组内的优先级级别。
//
//   - When GetRandomSatisfiedChannel returns nil (priorities exhausted), moves to next group.
//     当 GetRandomSatisfiedChannel 返回 nil（优先级用完）时，切换到下一个分组。
//
// Example flow (2 groups, each with 2 priorities, RetryTimes=3):
// 示例流程（2个分组，每个有2个优先级，RetryTimes=3）：
//
//	Retry=0: GroupA, priority0 (startRetryIndex=0, priorityRetry=0)
//	         分组A, 优先级0
//
//	Retry=1: GroupA, priority1 (startRetryIndex=0, priorityRetry=1)
//	         分组A, 优先级1
//
//	Retry=2: GroupA exhausted → GroupB, priority0 (startRetryIndex=2, priorityRetry=0)
//	         分组A用完 → 分组B, 优先级0
//
//	Retry=3: GroupB, priority1 (startRetryIndex=2, priorityRetry=1)
//	         分组B, 优先级1
func CacheGetRandomSatisfiedChannel(param *SelectParams) (*SelectedRoute, string, error) {
	var route *SelectedRoute
	var err error
	selectGroup := param.TokenGroup
	userGroup := common.GetCtxKeyString(param.Ctx, constant.ContextKeyUserGroup)
	crossGroupRetry := common.GetCtxKeyBool(param.Ctx, constant.ContextKeyTokenCrossGroupRetry)

	if param.TokenGroup == "auto" {
		autoGroups := GetRequestAutoGroups(param.Ctx, userGroup)
		if len(autoGroups) == 0 {
			return nil, selectGroup, errors.New("auto groups is not enabled")
		}
		startGroupIndex := common.GetCtxKeyInt(param.Ctx, constant.ContextKeyAutoGroupIndex)
		for i := startGroupIndex; i < len(autoGroups); i++ {
			autoGroup := autoGroups[i]
			priorityRetry := param.GetRetry()
			if i > startGroupIndex {
				priorityRetry = 0
			}
			logger.LogDebug(param.Ctx.Context(), "Auto selecting group: %s, priorityRetry: %d", autoGroup, priorityRetry)
			route, err = GetRandomSatisfiedChannel(autoGroup, param.ModelName, priorityRetry, param.RequestPath, param.ExcludeRoutes)
			if err != nil {
				return nil, autoGroup, err
			}
			if route == nil {
				common.SetCtxKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				common.SetCtxKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
				param.SetRetry(0)
				continue
			}
			common.SetCtxKey(param.Ctx, constant.ContextKeyAutoGroup, autoGroup)
			selectGroup = autoGroup
			if crossGroupRetry && priorityRetry >= common.RetryTimes {
				common.SetCtxKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				param.SetRetry(0)
				param.ResetRetryNextTry()
			} else {
				common.SetCtxKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i)
			}
			break
		}
	} else {
		route, err = GetRandomSatisfiedChannel(param.TokenGroup, param.ModelName, param.GetRetry(), param.RequestPath, param.ExcludeRoutes)
		if err != nil {
			return nil, param.TokenGroup, err
		}
	}
	return route, selectGroup, nil
}
