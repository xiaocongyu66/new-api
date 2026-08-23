package service

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"unsafe"

	goahocorasick "github.com/anknown/ahocorasick"
)

func SundaySearch(text string, pattern string) bool {
	// 计算偏移表
	offset := make(map[rune]int)
	for i, c := range pattern {
		offset[c] = len(pattern) - i
	}

	// 文本串长度和模式串长度
	n, m := len(text), len(pattern)

	// 主循环，i表示当前对齐的文本串位置
	for i := 0; i <= n-m; {
		// 检查子串
		j := 0
		for j < m && text[i+j] == pattern[j] {
			j++
		}
		// 如果完全匹配，返回匹配位置
		if j == m {
			return true
		}

		// 如果还有剩余字符，则检查下一位字符在偏移表中的值
		if i+m < n {
			next := rune(text[i+m])
			if val, ok := offset[next]; ok {
				i += val // 存在于偏移表中，进行跳跃
			} else {
				i += len(pattern) + 1 // 不存在于偏移表中，跳过整个模式串长度
			}
		} else {
			break
		}
	}
	return false // 如果没有找到匹配，返回-1
}



func InitAc(dict []string) *goahocorasick.Machine {
	return buildMachine(dict, false)
}

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

// dictCacheKey 用「切片指针 + 长度 + 首尾抽样」做键：词库仅在配置更新时换新切片，
// 指针不变即内容不变（setting.SensitiveWords 每次解析都是新分配）。
// 完整 acKey（小写+trim+排序+全文哈希）只在指针/SliceHeader 变化时重算，
// 避免热路径上每次调用 O(n log n)。
type dictCacheKey struct {
	ptr   uintptr
	len   int
	first string
	last  string
}

func makeDictCacheKey(dict []string) dictCacheKey {
	k := dictCacheKey{len: len(dict)}
	if len(dict) > 0 {
		k.ptr = uintptr(unsafe.Pointer(&dict[0]))
		k.first = dict[0]
		k.last = dict[len(dict)-1]
	}
	return k
}

// keyedMachine 把 dict 引用 → 已构建机器 + 其全文哈希键。同一切片指针复用键。
type keyedMachine struct {
	key     string
	machine *goahocorasick.Machine
}

// dictKeyMemo 按 (指针, 长度) 记忆 machine；内容变更（同指针重写）由抽样兜底重建。
type dictMemo struct {
	mu      sync.Mutex
	byKey   map[dictCacheKey]*keyedMachine
	lastKey dictCacheKey
	lastVal *keyedMachine
}

func newDictMemo() *dictMemo {
	return &dictMemo{byKey: make(map[dictCacheKey]*keyedMachine, 4)}
}

func (m *dictMemo) Get(rawDict []string, raw bool) *goahocorasick.Machine {
	if len(rawDict) == 0 {
		return nil
	}
	k := makeDictCacheKey(rawDict)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastKey == k && m.lastVal != nil {
		return m.lastVal.machine
	}
	if km, ok := m.byKey[k]; ok {
		m.lastKey, m.lastVal = k, km
		return km.machine
	}
	key := acKey(rawDict)
	if key == "" {
		return nil
	}
	mach := buildMachine(rawDict, raw)
	if mach == nil {
		return nil
	}
	km := &keyedMachine{key: key, machine: mach}
	m.byKey[k] = km
	if len(m.byKey) > 16 {
		m.byKey = make(map[dictCacheKey]*keyedMachine, 4)
	}
	m.lastKey, m.lastVal = k, km
	return mach
}

var acMemo = newDictMemo()
var acRawMemo = newDictMemo()

func getOrBuildAC(dict []string) *goahocorasick.Machine {
	return acMemo.Get(dict, false)
}

// getOrBuildACRaw 构建不裁剪首尾空白的机器（模板前缀专用）。
func getOrBuildACRaw(dict []string) *goahocorasick.Machine {
	return acRawMemo.Get(dict, true)
}

func readRunes(dict []string) [][]rune {
	var runes [][]rune

	for _, word := range dict {
		word = strings.ToLower(word)
		l := bytes.TrimSpace([]byte(word))
		runes = append(runes, bytes.Runes(l))
	}

	return runes
}

// readRunesRaw 与 readRunes 相同但不裁剪首尾空白。
// 模板前缀的尾随空白是模式的一部分（区分 "...ways: " 与 "...ways:\n" 边界），
// 裁剪会改变匹配语义（见 sensitive_expected 292 行的对齐回归）。
func readRunesRaw(dict []string) [][]rune {
	runes := make([][]rune, 0, len(dict))
	for _, word := range dict {
		runes = append(runes, []rune(strings.ToLower(word)))
	}
	return runes
}

func buildMachine(dict []string, raw bool) *goahocorasick.Machine {
	m := new(goahocorasick.Machine)
	var runes [][]rune
	if raw {
		runes = readRunesRaw(dict)
	} else {
		runes = readRunes(dict)
	}
	if err := m.Build(runes); err != nil {
		fmt.Println(err)
		return nil
	}
	return m
}

// AcSearch 字节级 AC 搜索（语义与 anko 旧实现完全一致，热路径用）。
func AcSearch(findText string, dict []string, stopImmediately bool) (bool, []string) {
	if len(dict) == 0 {
		return false, nil
	}
	if len(findText) == 0 {
		return false, nil
	}
	m := getOrBuildByteAC(dict)
	if m == nil {
		return false, nil
	}
	hits := m.search(findText, stopImmediately)
	if len(hits) > 0 {
		words := make([]string, 0, len(hits))
		for _, wi := range hits {
			words = append(words, m.words[wi])
		}
		return true, words
	}
	return false, nil
}

// AcSearchLegacy 旧 anko 双数组机实现（保留供基准对照，勿用于热路径）。
func AcSearchLegacy(findText string, dict []string, stopImmediately bool) (bool, []string) {
	if len(dict) == 0 {
		return false, nil
	}
	if len(findText) == 0 {
		return false, nil
	}
	m := getOrBuildAC(dict)
	if m == nil {
		return false, nil
	}
	hits := m.MultiPatternSearch([]rune(findText), stopImmediately)
	if len(hits) > 0 {
		words := make([]string, 0)
		for _, hit := range hits {
			words = append(words, string(hit.Word))
		}
		return true, words
	}
	return false, nil
}
