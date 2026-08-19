package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	identitymodel "github.com/QuantumNous/new-api/internal/identity/model"
)

const authArtifactCleanupInterval = time.Hour

// StartAuthArtifactCleanup removes expired dashboard Sessions and old
// one-time authentication flows. Only the master instance performs cleanup.
func StartAuthArtifactCleanup() {
	if !common.IsMasterNode {
		return
	}
	go func() {
		cleanupAuthArtifacts()
		ticker := time.NewTicker(authArtifactCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			cleanupAuthArtifacts()
		}
	}()
}

func cleanupAuthArtifacts() {
	now := time.Now()
	count, err := identitymodel.CountUserSessionsCreatedSince(0, now.Add(-time.Hour).Unix())
	if err != nil {
		common.SysError("failed to count hourly user session issuance: " + err.Error())
	} else if count > int64(common.UserSessionHourlyAlertThreshold) {
		common.SysError(fmt.Sprintf(
			"hourly user session issuance exceeded alert threshold: count=%d threshold=%d window_seconds=%d",
			count,
			common.UserSessionHourlyAlertThreshold,
			int64(time.Hour/time.Second),
		))
	}
	if err := identitymodel.DeleteExpiredUserSessions(now.Unix()); err != nil {
		common.SysError("failed to delete expired user sessions: " + err.Error())
	}
	if err := identitymodel.DeleteOldRevokedUserSessions(now.Unix()); err != nil {
		common.SysError("failed to delete old revoked user sessions: " + err.Error())
	}
	if err := identitymodel.DeleteExpiredAuthFlows(now); err != nil {
		common.SysError("failed to delete expired authentication flows: " + err.Error())
	}
}
