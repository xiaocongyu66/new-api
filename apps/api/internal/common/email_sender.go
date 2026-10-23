package common

import "time"

// OutgoingMessage is one message a caller wants delivered. Callers build it
// from their own domain data and never see which upstream identity sends it.
type OutgoingMessage struct {
	Subject  string
	Receiver string // semicolon-separated recipients
	Content  string // HTML body
}

// SenderStatus is the observable state of one sender, which the admin UI reads
// to show quota usage and recent failures.
type SenderStatus struct {
	Account        string    `json:"account"`
	Server         string    `json:"server"`
	Port           int       `json:"port"`
	From           string    `json:"from"`
	Priority       int       `json:"priority"`
	DailyLimit     int       `json:"dailyLimit"`
	UsedToday      int       `json:"usedToday"`
	RemainingToday int       `json:"remainingToday"`
	LastSentAt     time.Time `json:"lastSentAt"`
	LastError      string    `json:"lastError"`
	LastErrorAt    time.Time `json:"lastErrorAt"`
}

// EmailSender is one upstream identity new-api can deliver mail through. SMTP
// is the first implementation; an HTTP-API sender can be added later without
// touching SendEmail callers.
type EmailSender interface {
	// Send delivers the message and returns nil on success.
	Send(msg OutgoingMessage) error
	// Status reports the sender's configuration and today's quota usage.
	Status() SenderStatus
}
