package common

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// LogOutput is the writer for standard log output.
// Protected by LogWriterMu.
var LogOutput io.Writer = os.Stdout

// LogErrOutput is the writer for error log output.
// Protected by LogWriterMu.
var LogErrOutput io.Writer = os.Stderr

// LogWriterMu protects concurrent access to LogOutput/LogErrOutput
// during log file rotation. Acquire RLock when reading/writing through the writers,
// acquire Lock when swapping writers and closing old files.
var LogWriterMu sync.RWMutex

func SysLog(s string) {
	t := time.Now()
	LogWriterMu.RLock()
	_, _ = fmt.Fprintf(LogOutput, "[SYS] %v | %s \n", t.Format("2006/01/02 - 15:04:05"), s)
	LogWriterMu.RUnlock()
}

func SysError(s string) {
	t := time.Now()
	LogWriterMu.RLock()
	_, _ = fmt.Fprintf(LogErrOutput, "[SYS] %v | %s \n", t.Format("2006/01/02 - 15:04:05"), s)
	LogWriterMu.RUnlock()
}

func FatalLog(v ...any) {
	t := time.Now()
	LogWriterMu.RLock()
	_, _ = fmt.Fprintf(LogErrOutput, "[FATAL] %v | %v \n", t.Format("2006/01/02 - 15:04:05"), v)
	LogWriterMu.RUnlock()
	os.Exit(1)
}

func LogStartupSuccess(startTime time.Time, port string) {
	duration := time.Since(startTime)
	durationMs := duration.Milliseconds()

	// Get network IPs
	networkIps := GetNetworkIps()

	LogWriterMu.RLock()
	defer LogWriterMu.RUnlock()

	if SessionCookieSecure == false {
		// Warn when the local HTTP compatibility mode disables cookie transport
		// security and refresh/logout Origin validation.
		fmt.Fprintf(LogOutput, "\n")
		fmt.Fprintf(LogOutput, "  \033[33mWarning: Refresh cookie is not secure and refresh/logout Origin validation is disabled. Please set SESSION_COOKIE_SECURE=true in production.\033[0m\n")
		fmt.Fprintf(LogOutput, "\n")
	}

	fmt.Fprintf(LogOutput, "\n")
	fmt.Fprintf(LogOutput, "  \033[32m%s %s\033[0m  ready in %d ms\n", SystemName, Version, durationMs)
	fmt.Fprintf(LogOutput, "\n")

	if !IsRunningInContainer() {
		fmt.Fprintf(LogOutput, "  ➜  \033[1mLocal:\033[0m   http://localhost:%s/\n", port)
	}

	for _, ip := range networkIps {
		fmt.Fprintf(LogOutput, "  ➜  \033[1mNetwork:\033[0m http://%s:%s/\n", ip, port)
	}

	fmt.Fprintf(LogOutput, "\n")
}