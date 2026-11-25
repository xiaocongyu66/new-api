package upstreamfetch

// scrape 档：解析服务端渲染的 HTML。
//
// 只实现取数真正需要的一小撮选择器语法，刻意不做完整 CSS：
//
//	div                    标签
//	.price                 类名
//	#main                   id
//	td[data-field=price]    属性等值
//	table tbody tr          后代组合
//
// 之所以只用这一小撮：选择器越花，上游改版时越容易静默失配。窄语法配合
// 严格的「匹配不到就整次失败」，出问题时是响亮的失败，不是错数字。

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"golang.org/x/net/html"
)

func fetchScrape(ctx context.Context, channel *channelpkg.Channel, target string, spec Spec) ([]Row, error) {
	timeout := time.Duration(spec.TimeoutSec) * time.Second
	body, err := Get(ctx, target, timeout, defaultBodyLimit, func(req *http.Request) {
		ApplyAuth(req, channel, spec.Auth)
		for k, v := range spec.Headers {
			req.Header.Set(k, v)
		}
	})
	if err != nil {
		return nil, err
	}
	rows, err := extractRowsFromHTML(body, spec)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// extractRowsFromHTML 抽成纯函数以便单测：解析逻辑必须能脱离 HTTP 验证。
func extractRowsFromHTML(document []byte, spec Spec) ([]Row, error) {
	root, err := html.Parse(bytes.NewReader(document))
	if err != nil {
		return nil, fmt.Errorf("unparseable html: %w", err)
	}
	nodes := selectNodes(root, spec.Rows)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("rows selector %q matched nothing", spec.Rows)
	}

	var rows []Row
	for _, node := range nodes {
		keyNode := firstMatch(node, spec.Key)
		if keyNode == nil {
			continue
		}
		key := nodeText(keyNode)
		if key == "" {
			continue
		}
		values := make(map[string]string, len(spec.Values))
		for name, selector := range spec.Values {
			// 选择器可以带 @attr 后缀直接取属性，例如 a@href。
			// 必须先拆分再匹配：@ 不是选择器语法的一部分，带着它去匹配
			// 只会把属性名当成标签名，永远匹配不到。
			selector, attr, hasAttr := splitAttrSelector(selector)
			valueNode := firstMatch(node, selector)
			if valueNode == nil {
				continue
			}
			if hasAttr {
				if text := nodeAttr(valueNode, attr); text != "" {
					values[name] = text
				}
				continue
			}
			if text := nodeText(valueNode); text != "" {
				values[name] = text
			}
		}
		rows = append(rows, Row{Key: key, Values: values})
	}
	return rows, nil
}

// splitAttrSelector 拆出 "selector@attr" 形式。
func splitAttrSelector(selector string) (string, string, bool) {
	index := strings.LastIndex(selector, "@")
	if index <= 0 || index == len(selector)-1 {
		return selector, "", false
	}
	return selector[:index], selector[index+1:], true
}

// firstMatch 先按「以当前节点为根的子树」找，再退回整棵树。
// 这样同一条选择器既能描述行内相对路径，也能描述绝对路径。
func firstMatch(scope *html.Node, selector string) *html.Node {
	if strings.TrimSpace(selector) == "" {
		return nil
	}
	if found := selectNodesWithin(scope, selector); len(found) > 0 {
		return found[0]
	}
	return nil
}

// ---- 选择器引擎 ----

type compound struct {
	tag       string
	id        string
	classes   []string
	attrName  string
	attrValue string
}

// selectNodes 在整个文档里匹配选择器，返回文档顺序的命中节点。
func selectNodes(root *html.Node, selector string) []*html.Node {
	return selectNodesWithin(root, selector)
}

func selectNodesWithin(root *html.Node, selector string) []*html.Node {
	parts := splitSelector(selector)
	if len(parts) == 0 {
		return nil
	}
	compounds := make([]compound, 0, len(parts))
	for _, part := range parts {
		parsed, err := parseCompound(part)
		if err != nil {
			return nil
		}
		compounds = append(compounds, parsed)
	}

	var out []*html.Node
	var walk func(node *html.Node, depth int)
	walk = func(node *html.Node, depth int) {
		if node.Type == html.ElementNode && matchesChain(node, compounds) {
			out = append(out, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, depth+1)
		}
	}
	walk(root, 0)
	return out
}

// matchesChain 判定节点是否匹配最后一个 compound，且其祖先依次匹配前面各级。
func matchesChain(node *html.Node, compounds []compound) bool {
	if !matchesCompound(node, compounds[len(compounds)-1]) {
		return false
	}
	remaining := len(compounds) - 2
	for parent := node.Parent; parent != nil && remaining >= 0; parent = parent.Parent {
		if parent.Type == html.ElementNode && matchesCompound(parent, compounds[remaining]) {
			remaining--
		}
	}
	return remaining < 0
}

func matchesCompound(node *html.Node, want compound) bool {
	if node.Type != html.ElementNode {
		return false
	}
	if want.tag != "" && !strings.EqualFold(node.Data, want.tag) {
		return false
	}
	if want.id != "" && nodeAttr(node, "id") != want.id {
		return false
	}
	if len(want.classes) > 0 {
		classes := strings.Fields(nodeAttr(node, "class"))
		for _, required := range want.classes {
			found := false
			for _, actual := range classes {
				if actual == required {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	if want.attrName != "" && nodeAttr(node, want.attrName) != want.attrValue {
		return false
	}
	return true
}

// splitSelector 按空白切分后代组合；不支持子代/兄弟等其它组合符。
func splitSelector(selector string) []string {
	return strings.Fields(strings.TrimSpace(selector))
}

// parseCompound 解析 "tag#id.class[attr=value]"。
func parseCompound(raw string) (compound, error) {
	var out compound
	i := 0
	for i < len(raw) {
		switch raw[i] {
		case '#':
			i++
			start := i
			for i < len(raw) && isIdentByte(raw[i]) {
				i++
			}
			out.id = raw[start:i]
		case '.':
			i++
			start := i
			for i < len(raw) && isIdentByte(raw[i]) {
				i++
			}
			out.classes = append(out.classes, raw[start:i])
		case '[':
			end := strings.IndexByte(raw[i:], ']')
			if end < 0 {
				return out, fmt.Errorf("unclosed attribute selector in %q", raw)
			}
			inner := raw[i+1 : i+end]
			if eq := strings.IndexByte(inner, '='); eq > 0 {
				out.attrName = strings.TrimSpace(inner[:eq])
				out.attrValue = strings.Trim(strings.TrimSpace(inner[eq+1:]), `"'`)
			} else {
				out.attrName = strings.TrimSpace(inner)
			}
			i += end + 1
		default:
			start := i
			for i < len(raw) && isIdentByte(raw[i]) {
				i++
			}
			if start == i {
				i++ // 跳过无法识别的字符，避免死循环
				continue
			}
			if out.tag == "" {
				out.tag = raw[start:i]
			}
		}
	}
	return out, nil
}

func isIdentByte(b byte) bool {
	return b == '-' || b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func nodeAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val
		}
	}
	return ""
}

// nodeText 收集节点下的全部文本并压缩空白。
func nodeText(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			builder.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(builder.String()), " ")
}
