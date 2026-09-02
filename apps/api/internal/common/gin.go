package common

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/pkg/errors"
)

const KeyRequestBody = "key_request_body"
const KeyBodyStorage = "key_body_storage"

var ErrRequestBodyTooLarge = errors.New("request body too large")

func IsRequestBodyTooLargeError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrRequestBodyTooLarge) {
		return true
	}
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

func GetRequestBody(c contract.Context) (io.Seeker, error) {
	// 首先检查是否有 BodyStorage 缓存
	if storage, exists := c.Get(KeyBodyStorage); exists && storage != nil {
		if bs, ok := storage.(BodyStorage); ok {
			if _, err := bs.Seek(0, io.SeekStart); err != nil {
				return nil, fmt.Errorf("failed to seek body storage: %w", err)
			}
			return bs, nil
		}
	}

	// 检查旧的缓存方式
	cached, exists := c.Get(KeyRequestBody)
	if exists && cached != nil {
		if b, ok := cached.([]byte); ok {
			bs, err := CreateBodyStorage(b)
			if err != nil {
				return nil, err
			}
			c.Set(KeyBodyStorage, bs)
			return bs, nil
		}
	}

	maxMB := constant.MaxRequestBodyMB
	if maxMB <= 0 {
		maxMB = 128 // 默认 128MB
	}
	maxBytes := int64(maxMB) << 20

	contentLength := c.ContentLength()

	// TODO(#287) A: concrete *http.Request read of the raw inbound body stream, which
	// bootstraps the replayable-body cache. BodyReader delegates back here, so routing
	// this through the contract would recurse; the Fiber-side resolution is to
	// implement this bootstrap inside each adapter rather than in shared code.
	request := c.HTTPRequest()
	storage, err := CreateBodyStorageFromReader(request.Body, contentLength, maxBytes)
	_ = request.Body.Close()

	if err != nil {
		if IsRequestBodyTooLargeError(err) {
			return nil, errors.Wrap(ErrRequestBodyTooLarge, fmt.Sprintf("request body exceeds %d MB", maxMB))
		}
		return nil, err
	}

	// 缓存存储对象
	c.Set(KeyBodyStorage, storage)

	return storage, nil
}

// GetBodyStorage 获取请求体存储对象（用于需要多次读取的场景）
func GetBodyStorage(c contract.Context) (BodyStorage, error) {
	seeker, err := GetRequestBody(c)
	if err != nil {
		return nil, err
	}
	bs, ok := seeker.(BodyStorage)
	if !ok {
		return nil, errors.New("unexpected body storage type")
	}
	return bs, nil
}

// CleanupBodyStorage 清理请求体存储（应在请求结束时调用）
func CleanupBodyStorage(c contract.Context) {
	if storage, exists := c.Get(KeyBodyStorage); exists && storage != nil {
		if bs, ok := storage.(BodyStorage); ok {
			bs.Close()
		}
		c.Set(KeyBodyStorage, nil)
	}
}

func UnmarshalBodyReusable(c contract.Context, v any) error {
	storage, err := GetBodyStorage(c)
	if err != nil {
		return err
	}
	contentType := c.ContentType()
	if contentType == "" {
		// callers that previously decoded the raw stream (DecodeJson) accepted
		// bodies regardless of content-type; keep that behaviour for the
		// replayable path too
		contentType = "application/json"
	}

	// disk-backed JSON: stream-decode directly from the file to avoid
	// materializing the entire payload back into a transient []byte
	// (diskStorage.Bytes() would ReadFull the whole file into the heap).
	if storage.IsDisk() && strings.HasPrefix(contentType, "application/json") {
		if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
			return seekErr
		}
		if err := DecodeJson(storage, v); err != nil {
			return err
		}
		if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
			return seekErr
		}
		c.ResetBody(io.NopCloser(storage))
		return nil
	}

	requestBody, err := storage.Bytes()
	if err != nil {
		return err
	}
	if len(requestBody) == 0 {
		// Nothing to decode: leave v at its zero value. Bodyless requests
		// (GET/DELETE) reach here through middleware that inspects the body for
		// a model name, and decoding zero bytes as JSON fails with
		// "unexpected end of JSON input", rejecting the request outright.
		// Before the content-type coercion above was introduced, an absent
		// Content-Type fell through to the no-op branch below; this restores
		// that behaviour for the coerced path.
	} else if strings.HasPrefix(contentType, "application/json") {
		err = Unmarshal(requestBody, v)
	} else if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		err = parseFormData(requestBody, v)
	} else if strings.Contains(contentType, "multipart/form-data") {
		err = parseMultipartFormData(c, requestBody, v)
	} else {
		// skip for now
		// TODO: someday non json request have variant model, we will need to implementation this
	}
	if err != nil {
		return err
	}
	// Reset request body
	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		return seekErr
	}
	c.ResetBody(io.NopCloser(storage))
	return nil
}

func SetContextKey(c contract.Context, key constant.ContextKey, value any) {
	c.Set(string(key), value)
}

func GetContextKey(c contract.Context, key constant.ContextKey) (any, bool) {
	return c.Get(string(key))
}

func GetContextKeyString(c contract.Context, key constant.ContextKey) string {
	return c.GetString(string(key))
}

func GetContextKeyInt(c contract.Context, key constant.ContextKey) int {
	return c.GetInt(string(key))
}

func GetContextKeyBool(c contract.Context, key constant.ContextKey) bool {
	return c.GetBool(string(key))
}

func GetContextKeyStringMap(c contract.Context, key constant.ContextKey) map[string]any {
	return c.GetStringMap(string(key))
}

func GetContextKeyTime(c contract.Context, key constant.ContextKey) time.Time {
	return c.GetTime(string(key))
}

func GetContextKeyType[T any](c contract.Context, key constant.ContextKey) (T, bool) {
	if value, ok := c.Get(string(key)); ok {
		if v, ok := value.(T); ok {
			return v, true
		}
	}
	var t T
	return t, false
}

func ApiError(c contract.Context, err error) {
	c.JSON(http.StatusOK, H{
		"success": false,
		"message": err.Error(),
	})
}

func ApiErrorMsg(c contract.Context, msg string) {
	c.JSON(http.StatusOK, H{
		"success": false,
		"message": msg,
	})
}

func ApiSuccess(c contract.Context, data any) {
	c.JSON(http.StatusOK, H{
		"success": true,
		"message": "",
		"data":    data,
	})
}

// ApiErrorI18n returns a translated error message based on the user's language preference
// key is the i18n message key, args is optional template data
func ApiErrorI18n(c contract.Context, key string, args ...map[string]any) {
	msg := TranslateMessage(c, key, args...)
	c.JSON(http.StatusOK, H{
		"success": false,
		"message": msg,
	})
}

// ApiSuccessI18n returns a translated success message based on the user's language preference
func ApiSuccessI18n(c contract.Context, key string, data any, args ...map[string]any) {
	msg := TranslateMessage(c, key, args...)
	c.JSON(http.StatusOK, H{
		"success": true,
		"message": msg,
		"data":    data,
	})
}

// TranslateMessage is a helper function that calls i18n.T
// This function is defined here to avoid circular imports
// The actual implementation will be set during init
var TranslateMessage func(c contract.Context, key string, args ...map[string]any) string

func init() {
	// Default implementation that returns the key as-is
	// This will be replaced by i18n.T during i18n initialization
	TranslateMessage = func(c contract.Context, key string, args ...map[string]any) string {
		c.SetHeader("X-Translate-id", "d5e7afdfc7f03414b941f9c1e7096be9966510e7")
		return key
	}
}

func ParseMultipartFormReusable(c contract.Context) (*multipart.Form, error) {
	storage, err := GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	requestBody, err := storage.Bytes()
	if err != nil {
		return nil, err
	}

	// Use the original Content-Type saved on first call to avoid boundary
	// mismatch when callers overwrite c.Request.Header after multipart rebuild.
	var contentType string
	if saved, ok := c.Get("_original_multipart_ct"); ok {
		contentType = saved.(string)
	} else {
		contentType = c.Header("Content-Type")
		c.Set("_original_multipart_ct", contentType)
	}
	boundary, err := parseBoundary(contentType)
	if err != nil {
		return nil, err
	}

	reader := multipart.NewReader(bytes.NewReader(requestBody), boundary)
	form, err := reader.ReadForm(multipartMemoryLimit())
	if err != nil {
		return nil, err
	}

	// Reset request body
	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		return nil, seekErr
	}
	c.ResetBody(io.NopCloser(storage))
	c.SetParsedForm(form)
	return form, nil
}

func processFormMap(formMap map[string]any, v any) error {
	jsonData, err := Marshal(formMap)
	if err != nil {
		return err
	}

	err = Unmarshal(jsonData, v)
	if err != nil {
		return err
	}

	return nil
}

func parseFormData(data []byte, v any) error {
	values, err := url.ParseQuery(string(data))
	if err != nil {
		return err
	}
	formMap := make(map[string]any)
	for key, vals := range values {
		if len(vals) == 1 {
			formMap[key] = vals[0]
		} else {
			formMap[key] = vals
		}
	}

	return processFormMap(formMap, v)
}

func parseMultipartFormData(c contract.Context, data []byte, v any) error {
	var contentType string
	if saved, ok := c.Get("_original_multipart_ct"); ok {
		contentType = saved.(string)
	} else {
		contentType = c.Header("Content-Type")
		c.Set("_original_multipart_ct", contentType)
	}
	boundary, err := parseBoundary(contentType)
	if err != nil {
		if errors.Is(err, errBoundaryNotFound) {
			return Unmarshal(data, v) // Fallback to JSON
		}
		return err
	}

	reader := multipart.NewReader(bytes.NewReader(data), boundary)
	form, err := reader.ReadForm(multipartMemoryLimit())
	if err != nil {
		return err
	}
	defer form.RemoveAll()
	formMap := make(map[string]any)
	for key, vals := range form.Value {
		if len(vals) == 1 {
			formMap[key] = vals[0]
		} else {
			formMap[key] = vals
		}
	}

	return processFormMap(formMap, v)
}

var errBoundaryNotFound = errors.New("multipart boundary not found")

// parseBoundary extracts the multipart boundary from the Content-Type header using mime.ParseMediaType
func parseBoundary(contentType string) (string, error) {
	if contentType == "" {
		return "", errBoundaryNotFound
	}
	// Boundary-UUID / boundary-------xxxxxx
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", err
	}
	boundary, ok := params["boundary"]
	if !ok || boundary == "" {
		return "", errBoundaryNotFound
	}
	return boundary, nil
}

// multipartMemoryLimit returns the configured multipart memory limit in bytes
func multipartMemoryLimit() int64 {
	limitMB := constant.MaxFileDownloadMB
	if limitMB <= 0 {
		limitMB = 32
	}
	return int64(limitMB) << 20
}
