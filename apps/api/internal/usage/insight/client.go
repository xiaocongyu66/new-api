package insight

import (
	"net/http"
	"regexp"
	"strings"
)

// clientRule 描述一个客户端的识别规则。
// UA / Header 命中给出较高置信度（客户端很难伪造得完全一致），
// PromptMarker 命中说明请求体里带着该工具特有的注入提示词，
// 两者同时命中则置信度拉满。
type clientRule struct {
	ID            string
	Name          string
	Kind          string
	UASubstrings  []string
	HeaderKeys    []string
	HeaderPairs   map[string]string
	PromptMarkers []string
	// VersionFrom 指定版本号从哪里取："ua" 表示从 UA 里按 VersionRegex 提取，
	// 非空 header 名表示直接读该请求头。
	VersionFrom  string
	VersionRegex *regexp.Regexp
}

var semverRe = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?(?:-[0-9A-Za-z.-]+)?)`)

// clientRules 按优先级从具体到宽泛排列，命中即停。
// 提示词特征取自各工具公开的系统提示词/工具注入片段，只匹配稳定的骨架句，
// 避免因版本改写措辞而整体失效。
var clientRules = []clientRule{
	{
		ID:           "claude_code",
		Name:         "Claude Code",
		Kind:         KindAgentCLI,
		UASubstrings: []string{"claude-cli", "claude-code"},
		HeaderKeys:   []string{"X-App", "Anthropic-Beta"},
		HeaderPairs:  map[string]string{"X-App": "cli"},
		PromptMarkers: []string{
			"you are claude code",
			"anthropic's official cli",
			"claude code, anthropic's",
			"# claude.md",
			"<system-reminder>",
			"str_replace_editor",
			"todowrite",
		},
		VersionFrom:  "ua",
		VersionRegex: semverRe,
	},
	{
		ID:           "codex_cli",
		Name:         "Codex CLI",
		Kind:         KindAgentCLI,
		UASubstrings: []string{"codex_cli_rs", "codex-cli", "codex/"},
		HeaderKeys:   []string{"Originator", "Session_id", "Openai-Beta"},
		HeaderPairs:  map[string]string{"Originator": "codex_cli_rs"},
		PromptMarkers: []string{
			"you are a coding agent running in the codex cli",
			"you are codex, based on gpt-5",
			"codex cli",
			"apply_patch",
			"*** begin patch",
			"shell(command",
			"# sandboxing and approvals",
		},
		VersionFrom:  "ua",
		VersionRegex: semverRe,
	},
	{
		ID:           "opencode",
		Name:         "OpenCode",
		Kind:         KindAgentCLI,
		UASubstrings: []string{"opencode"},
		HeaderKeys:   []string{"X-Opencode-Version", "X-Opencode"},
		PromptMarkers: []string{
			"you are opencode",
			"opencode, an autonomous",
			"agents.md",
			"you are a coding agent built by sst",
			"opencode.ai",
		},
		VersionFrom:  "X-Opencode-Version",
		VersionRegex: semverRe,
	},
	{
		ID:           "zcode",
		Name:         "ZCode",
		Kind:         KindAgentCLI,
		UASubstrings: []string{"zcode", "z-code", "zai-code", "glm-code"},
		HeaderKeys:   []string{"X-Zcode-Version", "X-Z-Client"},
		PromptMarkers: []string{
			"you are zcode",
			"zcode cli",
			"z.ai code",
			"你是 zcode",
		},
		VersionFrom:  "X-Zcode-Version",
		VersionRegex: semverRe,
	},
	{
		ID:           "gemini_cli",
		Name:         "Gemini CLI",
		Kind:         KindAgentCLI,
		UASubstrings: []string{"gemini-cli", "genai-js", "google-genai"},
		HeaderKeys:   []string{"X-Goog-Api-Client"},
		PromptMarkers: []string{
			"you are an interactive cli agent",
			"gemini.md",
			"you are a cli agent specializing in software engineering",
		},
		VersionFrom:  "ua",
		VersionRegex: semverRe,
	},
	{
		ID:           "cline",
		Name:         "Cline",
		Kind:         KindIDE,
		UASubstrings: []string{"cline/", "cline-vscode"},
		PromptMarkers: []string{
			"you are cline",
			"cline, a highly skilled software engineer",
			"replace_in_file",
			"environment_details",
		},
	},
	{
		ID:           "roo_code",
		Name:         "Roo Code",
		Kind:         KindIDE,
		UASubstrings: []string{"roo-code", "roocode", "roo-cline"},
		PromptMarkers: []string{
			"you are roo",
			"roo code",
			"apply_diff",
		},
	},
	{
		ID:           "kilo_code",
		Name:         "Kilo Code",
		Kind:         KindIDE,
		UASubstrings: []string{"kilocode", "kilo-code"},
		PromptMarkers: []string{
			"you are kilo code",
			"kilocode",
		},
	},
	{
		ID:           "cursor",
		Name:         "Cursor",
		Kind:         KindIDE,
		UASubstrings: []string{"cursor"},
		HeaderKeys:   []string{"X-Cursor-Client-Version", "X-Cursor-Checksum"},
		PromptMarkers: []string{
			"you are a powerful agentic ai coding assistant, powered by",
			"you are pair programming with a user in cursor",
			"cursor ide",
			"<edit_file>",
		},
		VersionFrom:  "X-Cursor-Client-Version",
		VersionRegex: semverRe,
	},
	{
		ID:           "windsurf",
		Name:         "Windsurf",
		Kind:         KindIDE,
		UASubstrings: []string{"windsurf", "codeium"},
		PromptMarkers: []string{
			"you are cascade",
			"windsurf, the world's first agentic ide",
			"cascade, a powerful agentic ai coding assistant",
		},
	},
	{
		ID:           "copilot",
		Name:         "GitHub Copilot",
		Kind:         KindIDE,
		UASubstrings: []string{"githubcopilot", "copilot-", "vscode-copilot"},
		HeaderKeys:   []string{"Copilot-Integration-Id", "Editor-Version"},
		PromptMarkers: []string{
			"you are github copilot",
			"you are an ai programming assistant",
			"when asked for your name, you must respond with \"github copilot\"",
		},
		VersionFrom:  "Editor-Version",
		VersionRegex: semverRe,
	},
	{
		ID:           "continue",
		Name:         "Continue",
		Kind:         KindIDE,
		UASubstrings: []string{"continue/", "continuedev"},
		PromptMarkers: []string{
			"you are an expert software developer",
			"continue.dev",
		},
	},
	{
		ID:           "aider",
		Name:         "Aider",
		Kind:         KindAgentCLI,
		UASubstrings: []string{"aider"},
		PromptMarkers: []string{
			"act as an expert software developer",
			"always use best practices when coding",
			"search/replace block",
			"<<<<<<< search",
		},
	},
	{
		ID:           "crush",
		Name:         "Crush",
		Kind:         KindAgentCLI,
		UASubstrings: []string{"crush", "charmbracelet"},
		PromptMarkers: []string{
			"you are crush",
			"crush, an autonomous coding agent",
		},
	},
	{
		ID:   "droid",
		Name: "Factory Droid",
		Kind: KindAgentCLI,
		// 只用 factory-cli / factory.ai 这类专有串。
		// 绝不能收 "droid"：它是 "Android" 的子串，会把所有安卓端
		// 请求（手机浏览器、移动 App、SillyTavern 安卓版）误认成
		// Factory Droid，而 agent_cli 类型会被强制判为写代码。
		UASubstrings: []string{"factory-cli", "factory.ai", "factorydroid"},
		PromptMarkers: []string{
			"you are droid",
			"factory.ai",
			"you are a droid built by factory",
		},
	},
	{
		ID:           "sillytavern",
		Name:         "SillyTavern",
		Kind:         KindChatUI,
		UASubstrings: []string{"sillytavern", "silly-tavern", "tauritavern"},
		PromptMarkers: []string{
			"[start a new chat]",
			"write {{char}}'s next reply",
			"{{char}}",
			"{{user}}",
			"you are {{char}}",
			"[system note:",
			"jailbreak",
			"nsfwprompt",
		},
	},
	{
		// 安卓端第三方聊天客户端，站内实际流量里排名靠前，
		// 之前没有规则导致这些请求全部落在 unknown。
		ID:           "rikkahub",
		Name:         "RikkaHub",
		Kind:         KindChatUI,
		UASubstrings: []string{"rikkahub"},
		VersionFrom:  "ua",
		VersionRegex: semverRe,
	},
	{
		ID:           "tavo",
		Name:         "Tavo",
		Kind:         KindChatUI,
		UASubstrings: []string{"tavo/", "tavoai.dev"},
		VersionFrom:  "ua",
		VersionRegex: semverRe,
	},
	{
		ID:           "kelivo",
		Name:         "Kelivo",
		Kind:         KindChatUI,
		UASubstrings: []string{"kelivo"},
	},
	{
		ID:           "vercel_ai_sdk",
		Name:         "Vercel AI SDK",
		Kind:         KindSDK,
		UASubstrings: []string{"ai-sdk/"},
		VersionFrom:  "ua",
		VersionRegex: semverRe,
	},
	{
		ID:           "cherry_studio",
		Name:         "Cherry Studio",
		Kind:         KindChatUI,
		UASubstrings: []string{"cherrystudio", "cherry-studio"},
		HeaderKeys:   []string{"X-Cherry-Version"},
		PromptMarkers: []string{
			"cherry studio",
		},
		VersionFrom:  "X-Cherry-Version",
		VersionRegex: semverRe,
	},
	{
		ID:           "chatbox",
		Name:         "Chatbox",
		Kind:         KindChatUI,
		UASubstrings: []string{"chatbox"},
	},
	{
		ID:            "lobechat",
		Name:          "LobeChat",
		Kind:          KindChatUI,
		UASubstrings:  []string{"lobechat", "lobe-chat"},
		PromptMarkers: []string{"lobechat"},
	},
	{
		ID:           "openwebui",
		Name:         "Open WebUI",
		Kind:         KindChatUI,
		UASubstrings: []string{"open-webui", "openwebui"},
		PromptMarkers: []string{
			"### task:\ngenerate",
			"### task:\ncreate a concise",
			"chat history:\n<chat_history>",
		},
	},
	{
		ID:           "nextchat",
		Name:         "NextChat",
		Kind:         KindChatUI,
		UASubstrings: []string{"nextchat", "chatgpt-next-web"},
	},
	{
		ID:           "immersive_translate",
		Name:         "Immersive Translate",
		Kind:         KindSDK,
		UASubstrings: []string{"immersivetranslate", "immersive-translate"},
		PromptMarkers: []string{
			"you are a professional, authentic machine translation engine",
			"translate the following source text to",
		},
	},
	{
		ID:           "openai_sdk",
		Name:         "OpenAI SDK",
		Kind:         KindSDK,
		UASubstrings: []string{"openai-python", "openai-node", "openai/"},
		HeaderKeys:   []string{"X-Stainless-Lang", "X-Stainless-Package-Version"},
		VersionFrom:  "X-Stainless-Package-Version",
		VersionRegex: semverRe,
	},
	{
		ID:           "anthropic_sdk",
		Name:         "Anthropic SDK",
		Kind:         KindSDK,
		UASubstrings: []string{"anthropic-python", "anthropic-sdk", "anthropic-ai"},
		HeaderKeys:   []string{"X-Stainless-Lang"},
		VersionFrom:  "X-Stainless-Package-Version",
		VersionRegex: semverRe,
	},
	{
		ID:           "langchain",
		Name:         "LangChain",
		Kind:         KindSDK,
		UASubstrings: []string{"langchain", "langgraph", "llamaindex"},
	},
	{
		ID:           "generic_http",
		Name:         "Generic HTTP Client",
		Kind:         KindSDK,
		UASubstrings: []string{"curl/", "python-requests", "httpx", "axios", "okhttp", "go-http-client", "postman"},
	},
	{
		// 兜底规则，必须放在最后：手机/桌面浏览器与 WebView 套壳应用。
		// 站内相当一部分流量来自这类 UA，识别为浏览器比留空更有信息量，
		// 但因为特征宽泛（很多 App 的 UA 也带 Mozilla/5.0），只能垫底匹配。
		ID:           "browser",
		Name:         "Web Browser",
		Kind:         KindChatUI,
		UASubstrings: []string{"mozilla/5.0", "safari/", "chrome/", "firefox/", "edg/", "dalvik/", "cfnetwork/"},
	},
}

// DetectClient 依据请求头与提示词特征识别调用方工具及版本。
// prompt 已由调用方降为小写并截断，可为空（例如 embedding 请求）。
func DetectClient(header http.Header, prompt string) (id, name, kind, version, source string, score int) {
	ua := strings.ToLower(header.Get("User-Agent"))
	for i := range clientRules {
		rule := &clientRules[i]
		headerHit := ruleMatchesHeader(rule, header, ua)
		promptHit := prompt != "" && containsAny(prompt, rule.PromptMarkers)
		if !headerHit && !promptHit {
			continue
		}
		switch {
		case headerHit && promptHit:
			source, score = "both", 100
		case headerHit:
			source, score = "header", 85
		default:
			// 仅提示词命中：工具指纹被中转站抹掉 UA 时的主要依据，
			// 置信度略低但足以定性。
			source, score = "prompt", 70
		}
		return rule.ID, rule.Name, rule.Kind, extractClientVersion(rule, header, ua, headerHit), source, score
	}
	return "", "", KindUnknown, "", "", 0
}

func ruleMatchesHeader(rule *clientRule, header http.Header, ua string) bool {
	if ua != "" && containsAny(ua, rule.UASubstrings) {
		return true
	}
	for key, want := range rule.HeaderPairs {
		if strings.Contains(strings.ToLower(header.Get(key)), strings.ToLower(want)) {
			return true
		}
	}
	for _, key := range rule.HeaderKeys {
		if header.Get(key) != "" {
			return true
		}
	}
	return false
}

func extractClientVersion(rule *clientRule, header http.Header, ua string, headerHit bool) string {
	if rule.VersionRegex == nil {
		return ""
	}
	if rule.VersionFrom != "" && rule.VersionFrom != "ua" {
		return rule.VersionRegex.FindString(header.Get(rule.VersionFrom))
	}
	// 规则声明从 UA 取版本时，只有 UA 本身确实匹配该工具才可信。
	// 否则请求可能只命中了提示词特征，而 UA 属于中转站的 HTTP 库
	// （例如 okhttp/4.12.0），那里的版本号与工具无关。
	if !headerHit || !containsAny(ua, rule.UASubstrings) {
		return ""
	}
	return rule.VersionRegex.FindString(ua)
}

// clientIsCodingAgent 判断客户端是否属于"编码 agent"这一类。
// 这类客户端的官方系统提示词天然是超长编号规则清单，
// 破甲检测中的 rule_stacking 规则必须对它们豁免，否则 100% 误报。
func clientIsCodingAgent(kind string) bool {
	return kind == KindAgentCLI || kind == KindIDE
}

// findClientRule 按 ID 查找客户端规则，供证据收集复用提示词特征表。
func findClientRule(id string) *clientRule {
	for i := range clientRules {
		if clientRules[i].ID == id {
			return &clientRules[i]
		}
	}
	return nil
}

func containsAny(haystack string, needles []string) bool {
	for _, needle := range needles {
		if needle == "" {
			continue
		}
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}
