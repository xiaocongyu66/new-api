package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/dto"
	"github.com/QuantumNous/new-api/internal/logger"

	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage/record_perf_config"
)

// DoMidjourneyHttpRequest forwards an HTTP request to the Midjourney upstream.
// This function is bound to gin.Context and cannot be moved to the capability layer.
func DoMidjourneyHttpRequest(c contract.Context, timeout time.Duration, fullRequestURL string) (*dto.MidjourneyResponseWithStatusCode, []byte, error) {
	var nullBytes []byte
	if c.HTTPRequest().Method != "GET" {
		var mapResult map[string]interface{}
		err := json.NewDecoder(c.HTTPRequest().Body).Decode(&mapResult)
		if err != nil {
			return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "read_request_body_failed", http.StatusInternalServerError), nullBytes, err
		}
		if !record_perf_config.MjAccountFilterEnabled {
			delete(mapResult, "accountFilter")
		}
		if !record_perf_config.MjNotifyEnabled {
			delete(mapResult, "notifyHook")
		}
		if record_perf_config.MjModeClearEnabled {
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
		c.HTTPRequest().Body = io.NopCloser(strings.NewReader(string(reqBody)))
	}

	req, err := http.NewRequest(c.HTTPRequest().Method, fullRequestURL, c.HTTPRequest().Body)
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "create_request_failed", http.StatusInternalServerError), nullBytes, err
	}
	ctx, cancel := context.WithTimeout(c.HTTPRequest().Context(), timeout)
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", c.HTTPRequest().Header.Get("Content-Type"))
	req.Header.Set("Accept", c.HTTPRequest().Header.Get("Accept"))
	auth := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	if auth != "" {
		auth = strings.TrimPrefix(auth, "Bearer ")
		req.Header.Set("mj-api-secret", auth)
	}
	defer cancel()
	resp, err := GetHttpClient().Do(req)
	if err != nil {
		common.SysLog("do request failed: " + err.Error())
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "do_request_failed", http.StatusInternalServerError), nullBytes, err
	}
	statusCode := resp.StatusCode
	err = req.Body.Close()
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "close_request_body_failed", statusCode), nullBytes, err
	}
	err = c.HTTPRequest().Body.Close()
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "close_request_body_failed", statusCode), nullBytes, err
	}
	var midjResponse dto.MidjourneyResponse
	var midjourneyUploadsResponse dto.MidjourneyUploadResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "read_response_body_failed", statusCode), nullBytes, err
	}
	CloseResponseBodyGracefully(resp)
	logger.LogDebug(c.HTTPRequest().Context(), "midjourney response body: %s", responseBody)
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
