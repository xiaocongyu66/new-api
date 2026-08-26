package middleware

import (
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"net/url"

	"github.com/QuantumNous/new-api/common"
)

type turnstileCheckResponse struct {
	Success bool `json:"success"`
}

func TurnstileCheck() contract.Middleware {
	return func(c contract.Context) {
		if common.TurnstileCheckEnabled {
			response := c.Query("turnstile")
			if response == "" {
				_ = c.JSON(http.StatusOK, common.H{
					"success": false,
					"message": "Turnstile token 为空",
				})
				c.Abort()
				return
			}
			rawRes, err := http.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", url.Values{
				"secret":   {common.TurnstileSecretKey},
				"response": {response},
				"remoteip": {c.ClientIP()},
			})
			if err != nil {
				common.SysLog(err.Error())
				_ = c.JSON(http.StatusOK, common.H{
					"success": false,
					"message": err.Error(),
				})
				c.Abort()
				return
			}
			defer rawRes.Body.Close()
			var res turnstileCheckResponse
			err = common.DecodeJson(rawRes.Body, &res)
			if err != nil {
				common.SysLog(err.Error())
				_ = c.JSON(http.StatusOK, common.H{
					"success": false,
					"message": err.Error(),
				})
				c.Abort()
				return
			}
			if !res.Success {
				_ = c.JSON(http.StatusOK, common.H{
					"success": false,
					"message": "Turnstile 校验失败，请刷新重试！",
				})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
