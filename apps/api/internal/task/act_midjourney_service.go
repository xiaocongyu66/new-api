package task

import (
	"context"
	"encoding/json"
	"github.com/QuantumNous/new-api/internal/egress"
	"github.com/QuantumNous/new-api/internal/usage"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/dto"
	"github.com/QuantumNous/new-api/internal/logger"

	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// DoMidjourneyHttpRequest forwards an HTTP request to the Midjourney upstream.
func DoMidjourneyHttpRequest(c contract.Context, timeout time.Duration, fullRequestURL string) (*dto.MidjourneyResponseWithStatusCode, []byte, error) {
	var nullBytes []byte
	if c.Method() != http.MethodGet {
		var mapResult map[string]interface{}
		body, err := c.RawBody()
		if err != nil {
			return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "read_request_body_failed", http.StatusInternalServerError), nullBytes, err
		}
		err = json.Unmarshal(body, &mapResult)
		if err != nil {
			return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "read_request_body_failed", http.StatusInternalServerError), nullBytes, err
		}
		if !usage.MjAccountFilterEnabled {
			delete(mapResult, "accountFilter")
		}
		if !usage.MjNotifyEnabled {
			delete(mapResult, "notifyHook")
		}
		if usage.MjModeClearEnabled {
			if prompt, ok := mapResult["prompt"].(string); ok {
				prompt = strings.Replace(prompt, "--fast", "", -1)
				prompt = strings.Replace(prompt, "--relax", "", -1)
				prompt = strings.Replace(prompt, "--turbo", "", -1)
				mapResult["prompt"] = prompt
			}
		}
		reqBody, err := json.Marshal(mapResult)
		if err != nil {
			return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "marshal_request_body_failed", http.StatusInternalServerError), nullBytes, err
		}
		c.ReplaceBody(reqBody)
	}

	body, err := c.BodyReader()
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "read_request_body_failed", http.StatusInternalServerError), nullBytes, err
	}
	req, err := http.NewRequest(c.Method(), fullRequestURL, body)
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "create_request_failed", http.StatusInternalServerError), nullBytes, err
	}
	ctx, cancel := context.WithTimeout(c.Context(), timeout)
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", c.Header("Content-Type"))
	req.Header.Set("Accept", c.Header("Accept"))
	auth := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	if auth != "" {
		auth = strings.TrimPrefix(auth, "Bearer ")
		req.Header.Set("mj-api-secret", auth)
	}
	defer cancel()
	resp, err := egress.GetHttpClient().Do(req)
	if err != nil {
		common.SysLog("do request failed: " + err.Error())
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "do_request_failed", http.StatusInternalServerError), nullBytes, err
	}
	statusCode := resp.StatusCode
	err = req.Body.Close()
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "close_request_body_failed", statusCode), nullBytes, err
	}
	var midjResponse dto.MidjourneyResponse
	var midjourneyUploadsResponse dto.MidjourneyUploadResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "read_response_body_failed", statusCode), nullBytes, err
	}
	egress.CloseResponseBodyGracefully(resp)
	logger.LogDebug(c.Context(), "midjourney response body: %s", responseBody)
	if len(responseBody) == 0 {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "empty_response_body", statusCode), responseBody, nil
	} else {
		err = json.Unmarshal(responseBody, &midjResponse)
		if err != nil {
			err2 := json.Unmarshal(responseBody, &midjourneyUploadsResponse)
			if err2 != nil {
				return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "unmarshal_response_body_failed", statusCode), responseBody, err
			}
		}
	}
	return &dto.MidjourneyResponseWithStatusCode{
		StatusCode: statusCode,
		Response:   midjResponse,
	}, responseBody, nil
}
