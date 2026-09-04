package middleware

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"runtime/debug"

	"github.com/QuantumNous/new-api/internal/common"
)

func RelayPanicRecover() contract.Middleware {
	return func(c contract.Context) {
		defer func() {
			if err := recover(); err != nil {
				common.SysLog(fmt.Sprintf("panic detected: %v", err))
				common.SysLog(fmt.Sprintf("stacktrace from panic: %s", string(debug.Stack())))
				_ = c.JSON(http.StatusInternalServerError, common.H{
					"error": common.H{
						"message": fmt.Sprintf("Panic detected, error: %v. Please submit a issue here: https://github.com/Calcium-Ion/new-api", err),
						"type":    "new_api_panic",
					},
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}
