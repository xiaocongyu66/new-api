package insight

// Result 是单次请求的画像分析结果，写入消费日志的 other.insight 字段。
// 所有字段都是统计/标签，不包含请求原文，避免日志泄露用户隐私内容。
type Result struct {
	// 客户端识别
	Client        string `json:"client,omitempty"`         // 归一化客户端标识，如 claude_code / codex_cli
	ClientName    string `json:"client_name,omitempty"`    // 展示名，如 Claude Code
	ClientVersion string `json:"client_version,omitempty"` // 版本号，如 1.0.58
	ClientKind    string `json:"client_kind,omitempty"`    // agent_cli / ide / chat_ui / sdk / relay / unknown
	ClientSource  string `json:"client_source,omitempty"`  // header / prompt / both
	ClientScore   int    `json:"client_score,omitempty"`   // 置信度 0-100

	// 中转站识别
	IsRelay      bool     `json:"is_relay,omitempty"`      // 请求来自另一个中转网关
	RelayVendor  string   `json:"relay_vendor,omitempty"`  // new-api / one-api / sub2api / uni-api / unknown
	RelayScore   int      `json:"relay_score,omitempty"`   // 置信度 0-100
	RelayReasons []string `json:"relay_reasons,omitempty"` // 命中的判定依据

	// 用途分类
	Category      string `json:"category,omitempty"`       // code / roleplay / qa / translate / embedding / other
	CategoryScore int    `json:"category_score,omitempty"` // 主类别得分 0-100
	CodeScore     int    `json:"code_score,omitempty"`
	RoleplayScore int    `json:"roleplay_score,omitempty"`
	QAScore       int    `json:"qa_score,omitempty"`

	// 代码方向
	Stack      string   `json:"stack,omitempty"` // frontend / backend / fullstack / infra / mobile / data / unknown
	StackFront int      `json:"stack_front,omitempty"`
	StackBack  int      `json:"stack_back,omitempty"`
	Languages  []string `json:"languages,omitempty"` // 命中的语言/框架标签
	// CodeModules 是命中的基础语法模块（function / datatype / control / block …）。
	// 这是"判定为写代码"的直接依据：判定要求多类结构共现，
	// 因此这个列表本身就说明了"凭什么算代码"，供证据面板展示。
	CodeModules []string `json:"code_modules,omitempty"`

	// 角色扮演画像
	AIGender    string `json:"ai_gender,omitempty"`    // female / male / unknown
	UserGender  string `json:"user_gender,omitempty"`  // female / male / unknown
	GuessGender string `json:"guess_gender,omitempty"` // 推断的真人性别 female / male / unknown
	GenderScore int    `json:"gender_score,omitempty"` // 推断置信度 0-100
	// GenderBasis 是 GuessGender 的依据强度：self_report / inverse。
	// 必须与 GuessGender 一起消费——inverse 是群体统计倾向反推，
	// 判别力远低于自述，聚合层按此分开计数，避免弱证据靠重复次数"升级"成强结论。
	GenderBasis   string `json:"gender_basis,omitempty"`
	RoleplayStyle string `json:"roleplay_style,omitempty"` // card / novel / chat / unknown

	// 破甲/越狱检测
	Jailbreak       bool     `json:"jailbreak,omitempty"`
	JailbreakScore  int      `json:"jailbreak_score,omitempty"`  // 0-100
	JailbreakLevel  string   `json:"jailbreak_level,omitempty"`  // none / suspect / likely / confirmed
	JailbreakTags   []string `json:"jailbreak_tags,omitempty"`   // 命中的破甲手法标签
	JailbreakVector string   `json:"jailbreak_vector,omitempty"` // system_prompt / prefill / encoding / multi_turn

	// 请求形态
	Turns      int  `json:"turns,omitempty"`       // 对话轮数
	SystemLen  int  `json:"system_len,omitempty"`  // system 提示词字符数
	PromptLen  int  `json:"prompt_len,omitempty"`  // 全部提示词字符数（截断后）
	HasTools   bool `json:"has_tools,omitempty"`   // 是否带工具定义
	HasPrefill bool `json:"has_prefill,omitempty"` // 是否使用 assistant 预填
	Truncated  bool `json:"truncated,omitempty"`   // 提示词超过扫描上限被截断

	// 以下字段不写入消费日志（json:"-"），仅用于按需生成人工复核样本。
	// 保留提示词引用不额外拷贝内存：字符串在 Go 里是只读切片视图。
	system       string
	conversation string
	rawBody      []byte
}

// SetPromptText 由 Analyze 内部填充，供后续按需抽取命中原句。
func (r *Result) SetPromptText(system, conversation string) {
	if r == nil {
		return
	}
	r.system = system
	r.conversation = conversation
}

// SetRawBody 由中间件填充请求体前缀，仅在开启完整留存时使用。
func (r *Result) SetRawBody(body []byte) {
	if r == nil {
		return
	}
	r.rawBody = body
}

// RawBody 返回请求体前缀。
func (r *Result) RawBody() []byte {
	if r == nil {
		return nil
	}
	return r.rawBody
}

// BuildEvidence 抽取本次请求所有命中关键词及其原文上下文。
// 这是第二遍扫描，只在该请求被判定为需要留证时调用。
func (r *Result) BuildEvidence() []Evidence {
	if r == nil {
		return nil
	}
	return CollectEvidence(r.system, r.conversation, r)
}

// clientKind 常量，前端按此分组展示。
const (
	KindAgentCLI = "agent_cli"
	KindIDE      = "ide"
	KindChatUI   = "chat_ui"
	KindSDK      = "sdk"
	KindRelay    = "relay"
	KindUnknown  = "unknown"
)

// 用途类别常量。
const (
	CategoryCode      = "code"
	CategoryRoleplay  = "roleplay"
	CategoryQA        = "qa"
	CategoryTranslate = "translate"
	CategoryEmbedding = "embedding"
	CategoryOther     = "other"
)

// 技术栈方向常量。
const (
	StackFrontend = "frontend"
	StackBackend  = "backend"
	StackFull     = "fullstack"
	StackInfra    = "infra"
	StackMobile   = "mobile"
	StackData     = "data"
	StackUnknown  = "unknown"
)

// 性别常量。
const (
	GenderFemale  = "female"
	GenderMale    = "male"
	GenderUnknown = "unknown"
)

// 破甲等级常量。
const (
	JailbreakNone      = "none"
	JailbreakSuspect   = "suspect"
	JailbreakLikely    = "likely"
	JailbreakConfirmed = "confirmed"
)

// prompt 为空时 Result 仍会带客户端信息，因此用该方法判断是否值得落库聚合。
func (r *Result) HasProfile() bool {
	if r == nil {
		return false
	}
	return r.Category != "" || r.Client != "" || r.IsRelay || r.Jailbreak
}
