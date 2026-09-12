package common

import (
	"sync"
)

var defaultTopupGroupRatio = map[string]float64{
	"default": 1,
	"vip":     1,
	"svip":    1,
}
var topupGroupRatio = map[string]float64{
	"default": 1,
	"vip":     1,
	"svip":    1,
}
var topupGroupRatioMutex sync.RWMutex

func TopupGroupRatio2JSONString() string {
	topupGroupRatioMutex.RLock()
	defer topupGroupRatioMutex.RUnlock()
	jsonBytes, err := Marshal(topupGroupRatio)
	if err != nil {
		SysError("error marshalling topup group ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateTopupGroupRatioByJSONString(jsonStr string) error {
	loaded := map[string]float64{}
	if err := Unmarshal([]byte(jsonStr), &loaded); err != nil {
		return err
	}
	// Option reloads replace the whole map; keep the hardcoded fallback
	// groups present so GetTopupGroupRatio never silently drifts to its 1.0
	// fallback for them.
	for k, v := range defaultTopupGroupRatio {
		if _, ok := loaded[k]; !ok {
			loaded[k] = v
		}
	}
	topupGroupRatioMutex.Lock()
	defer topupGroupRatioMutex.Unlock()
	topupGroupRatio = loaded
	return nil
}

func ContainsTopupGroupRatio(name string) bool {
	topupGroupRatioMutex.RLock()
	defer topupGroupRatioMutex.RUnlock()
	_, ok := topupGroupRatio[name]
	return ok
}

func GetTopupGroupRatio(name string) float64 {
	topupGroupRatioMutex.RLock()
	defer topupGroupRatioMutex.RUnlock()
	ratio, ok := topupGroupRatio[name]
	if !ok {
		SysError("topup group ratio not found: " + name)
		return 1
	}
	return ratio
}
