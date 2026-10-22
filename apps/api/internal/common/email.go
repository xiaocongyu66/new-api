package common

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// SMTPAccountConfig is one upstream SMTP identity new-api can send through.
// Several can be configured at once so that a provider-side rate limit or a
// single rejected account never stops outbound mail.
type SMTPAccountConfig struct {
	Server     string `json:"server"`
	Port       int    `json:"port"`
	Account    string `json:"account"`
	Token      string `json:"token"`
	From       string `json:"from"`
	SSLEnabled bool   `json:"ssl"`
	StartTLS   bool   `json:"starttls"`
}

var (
	// SMTPAccountPool holds every configured upstream SMTP identity. An empty
	// pool means the deployment only set the legacy single-account options.
	SMTPAccountPool []SMTPAccountConfig
	// smtpSendCursor spreads consecutive sends across the pool.
	smtpSendCursor atomic.Uint32
)

// ParseSMTPAccountPoolJSON decodes the SMTPAccountPool option value. An empty
// value means "no pool configured" and is not an error.
func ParseSMTPAccountPoolJSON(value string) ([]SMTPAccountConfig, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var pool []SMTPAccountConfig
	if err := UnmarshalJsonStr(value, &pool); err != nil {
		return nil, fmt.Errorf("SMTPAccountPool must be a JSON array of accounts")
	}
	for i, account := range pool {
		if account.Server == "" || account.Account == "" || account.Port <= 0 {
			return nil, fmt.Errorf("SMTPAccountPool entry %d requires server, account and a positive port", i)
		}
	}
	return pool, nil
}

func generateMessageID(from string) (string, error) {
	parts := strings.Split(from, "@")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid SMTP account")
	}
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), GetRandomString(12), parts[1]), nil
}

func shouldUseSMTPLoginAuth(account SMTPAccountConfig) bool {
	if SMTPForceAuthLogin {
		return true
	}
	return isOutlookServer(account.Account) || slices.Contains(EmailLoginAuthServerList, account.Server)
}

func getSMTPAuth(account SMTPAccountConfig) smtp.Auth {
	return AutoSMTPAuth(account.Account, account.Token, account.Server)
}

func smtpTLSConfig(server string) *tls.Config {
	return &tls.Config{
		ServerName:         server,
		InsecureSkipVerify: SMTPInsecureSkipVerify, // #nosec G402 -- admin-controlled SMTP compatibility option.
	}
}

func newSMTPClient(addr string, account SMTPAccountConfig) (*smtp.Client, error) {
	if account.SSLEnabled || (account.Port == 465 && !account.StartTLS) {
		conn, err := tls.Dial("tcp", addr, smtpTLSConfig(account.Server))
		if err != nil {
			return nil, err
		}
		client, err := smtp.NewClient(conn, account.Server)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		return client, nil
	}

	client, err := smtp.Dial(addr)
	if err != nil {
		return nil, err
	}

	if account.StartTLS {
		startTLSSupported, _ := client.Extension("STARTTLS")
		if !startTLSSupported {
			_ = client.Close()
			return nil, fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(smtpTLSConfig(account.Server)); err != nil {
			return nil, err
		}
	}

	return client, nil
}

func SendEmail(subject string, receiver string, content string) error {
	accounts := SMTPAccountPool
	if len(accounts) == 0 {
		if SMTPServer == "" && SMTPAccount == "" {
			return fmt.Errorf("SMTP 服务器未配置")
		}
		accounts = []SMTPAccountConfig{{
			Server:     SMTPServer,
			Port:       SMTPPort,
			Account:    SMTPAccount,
			Token:      SMTPToken,
			From:       SMTPFrom,
			SSLEnabled: SMTPSSLEnabled,
			StartTLS:   SMTPStartTLSEnabled,
		}}
	}

	// Every send starts at a different account so the pool spreads load, and
	// falls through to the remaining accounts when one of them is rejected.
	start := smtpSendCursor.Add(1) - 1
	var lastErr error
	for offset := uint32(0); offset < uint32(len(accounts)); offset++ {
		account := accounts[(start+offset)%uint32(len(accounts))]
		lastErr = sendEmailThrough(account, subject, receiver, content)
		if lastErr == nil {
			return nil
		}
		SysError(fmt.Sprintf("failed to send email through %s: %v", account.Account, lastErr))
	}
	return lastErr
}

func sendEmailThrough(account SMTPAccountConfig, subject string, receiver string, content string) error {
	// From 缺省时回落到登录账号，与旧的单账号配置行为一致。
	from := account.From
	if from == "" {
		from = account.Account
	}
	id, err := generateMessageID(from)
	if err != nil {
		return err
	}
	encodedSubject := fmt.Sprintf("=?UTF-8?B?%s?=", base64.StdEncoding.EncodeToString([]byte(subject)))
	mail := []byte(fmt.Sprintf("To: %s\r\n"+
		"From: %s <%s>\r\n"+
		"Subject: %s\r\n"+
		"Date: %s\r\n"+
		"Message-ID: %s\r\n"+
		"Content-Type: text/html; charset=UTF-8\r\n\r\n%s\r\n",
		receiver, SystemName, from, encodedSubject, time.Now().Format(time.RFC1123Z), id, content))
	auth := getSMTPAuth(account)
	addr := fmt.Sprintf("%s:%d", account.Server, account.Port)
	to := strings.Split(receiver, ";")
	client, err := newSMTPClient(addr, account)
	if err != nil {
		return err
	}
	defer client.Close()
	if account.Account != "" && account.Token != "" {
		if err = client.Auth(auth); err != nil {
			return err
		}
	}
	if err = client.Mail(from); err != nil {
		return err
	}
	for _, receiver := range to {
		if err = client.Rcpt(receiver); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = w.Write(mail)
	if err != nil {
		return err
	}
	err = w.Close()
	if err != nil {
		return err
	}
	err = client.Quit()
	if err != nil {
		SysError(fmt.Sprintf("failed to send email to %s: %v", receiver, err))
	}
	return err
}
