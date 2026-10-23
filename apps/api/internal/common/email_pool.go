package common

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

// SenderPool holds every configured EmailSender and decides which one delivers
// a given message: accounts with quota left, lowest priority tier first, rotated
// inside that tier, falling through to the next tier when one is rejected.
type SenderPool struct {
	mu      sync.RWMutex
	senders []EmailSender
}

// smtpSendCursor spreads consecutive sends across the accounts of a tier.
var smtpSendCursor atomic.Uint32

type senderEntry struct {
	sender EmailSender
	status SenderStatus
}

var (
	defaultSenderPoolMu sync.Mutex
	defaultSenderPool   *SenderPool
)

// DefaultSenderPool returns the process-wide pool, building it from the
// configured SMTP accounts on first use.
func DefaultSenderPool() *SenderPool {
	defaultSenderPoolMu.Lock()
	defer defaultSenderPoolMu.Unlock()

	if defaultSenderPool == nil {
		defaultSenderPool = NewSenderPool(SMTPAccountPool)
	}
	return defaultSenderPool
}

// RebuildSMTPSenderPool replaces the process-wide pool after the
// SMTPAccountPool option changes, so an edit takes effect without a restart.
func RebuildSMTPSenderPool() {
	defaultSenderPoolMu.Lock()
	defer defaultSenderPoolMu.Unlock()

	defaultSenderPool = NewSenderPool(SMTPAccountPool)
}

func NewSenderPool(configs []SMTPAccountConfig) *SenderPool {
	senders := make([]EmailSender, 0, len(configs))
	for _, config := range configs {
		senders = append(senders, newSMTPSender(config))
	}
	return &SenderPool{senders: senders}
}

func (p *SenderPool) Send(msg OutgoingMessage) error {
	ordered := p.ordered()
	if len(ordered) == 0 {
		if len(p.senders) == 0 {
			return fmt.Errorf("SMTP 服务器未配置")
		}
		return fmt.Errorf("所有 SMTP 账号均已达到当日发送额度")
	}

	var lastErr error
	for _, entry := range ordered {
		lastErr = entry.sender.Send(msg)
		if lastErr == nil {
			if err := smtpRecordQuotaUse(entry.status.Account, entry.status.DailyLimit); err != nil {
				SysError(fmt.Sprintf("failed to record SMTP quota for %s: %v", entry.status.Account, err))
			}
			return nil
		}
		SysError(fmt.Sprintf("failed to send email through %s: %v", entryLabel(entry), lastErr))
	}
	return lastErr
}

func (p *SenderPool) Statuses() []SenderStatus {
	p.mu.RLock()
	senders := make([]EmailSender, len(p.senders))
	copy(senders, p.senders)
	p.mu.RUnlock()

	statuses := make([]SenderStatus, 0, len(senders))
	for _, sender := range senders {
		statuses = append(statuses, sender.Status())
	}
	return statuses
}

// ordered returns the senders eligible for one delivery: quota not yet reached,
// lowest priority tier first, rotated inside that tier. Statuses are read once
// here and reused for selection, labelling and quota accounting, because each
// read may hit the database.
func (p *SenderPool) ordered() []senderEntry {
	p.mu.RLock()
	senders := make([]EmailSender, len(p.senders))
	copy(senders, p.senders)
	p.mu.RUnlock()

	entries := make([]senderEntry, 0, len(senders))
	for _, sender := range senders {
		status := sender.Status()
		if status.DailyLimit <= 0 || status.UsedToday < status.DailyLimit {
			entries = append(entries, senderEntry{sender: sender, status: status})
		}
	}
	if len(entries) == 0 {
		return nil
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].status.Priority < entries[j].status.Priority
	})

	topPriority := entries[0].status.Priority
	tierEnd := 1
	for tierEnd < len(entries) && entries[tierEnd].status.Priority == topPriority {
		tierEnd++
	}

	tier := entries[:tierEnd]
	if len(tier) > 1 {
		// 用自增前的旧值取模：cursor 为 0 的首次发送必须落在第一个账号上。
		offset := int(smtpSendCursor.Add(1)-1) % len(tier)
		rotated := make([]senderEntry, 0, len(tier))
		rotated = append(rotated, tier[offset:]...)
		rotated = append(rotated, tier[:offset]...)
		tier = rotated
	}

	ordered := make([]senderEntry, 0, len(entries))
	ordered = append(ordered, tier...)
	return append(ordered, entries[tierEnd:]...)
}

func entryLabel(entry senderEntry) string {
	if entry.status.Server != "" {
		return fmt.Sprintf("%s (%s:%d)", entry.status.Account, entry.status.Server, entry.status.Port)
	}
	return entry.status.Account
}
