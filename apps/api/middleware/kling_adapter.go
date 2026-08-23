package middleware

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

func KlingRequestConvert() func(c contract.Context) {
	return func(c contract.Context) {
		var originalReq map[string]interface{}
		if err := common.UnmarshalCtxBodyReusable(c, &originalReq); err != nil {
			c.Next()
			return
		}

		// Support both model_name and model fields
		model, _ := originalReq["model_name"].(string)
		if model == "" {
			model, _ = originalReq["model"].(string)
		}
		prompt, _ := originalReq["prompt"].(string)

		unifiedReq := map[string]interface{}{
			"model":    model,
			"prompt":   prompt,
			"metadata": originalReq,
		}

		jsonData, err := json.Marshal(unifiedReq)
		if err != nil {
			c.Next()
			return
		}

		// Rewrite request body and path
		c.ReplaceBody(jsonData)
		c.SetPath("/v1/video/generations")
		if image, ok := originalReq["image"]; !ok || image == "" {
			c.Set("action", constant.TaskActionTextGenerate)
		}

		// We have to reset the request body for the next handlers
		c.Set(common.KeyRequestBody, jsonData)
		c.Next()
	}
}
