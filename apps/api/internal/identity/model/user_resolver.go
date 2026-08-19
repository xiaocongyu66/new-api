package model

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/model"
)

type userResolver struct{}

func (userResolver) GetUsernameById(id int, fromDB bool) (string, error) {
	return GetUsernameById(id, fromDB)
}

func (userResolver) GetUserSetting(id int, fromDB bool) (map[string]any, error) {
	setting, err := GetUserSetting(id, fromDB)
	if err != nil {
		return nil, err
	}
	// 返回 map[string]any 以避免对外暴露 relaykit/dto 依赖。
	raw, _ := json.Marshal(setting)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out, nil
}

func init() {
	model.RegisterUserResolver(userResolver{})
}
