package middleware

import (
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
)

func AnonymousRequestBodyLimit() contract.Middleware {
	return func(c contract.Context) {
		maxBytes := common.GetAnonymousRequestBodyLimitBytes()
		if maxBytes <= 0 || c.ContentLength() == 0 {
			c.Next()
			return
		}

		originalBody, err := c.BodyReader()
		if err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		limitedBody, err := readAnonymousRequestBody(originalBody, maxBytes)
		_ = originalBody.Close()
		if err != nil {
			if common.IsRequestBodyTooLargeError(err) {
				c.AbortWithStatus(http.StatusRequestEntityTooLarge)
				return
			}
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		c.ReplaceBody(limitedBody)
		c.Next()
	}
}

func readAnonymousRequestBody(body io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, common.ErrRequestBodyTooLarge
	}
	return data, nil
}
