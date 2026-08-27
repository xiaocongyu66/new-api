package task

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/billing/settlecore"
	taskdto "github.com/QuantumNous/new-api/internal/dto"
	"github.com/QuantumNous/new-api/internal/gateway/port"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test adaptors implementing port.TaskProviderExec
// ---------------------------------------------------------------------------

type taskPollingFetchProvider struct {
	mu           sync.Mutex
	taskIDs      []string
	fetched      chan string
	blockTaskID  string
	blockStarted chan struct{}
	releaseBlock chan struct{}
	blockOnce    sync.Once
}

func (p *taskPollingFetchProvider) Init(baseURL, apiKey string) {}

func (p *taskPollingFetchProvider) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	// Extract task_id from body
	taskID, _ := body["task_id"].(string)
	if taskID == "" {
		// For bulk Suno, body has "ids"
		ids, _ := body["ids"].([]string)
		if len(ids) > 0 {
			taskID = ids[0]
		}
	}

	if taskID != "" {
		p.mu.Lock()
		p.taskIDs = append(p.taskIDs, taskID)
		p.mu.Unlock()

		if p.fetched != nil {
			select {
			case p.fetched <- taskID:
			default:
			}
		}
	}

	if p.blockTaskID != "" && taskID == p.blockTaskID {
		p.blockOnce.Do(func() {
			close(p.blockStarted)
		})
		<-p.releaseBlock
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(bytes.NewReader([]byte(`{
			"data": {
				"task_id": "` + taskID + `",
				"status": "IN_PROGRESS",
				"progress": "50%"
			},
			"code": 0,
			"success": true
		}`))),
		Header: make(http.Header),
	}, nil
}

func (p *taskPollingFetchProvider) ParseTaskResult(body []byte) (*port.TaskResult, error) {
	return &port.TaskResult{
		TaskID:           "",
		Status:           string(model.TaskStatusInProgress),
		Progress:         "50%",
		CompletionTokens: 0,
		TotalTokens:      0,
	}, nil
}

func (p *taskPollingFetchProvider) AdjustBillingOnComplete(task *model.Task, taskResult *port.TaskResult) int {
	return 0
}

func (p *taskPollingFetchProvider) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	return nil, nil
}

func (p *taskPollingFetchProvider) fetchCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.taskIDs)
}

func (p *taskPollingFetchProvider) fetchedTaskIDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]string, len(p.taskIDs))
	copy(ids, p.taskIDs)
	return ids
}

type sunoFailurePollingProvider struct {
	failReason string
}

func (p *sunoFailurePollingProvider) Init(baseURL, apiKey string) {}

func (p *sunoFailurePollingProvider) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	ids, _ := body["ids"].([]string)
	items := make([]taskdto.SunoDataResponse, 0, len(ids))
	for _, taskID := range ids {
		items = append(items, taskdto.SunoDataResponse{
			TaskID:     taskID,
			Status:     string(model.TaskStatusFailure),
			FailReason: p.failReason,
			FinishTime: time.Now().Unix(),
		})
	}
	responseBody, err := common.Marshal(taskdto.TaskResponse[[]taskdto.SunoDataResponse]{
		Code: taskdto.TaskSuccessCode,
		Data: items,
	})
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
		Header:     make(http.Header),
	}, nil
}

func (p *sunoFailurePollingProvider) ParseTaskResult(body []byte) (*port.TaskResult, error) {
	// Not used for Suno bulk path
	return nil, nil
}

func (p *sunoFailurePollingProvider) AdjustBillingOnComplete(task *model.Task, taskResult *port.TaskResult) int {
	return 0
}

func (p *sunoFailurePollingProvider) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Seed helpers (copied from service/task_billing_test.go)
// ---------------------------------------------------------------------------

func truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM tasks")
		model.DB.Exec("DELETE FROM users")
		model.DB.Exec("DELETE FROM tokens")
		model.DB.Exec("DELETE FROM logs")
		model.DB.Exec("DELETE FROM channels")
		model.DB.Exec("DELETE FROM midjourneys")
		model.DB.Exec("DELETE FROM top_ups")
		model.DB.Exec("DELETE FROM user_subscriptions")
		model.DB.Exec("DELETE FROM system_task_locks")
		model.DB.Exec("DELETE FROM system_tasks")
	})
}

func seedUser(t *testing.T, id int, quota int) {
	t.Helper()
	user := &model.User{Id: id, Username: "test_user", Quota: quota, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
}

func seedToken(t *testing.T, id int, userId int, key string, remainQuota int) {
	t.Helper()
	token := &model.Token{
		Id:          id,
		UserId:      userId,
		Key:         key,
		Name:        "test_token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: remainQuota,
		UsedQuota:   0,
	}
	require.NoError(t, model.DB.Create(token).Error)
}
func seedChannel(t *testing.T, id int, disableSleep bool) {
	t.Helper()
	ch := &model.Channel{
		Id:     id,
		Type:   constant.ChannelTypeKling,
		Name:   "polling_channel",
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
	}
	if disableSleep {
		ch.SetOtherSettings(dto.ChannelOtherSettings{DisableTaskPollingSleep: true})
	}
	require.NoError(t, model.DB.Create(ch).Error)
}

func makeTask(userId, channelId, quota, tokenId int, billingSource string, subscriptionId int) *model.Task {
	return &model.Task{
		TaskID:    "task_" + time.Now().Format("150405.000"),
		UserId:    userId,
		ChannelId: channelId,
		Quota:     quota,
		Status:    model.TaskStatus(model.TaskStatusInProgress),
		Group:     "default",
		Data:      json.RawMessage(`{}`),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: "test-model",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource:  billingSource,
			SubscriptionId: subscriptionId,
			TokenId:        tokenId,
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.02,
				GroupRatio:      1.0,
				OriginModelName: "test-model",
			},
		},
	}
}

func seedPollingTask(t *testing.T, channelID int, publicID string, upstreamID string) *model.Task {
	t.Helper()
	task := makeTask(1, channelID, 100, 0, settlecore.BillingSourceWallet, 0)
	task.TaskID = publicID
	task.Platform = constant.TaskPlatform("kling")
	task.Status = model.TaskStatusInProgress
	task.Progress = "50%"
	task.SubmitTime = time.Now().Unix()
	task.PrivateData.UpstreamTaskID = upstreamID
	require.NoError(t, model.DB.Create(task).Error)
	return task
}

func getUserQuota(t *testing.T, id int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&user).Error)
	return user.Quota
}

func getTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota").Where("id = ?", id).First(&token).Error)
	return token.RemainQuota
}

func countLogs(t *testing.T) int64 {
	t.Helper()
	var count int64
	model.LOG_DB.Model(&model.Log{}).Count(&count)
	return count
}

func stringPtr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestUpdateVideoTasksDefaultSleepWaitsBetweenTasks(t *testing.T) {
	truncate(t)

	const channelID = 101
	seedChannel(t, channelID, false)
	first := seedPollingTask(t, channelID, "task_public_1", "upstream_1")
	second := seedPollingTask(t, channelID, "task_public_2", "upstream_2")

	provider := &taskPollingFetchProvider{
		fetched: make(chan string, 2),
	}
	previousFactory := port.GetTaskProviderFunc
	port.GetTaskProviderFunc = func(platform string) port.TaskProviderExec {
		return provider
	}
	t.Cleanup(func() { port.GetTaskProviderFunc = previousFactory })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := updateVideoTasks(ctx, constant.TaskPlatform("kling"), map[int][]string{
		channelID: {
			first.GetUpstreamTaskID(),
			second.GetUpstreamTaskID(),
		},
	}, map[string]*model.Task{
		first.GetUpstreamTaskID():  first,
		second.GetUpstreamTaskID(): second,
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, 1, provider.fetchCount())
}

func TestUpdateVideoTasksCanSkipPollingSleepPerChannel(t *testing.T) {
	truncate(t)

	const channelID = 102
	seedChannel(t, channelID, true)
	first := seedPollingTask(t, channelID, "task_public_3", "upstream_3")
	second := seedPollingTask(t, channelID, "task_public_4", "upstream_4")

	provider := &taskPollingFetchProvider{
		fetched: make(chan string, 2),
	}
	previousFactory := port.GetTaskProviderFunc
	port.GetTaskProviderFunc = func(platform string) port.TaskProviderExec {
		return provider
	}
	t.Cleanup(func() { port.GetTaskProviderFunc = previousFactory })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := updateVideoTasks(ctx, constant.TaskPlatform("kling"), map[int][]string{
		channelID: {
			first.GetUpstreamTaskID(),
			second.GetUpstreamTaskID(),
		},
	}, map[string]*model.Task{
		first.GetUpstreamTaskID():  first,
		second.GetUpstreamTaskID(): second,
	})

	require.NoError(t, err)
	assert.Equal(t, 2, provider.fetchCount())
}

func TestUpdateVideoTasksDefaultSleepDoesNotBlockOtherChannels(t *testing.T) {
	truncate(t)

	const firstChannelID = 201
	const secondChannelID = 202
	seedChannel(t, firstChannelID, false)
	seedChannel(t, secondChannelID, false)

	firstChannelFirst := seedPollingTask(t, firstChannelID, "task_public_5", "upstream_a_1")
	firstChannelSecond := seedPollingTask(t, firstChannelID, "task_public_6", "upstream_a_2")
	secondChannelFirst := seedPollingTask(t, secondChannelID, "task_public_7", "upstream_b_1")
	secondChannelSecond := seedPollingTask(t, secondChannelID, "task_public_8", "upstream_b_2")

	provider := &taskPollingFetchProvider{
		fetched: make(chan string, 4),
	}
	previousFactory := port.GetTaskProviderFunc
	port.GetTaskProviderFunc = func(platform string) port.TaskProviderExec {
		return provider
	}
	t.Cleanup(func() { port.GetTaskProviderFunc = previousFactory })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := updateVideoTasks(ctx, constant.TaskPlatform("kling"), map[int][]string{
		firstChannelID: {
			firstChannelFirst.GetUpstreamTaskID(),
			firstChannelSecond.GetUpstreamTaskID(),
		},
		secondChannelID: {
			secondChannelFirst.GetUpstreamTaskID(),
			secondChannelSecond.GetUpstreamTaskID(),
		},
	}, map[string]*model.Task{
		firstChannelFirst.GetUpstreamTaskID():   firstChannelFirst,
		firstChannelSecond.GetUpstreamTaskID():  firstChannelSecond,
		secondChannelFirst.GetUpstreamTaskID():  secondChannelFirst,
		secondChannelSecond.GetUpstreamTaskID(): secondChannelSecond,
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ElementsMatch(t, []string{"upstream_a_1", "upstream_b_1"}, provider.fetchedTaskIDs())
}

func TestUpdateVideoTasksSlowChannelDoesNotBlockOtherChannels(t *testing.T) {
	truncate(t)

	const slowChannelID = 251
	const fastChannelID = 252
	seedChannel(t, slowChannelID, false)
	seedChannel(t, fastChannelID, true)
	slowTask := seedPollingTask(t, slowChannelID, "task_public_slow", "upstream_slow_1")
	fastFirst := seedPollingTask(t, fastChannelID, "task_public_fast_1", "upstream_fast_parallel_1")
	fastSecond := seedPollingTask(t, fastChannelID, "task_public_fast_2", "upstream_fast_parallel_2")

	provider := &taskPollingFetchProvider{
		fetched:      make(chan string, 4),
		blockTaskID:  slowTask.GetUpstreamTaskID(),
		blockStarted: make(chan struct{}),
		releaseBlock: make(chan struct{}),
	}
	var releaseOnce sync.Once
	releaseBlockedTask := func() {
		releaseOnce.Do(func() {
			close(provider.releaseBlock)
		})
	}
	t.Cleanup(releaseBlockedTask)
	previousFactory := port.GetTaskProviderFunc
	port.GetTaskProviderFunc = func(platform string) port.TaskProviderExec {
		return provider
	}
	t.Cleanup(func() { port.GetTaskProviderFunc = previousFactory })

	errCh := make(chan error, 1)
	gopool.Go(func() {
		errCh <- updateVideoTasks(context.Background(), constant.TaskPlatform("kling"), map[int][]string{
			slowChannelID: {
				slowTask.GetUpstreamTaskID(),
			},
			fastChannelID: {
				fastFirst.GetUpstreamTaskID(),
				fastSecond.GetUpstreamTaskID(),
			},
		}, map[string]*model.Task{
			slowTask.GetUpstreamTaskID():   slowTask,
			fastFirst.GetUpstreamTaskID():  fastFirst,
			fastSecond.GetUpstreamTaskID(): fastSecond,
		})
	})

	<-provider.blockStarted

	require.Eventually(t, func() bool {
		fetchedTaskIDs := provider.fetchedTaskIDs()
		found1 := false
		found2 := false
		for _, id := range fetchedTaskIDs {
			if id == fastFirst.GetUpstreamTaskID() {
				found1 = true
			}
			if id == fastSecond.GetUpstreamTaskID() {
				found2 = true
			}
		}
		return found1 && found2
	}, 500*time.Millisecond, 100*time.Millisecond)

	releaseBlockedTask()

	err := <-errCh
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"upstream_slow_1", "upstream_fast_parallel_1", "upstream_fast_parallel_2"}, provider.fetchedTaskIDs())
}

func TestUpdateVideoTasksMixedChannelSleepSettings(t *testing.T) {
	truncate(t)

	const sleepDisabledChannel = 301
	const sleepEnabledChannel = 302
	seedChannel(t, sleepDisabledChannel, true)
	seedChannel(t, sleepEnabledChannel, false)

	sleepDisabledTask := seedPollingTask(t, sleepDisabledChannel, "task_sd_1", "upstream_sd_1")
	sleepEnabledTask1 := seedPollingTask(t, sleepEnabledChannel, "task_se_1", "upstream_se_1")
	sleepEnabledTask2 := seedPollingTask(t, sleepEnabledChannel, "task_se_2", "upstream_se_2")

	provider := &taskPollingFetchProvider{
		fetched: make(chan string, 3),
	}
	previousFactory := port.GetTaskProviderFunc
	port.GetTaskProviderFunc = func(platform string) port.TaskProviderExec {
		return provider
	}
	t.Cleanup(func() { port.GetTaskProviderFunc = previousFactory })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := updateVideoTasks(ctx, constant.TaskPlatform("kling"), map[int][]string{
		sleepDisabledChannel: {
			sleepDisabledTask.GetUpstreamTaskID(),
		},
		sleepEnabledChannel: {
			sleepEnabledTask1.GetUpstreamTaskID(),
			sleepEnabledTask2.GetUpstreamTaskID(),
		},
	}, map[string]*model.Task{
		sleepDisabledTask.GetUpstreamTaskID(): sleepDisabledTask,
		sleepEnabledTask1.GetUpstreamTaskID(): sleepEnabledTask1,
		sleepEnabledTask2.GetUpstreamTaskID(): sleepEnabledTask2,
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ElementsMatch(t, []string{"upstream_sd_1", "upstream_se_1"}, provider.fetchedTaskIDs())
}

func TestUpdateSunoTasksStalePollsRefundExactlyOnce(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 401, 401, 401
	const initialUserQuota, initialTokenQuota, taskQuota = 10_000, 6_000, 2_500
	const publicTaskID, upstreamTaskID = "suno_public_refund_once", "suno_upstream_refund_once"

	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, "sk-suno-refund-once", initialTokenQuota)
	baseURL := "https://suno.invalid"
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeSunoAPI,
		Name:    "suno_refund_once",
		Key:     "sk-suno-channel",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}).Error)

	task := makeTask(userID, channelID, taskQuota, tokenID, settlecore.BillingSourceWallet, 0)
	task.TaskID = publicTaskID
	task.Platform = constant.TaskPlatformSuno
	task.Status = model.TaskStatusInProgress
	task.Progress = "50%"
	task.SubmitTime = time.Now().Unix()
	task.PrivateData.UpstreamTaskID = upstreamTaskID
	require.NoError(t, model.DB.Create(task).Error)

	var firstPollTask model.Task
	var staleSecondPollTask model.Task
	require.NoError(t, model.DB.First(&firstPollTask, task.ID).Error)
	require.NoError(t, model.DB.First(&staleSecondPollTask, task.ID).Error)

	provider := &sunoFailurePollingProvider{failReason: "upstream failed"}
	previousFactory := port.GetTaskProviderFunc
	port.GetTaskProviderFunc = func(platform string) port.TaskProviderExec {
		return provider
	}
	t.Cleanup(func() { port.GetTaskProviderFunc = previousFactory })

	require.NoError(t, updateSunoTasksChannel(context.Background(), channelID, []string{upstreamTaskID}, map[string]*model.Task{
		upstreamTaskID: &firstPollTask,
	}))
	require.NoError(t, updateSunoTasksChannel(context.Background(), channelID, []string{upstreamTaskID}, map[string]*model.Task{
		upstreamTaskID: &staleSecondPollTask,
	}))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	assert.Zero(t, reloaded.Quota)
	assert.Equal(t, initialUserQuota+taskQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota+taskQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, int64(1), countLogs(t))
}

func TestRunTaskPollingOnceDoesNotRefundHistoricalFailedTask(t *testing.T) {
	truncate(t)

	const userID, initialQuota, taskQuota = 402, 10_000, 1_200
	seedUser(t, userID, initialQuota)

	legacyTask := makeTask(userID, 0, taskQuota, 0, settlecore.BillingSourceWallet, 0)
	legacyTask.TaskID = "historical_failed_task"
	legacyTask.Status = model.TaskStatusFailure
	legacyTask.Progress = "100%"
	legacyTask.FinishTime = 1771718399
	legacyTask.FailReason = "old failure"
	require.NoError(t, model.DB.Create(legacyTask).Error)

	previousTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = previousTimeout })

	summary := RunTaskPollingOnce(context.Background(), nil)
	require.Equal(t, 0, summary.NullTasksFailed)

	var reloadedLegacy model.Task
	require.NoError(t, model.DB.First(&reloadedLegacy, legacyTask.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloadedLegacy.Status)
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
}

func TestSweepTimedOutTasksHonorsRefundRolloutBoundary(t *testing.T) {
	truncate(t)

	const (
		userID          = 403
		initialQuota    = 10_000
		legacyTaskQuota = 1_800
		modernTaskQuota = 1_200
	)
	seedUser(t, userID, initialQuota)

	legacyTask := makeTask(userID, 0, legacyTaskQuota, 0, settlecore.BillingSourceWallet, 0)
	legacyTask.TaskID = "legacy_timeout_without_refund"
	legacyTask.Progress = "50%"
	legacyTask.SubmitTime = 1771718399 // 2026-02-21 23:59:59 UTC
	require.NoError(t, model.DB.Create(legacyTask).Error)

	modernTask := makeTask(userID, 0, modernTaskQuota, 0, settlecore.BillingSourceWallet, 0)
	modernTask.TaskID = "modern_timeout_with_refund"
	modernTask.Progress = "50%"
	modernTask.SubmitTime = 1771718400 // 2026-02-22 00:00:00 UTC
	require.NoError(t, model.DB.Create(modernTask).Error)

	previousTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = previousTimeout })

	sweepTimedOutTasks(context.Background())

	var reloadedLegacy model.Task
	var reloadedModern model.Task
	require.NoError(t, model.DB.First(&reloadedLegacy, legacyTask.ID).Error)
	require.NoError(t, model.DB.First(&reloadedModern, modernTask.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloadedLegacy.Status)
	assert.EqualValues(t, model.TaskStatusFailure, reloadedModern.Status)
	assert.Zero(t, reloadedLegacy.Quota)
	assert.Zero(t, reloadedModern.Quota)
	assert.Contains(t, reloadedLegacy.FailReason, "旧系统遗留任务")
	assert.Contains(t, reloadedModern.FailReason, "任务超时")
	assert.Equal(t, initialQuota+modernTaskQuota, getUserQuota(t, userID))
	assert.Equal(t, int64(1), countLogs(t))
}
