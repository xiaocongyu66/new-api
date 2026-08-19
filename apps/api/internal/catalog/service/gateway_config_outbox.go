package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	catalogmodel "github.com/QuantumNous/new-api/internal/catalog/model"
)

const GatewayRoutingRevisionChannel = "new-api:gateway:routing-revision:v1"

var (
	ErrGatewayOutboxRedisDisabled    = errors.New("gateway outbox redis disabled")
	ErrGatewayOutboxRedisUnavailable = errors.New("gateway outbox redis unavailable")
)

type RoutingRevisionNotice struct {
	RoutingRevision int64 `json:"routing_revision"`
}

func PublishGatewayRoutingRevision(ctx context.Context, revision int64) error {
	if revision <= 0 {
		return fmt.Errorf("invalid routing revision %d", revision)
	}
	if !common.RedisEnabled || common.RDB == nil {
		return ErrGatewayOutboxRedisDisabled
	}
	payload, err := common.Marshal(RoutingRevisionNotice{RoutingRevision: revision})
	if err != nil {
		return err
	}
	if err := common.RDB.Publish(ctx, GatewayRoutingRevisionChannel, payload).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrGatewayOutboxRedisUnavailable, err)
	}
	return nil
}

func PublishPendingGatewayRevisions(ctx context.Context, limit int) error {
	rows, err := catalogmodel.ListPendingGatewayConfigOutbox(limit)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := PublishGatewayRoutingRevision(ctx, row.RoutingRevision); err != nil {
			if recordErr := catalogmodel.RecordGatewayConfigOutboxAttempt(row.ID, classifyGatewayOutboxError(err)); recordErr != nil {
				return recordErr
			}
			continue
		}
		if err := catalogmodel.RecordGatewayConfigOutboxAttempt(row.ID, "published"); err != nil {
			return err
		}
		if err := catalogmodel.MarkGatewayConfigOutboxPublished(row.ID, time.Now()); err != nil {
			return err
		}
	}
	return nil
}

func RunGatewayConfigOutboxPublisher(ctx context.Context) {
	interval := time.Duration(common.GetEnvOrDefault("GATEWAY_OUTBOX_PUBLISH_INTERVAL_SECONDS", 1)) * time.Second
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_ = PublishPendingGatewayRevisions(ctx, 100)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func classifyGatewayOutboxError(err error) string {
	if errors.Is(err, ErrGatewayOutboxRedisDisabled) {
		return "redis_disabled"
	}
	if errors.Is(err, ErrGatewayOutboxRedisUnavailable) {
		return "redis_unavailable"
	}
	return "publish_error"
}
