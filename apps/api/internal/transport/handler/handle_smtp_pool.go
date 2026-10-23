package handler

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// SMTPPoolStatusResponse is what the admin UI reads to show per-account quota
// usage and recent failures.
type SMTPPoolStatusResponse struct {
	Senders        []common.SenderStatus `json:"senders"`
	SentToday      int                   `json:"sentToday"`
	RemainingToday int                   `json:"remainingToday"`
}

func GetSMTPPoolStatus(c contract.Context) {
	statuses := common.DefaultSenderPool().Statuses()

	sentToday := 0
	remainingToday := 0
	for _, status := range statuses {
		if status.DailyLimit <= 0 {
			continue
		}
		sentToday += status.UsedToday
		if status.RemainingToday > 0 {
			remainingToday += status.RemainingToday
		}
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data": SMTPPoolStatusResponse{
			Senders:        statuses,
			SentToday:      sentToday,
			RemainingToday: remainingToday,
		},
	})
}
