package service

import (
	"strings"
	"sync"
)

// ──────────────────────────────────────────────────────────────
// byteAC —— 字节级 Aho-Corasick（热路径替代 anko 双数组机）
//
// 语义与 anko MultiPatternSearch 一致：
//   - 关键字小写 + TrimSpace（raw 变体不裁剪尾空白，模板专用）
//   - 命中词按文本中结束位置的次输出（左→右）；同一结束位置按关键字下标升序
//   - stopImmediately=true 时首个产出态即返回
//   - UTF-8 完整关键字均为有效字节序列，字节级 trie 与 rune 级等价
//
// 目标：字典扫描从 ~20ns/字符（anko 的 map 查找）降到 ~1ns/字节（稠密表）。
// 边界：状态数按 int32 计（~21 亿），受词库规模约束（本仓库敏感词/模板
// 各数千条）；输入含非法 UTF-8 时，上游 scanAndLower 已把非法字节归一为
// U+FFFD（合法序列），字节级搜索只会找不到对应路径而回落根节点，不会越界。
// ──────────────────────────────────────────────────────────────

type byteAC struct {
	next   [][256]int32 // 稠密转移表；-1 = 无转移（根对缺失字节自环）
	fail   []int32
	output [][]int32 // 各状态命中词下标（关键字原序）
	words  []string  // 归一化关键字
}

func buildByteAC(dict []string, raw bool) *byteAC {
	words := make([]string, 0, len(dict))
	for _, w := range dict {
		if raw {
			w = strings.ToLower(w)
		} else {
			w = strings.ToLower(strings.TrimSpace(w))
		}
		if w != "" {
			words = append(words, w)
		}
	}
	if len(words) == 0 {
		return nil
	}
	// 构建期稀疏表
	type node struct {
		next map[byte]int32
		outs []int32
	}
	nodes := make([]node, 1)
	nodes[0].next = make(map[byte]int32)
	for i, w := range words {
		s := 0
		for j := 0; j < len(w); j++ {
			c := w[j]
			ns, ok := nodes[s].next[c]
			if !ok {
				ns = int32(len(nodes))
				nodes = append(nodes, node{next: make(map[byte]int32)})
				nodes[s].next[c] = ns
			}
			s = int(ns)
		}
		nodes[s].outs = append(nodes[s].outs, int32(i))
	}

	n := len(nodes)
	m := &byteAC{
		next:   make([][256]int32, n),
		fail:   make([]int32, n),
		output: make([][]int32, n),
		words:  words,
	}
	for i := 0; i < n; i++ {
		for c := 0; c < 256; c++ {
			m.next[i][c] = -1
		}
		m.fail[i] = -1
		m.output[i] = nodes[i].outs
	}
	// BFS：填 fail + 转移完备化
	queue := make([]int32, 0, n)
	for c := 0; c < 256; c++ {
		bc := byte(c)
		if s, ok := nodes[0].next[bc]; ok {
			m.next[0][c] = s
			m.fail[s] = 0
			queue = append(queue, s)
		} else {
			m.next[0][c] = 0 // 根自环
		}
	}
	for head := 0; head < len(queue); head++ {
		s := queue[head]
		for c := 0; c < 256; c++ {
			bc := byte(c)
			// 不变量：m.fail[0] == -1 且 m.next[0][c] 对全部 c 非负（上面根循环已填），
			// 因此下面 `f = m.fail[f]` 回退循环必然在 f==0 处终止，不会读 m.fail[0]。
			if ns, ok := nodes[s].next[bc]; ok {
				// 子节点的 fail = fail[父] 走同字节
				f := m.fail[s]
				for m.next[f][c] == -1 {
					f = m.fail[f]
				}
				m.fail[ns] = m.next[f][c]
				m.next[s][c] = ns
				queue = append(queue, ns)
			} else {
				// 缺失转移 = fail 链的转移
				f := m.fail[s]
				for m.next[f][c] == -1 {
					f = m.fail[f]
				}
				m.next[s][c] = m.next[f][c]
			}
		}
	}
	return m
}

// search 与 anko MultiPatternSearch 语义一致；返回命中词下标序列。
func (m *byteAC) search(text string, stopImmediately bool) []int32 {
	state := 0
	var hits []int32
	for i := 0; i < len(text); i++ {
		state = int(m.next[state][text[i]])
		if out := m.output[state]; len(out) > 0 {
			hits = append(hits, out...)
			if stopImmediately {
				break
			}
		}
	}
	return hits
}

// ──────────────────────────────────────────────────────────────
// 缓存（与 dictMemo 同构：切片指针 + 首尾抽样）
// ──────────────────────────────────────────────────────────────

type byteKeyed struct {
	key string
	ac  *byteAC
}

type byteMemo struct {
	mu      sync.Mutex
	by      map[dictCacheKey]*byteKeyed
	lastKey dictCacheKey
	lastVal *byteKeyed
}

func newByteMemo() *byteMemo {
	return &byteMemo{by: make(map[dictCacheKey]*byteKeyed, 4)}
}

func (m *byteMemo) Get(rawDict []string, raw bool) *byteAC {
	if len(rawDict) == 0 {
		return nil
	}
	k := makeDictCacheKey(rawDict)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastKey == k && m.lastVal != nil {
		return m.lastVal.ac
	}
	if km, ok := m.by[k]; ok {
		m.lastKey, m.lastVal = k, km
		return km.ac
	}
	// 全部字空白/空串的词库：acKey 为空就直接返回，不建缓存。
	// 与 dictMemo 相同的后备约定：调用方必须传「内容稳定」的切片
	// （setting 每次解析重建）；若底层数组被 GC 复用且首/尾词与长度
	// 恰好一致，会命中旧机器——与既有营方敏感词缓存的取舍一致，见 str.go。
	key := acKey(rawDict)
	if key == "" {
		return nil
	}
	// buildByteAC 在锁内执行：首次构建偶发拖慢并发请求，但构建只发生一次
	// （dict 配置变更时）；与 dictMemo 的取舍一致，避免双检锁复杂度。
	km := &byteKeyed{key: key, ac: buildByteAC(rawDict, raw)}
	if km.ac == nil {
		return nil
	}
	m.by[k] = km
	if len(m.by) > 16 {
		m.by = make(map[dictCacheKey]*byteKeyed, 4)
	}
	m.lastKey, m.lastVal = k, km
	return km.ac
}

var (
	acBytesMemo    = newByteMemo()
	acBytesRawMemo = newByteMemo()
)

func getOrBuildByteAC(dict []string) *byteAC {
	return acBytesMemo.Get(dict, false)
}

// getOrBuildByteACRaw 模板专用（不裁剪关键字首尾空白）。
func getOrBuildByteACRaw(dict []string) *byteAC {
	return acBytesRawMemo.Get(dict, true)
}

// getOrBuildByteACTech tech 组模板专用机器（rp 组关闭时的默认路径）。
func getOrBuildByteACTech() *byteAC {
	if len(sensitiveTemplatesTech) == 0 {
		return nil
	}
	return acBytesRawMemo.Get(sensitiveTemplatesTech, true)
}
