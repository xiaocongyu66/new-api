package common

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"

	goahocorasick "github.com/anknown/ahocorasick"
)

// SundaySearch 带偏移表跳过的模式匹配
func SundaySearch(text string, pattern string) bool {
	offset := make(map[rune]int)
	for i, c := range pattern {
		offset[c] = len(pattern) - i
	}
	n, m := len(text), len(pattern)
	for i := 0; i <= n-m; {
		j := 0
		for j < m && text[i+j] == pattern[j] {
			j++
		}
		if j == m {
			return true
		}
		if i+m < n {
			next := rune(text[i+m])
			if val, ok := offset[next]; ok {
				i += val
			} else {
				i += len(pattern) + 1
			}
		} else {
			break
		}
	}
	return false
}

func RemoveDuplicate(s []string) []string {
	result := make([]string, 0, len(s))
	temp := map[string]struct{}{}
	for _, item := range s {
		if _, ok := temp[item]; !ok {
			temp[item] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

func InitAc(dict []string) *goahocorasick.Machine {
	m := new(goahocorasick.Machine)
	runes := readRunes(dict)
	if err := m.Build(runes); err != nil {
		return nil
	}
	return m
}

var acCache sync.Map

func acKey(dict []string) string {
	if len(dict) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(dict))
	for _, w := range dict {
		w = strings.ToLower(strings.TrimSpace(w))
		if w != "" {
			normalized = append(normalized, w)
		}
	}
	if len(normalized) == 0 {
		return ""
	}
	sort.Strings(normalized)
	hasher := fnv.New64a()
	for _, w := range normalized {
		hasher.Write([]byte{0})
		hasher.Write([]byte(w))
	}
	return fmt.Sprintf("%x", hasher.Sum64())
}

func GetOrBuildAC(dict []string) *goahocorasick.Machine {
	key := acKey(dict)
	if key == "" {
		return nil
	}
	if cached, ok := acCache.Load(key); ok {
		return cached.(*goahocorasick.Machine)
	}
	m := InitAc(dict)
	if m != nil {
		acCache.Store(key, m)
	}
	return m
}

func readRunes(dict []string) [][]rune {
	runes := make([][]rune, 0, len(dict))
	for _, w := range dict {
		w = strings.ToLower(strings.TrimSpace(w))
		if w != "" {
			runes = append(runes, bytes.Runes([]byte(w)))
		}
	}
	return runes
}

func AcSearch(findText string, dict []string, stopImmediately bool) (bool, []string) {
	if len(dict) == 0 {
		return false, nil
	}
	if len(findText) == 0 {
		return false, nil
	}
	m := GetOrBuildAC(dict)
	if m == nil {
		return false, nil
	}
	hits := m.MultiPatternSearch([]rune(findText), stopImmediately)
	if len(hits) > 0 {
		words := make([]string, 0, len(hits))
		for _, hit := range hits {
			words = append(words, string(hit.Word))
		}
		return true, words
	}
	return false, nil
}