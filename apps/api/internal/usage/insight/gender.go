package insight

import "strings"

// 性别相关的判定分成两件互不相同的事，本文件严格区分：
//
//  1. AI 角色性别（aiGender）：角色卡把 AI 设定成了什么性别。对内容的客观描述。
//  2. 真人用户性别（guess）：使用这个 key 的人是什么性别。这是概率推断。
//
// 第 2 项有三级证据，强弱递减，必须分开记账：
//
//   - 自述（self_report）：用户明确写了"我是女生" / "{{user}} is a man"。
//     直接采用，置信度 75~85。
//   - 内容偏好（preference）：题材本身有强性别相关性。BL（男男）向内容的
//     受众社区经验上以女性为主，故命中判女性（score 70）；GL（女女）向内容
//     的相关性弱一些，按 score 55 计。这比"只看 AI 角色性别取反"可靠，
//     因为它抓的是用户主动选择的题材，而不是单个角色的性别标签。
//   - 异性配对先验（inverse）：只知道 AI 角色性别，按"AI 设定为女性的用户
//     多为男性"这一群体倾向反推。置信度 40，对单个用户几乎没有判别力。
//
// 早期版本的错误不在于做了推断，而在于把弱证据的分数在聚合层丢弃：
// GuessedGender() 改用"多次结论一致率"当置信度，于是同一个 40 分先验
// 重复 10 次就在 UI 上显示成 confidence 90%。线上后果是 male_guess=118
// /ai_female=118、female_guess=893/ai_male=897 两组数字完全镜像——
// 一个纯粹由 AI 角色性别取反得到的列，被当成高置信度画像摆在封禁按钮旁边。
//
// 现在每一级证据的强度都随计数一路传到看板：聚合层对三级分别计数，
// 各自有置信度上限并在 UI 上标注依据。重复一万次弱证据仍然是弱证据。

// genderMarkers 收集角色卡里描述性别的常见写法。
type genderMarkers struct {
	Female []string
	Male   []string
}

// aiRoleMarkers 匹配"AI 扮演的角色"的性别描述。
// 角色卡通常把 AI 角色写在 {{char}} / persona / 描述段里。
var aiRoleMarkers = genderMarkers{
	Female: []string{
		"{{char}} is a girl", "{{char}} is a woman", "{{char}} is female",
		"char: female", "gender: female", "sex: female",
		"她是", "少女", "女孩", "女生", "女性", "妹妹", "姐姐", "女友", "女朋友",
		"母亲", "妈妈", "女仆", "女王", "公主", "老婆", "妻子", "女儿",
		"she is", "her name is", "girlfriend", "waifu", "onee-san", "imouto",
		"maid", "queen", "princess", "wife", "daughter", "mommy",
	},
	Male: []string{
		"{{char}} is a boy", "{{char}} is a man", "{{char}} is male",
		"char: male", "gender: male", "sex: male",
		"他是", "少年", "男孩", "男生", "男性", "哥哥", "弟弟", "男友", "男朋友",
		"父亲", "爸爸", "国王", "王子", "老公", "丈夫", "儿子", "总裁",
		"he is", "his name is", "boyfriend", "husbando", "onii-chan", "otouto",
		"king", "prince", "husband", "son", "daddy",
	},
}

// userRoleMarkers 匹配"用户自我代入的角色"的性别描述。
// SillyTavern 系一般写在 {{user}} / user's persona 段落。
//
// 这是真人性别的唯一依据，因此只收明确的第一人称自述，
// 不收"她是"/"女友"这类描述 AI 角色的写法。
var userRoleMarkers = genderMarkers{
	Female: []string{
		"{{user}} is a girl", "{{user}} is a woman", "{{user}} is female",
		"user: female", "user's persona: female", "i am a girl", "i'm a girl",
		"i am a woman", "i'm a woman",
		"我是女生", "我是女孩", "我是女性", "我是女的",
		"我是妹妹", "我是姐姐", "我是女儿", "我是你的老婆", "我是你的妻子",
		"我是你的女朋友", "我是你女朋友",
		"称呼我为小姐", "叫我姐姐", "叫我妹妹",
	},
	Male: []string{
		"{{user}} is a boy", "{{user}} is a man", "{{user}} is male",
		"user: male", "user's persona: male", "i am a boy", "i'm a man",
		"i am a man", "i'm a boy",
		"我是男生", "我是男孩", "我是男性", "我是男的",
		"我是哥哥", "我是弟弟", "我是儿子", "我是你的老公", "我是你的丈夫",
		"我是你的男朋友", "我是你男朋友",
		"称呼我为先生", "叫我哥哥", "叫我主人",
	},
}

// roleplayStyleMarkers 区分角色卡式、小说续写式和普通聊天式。
var roleplayStyleMarkers = map[string][]string{
	"card": {
		"{{char}}", "{{user}}", "personality:", "scenario:",
		"example dialogue", "char's persona", "角色卡", "人设",
	},
	"novel": {
		"续写", "小说", "章节", "第一人称叙述", "narrative", "novel",
		"write in third person", "prose", "文风",
	},
}

// 真人性别结论的依据强度。聚合层按此分开计数，
// 使不同强度的证据不会被混成同一个置信度。
const (
	// GenderBasisSelfReport 用户明确自述性别。
	GenderBasisSelfReport = "self_report"
	// GenderBasisPreference 由内容题材偏好推断（BL/GL 向内容的受众性别倾向）。
	GenderBasisPreference = "preference"
	// GenderBasisInverse 仅由 AI 角色性别按异性配对倾向反推。
	GenderBasisInverse = "inverse"
)

// blMarkers 是男男（BL / 耽美 / gay）向内容的题材标记。
// 社区经验上这类内容的受众以女性为主，因此命中时把真人性别推为女性。
// 只收明确指向"两名男性之间恋爱/情欲关系"的题材词，
// 不收单个男性角色标签——那属于 aiRoleMarkers 的范畴。
var blMarkers = []string{
	"耽美", "bl向", "bl文", "bl小说", "bl漫", "男男", "双男主", "攻受",
	"总攻", "总受", "受向", "攻向", "年下攻", "年上攻", "gay romance",
	"male x male", "malexmale", "m/m romance", "boys love", "boyslove",
	"yaoi", "shounen ai", "shounen-ai", "danmei",
}

// glMarkers 是女女（GL / 百合 / yuri）向内容的题材标记。
// 这类内容的受众性别相关性弱于 BL，按较低权重推断为女性。
var glMarkers = []string{
	"百合", "gl向", "gl文", "gl小说", "双女主", "女女",
	"girls love", "girlslove", "yuri", "female x female", "femalexfemale",
	"f/f romance", "shoujo ai", "shoujo-ai",
}

// 内容偏好推断的置信度。preference 强于 inverse、弱于自述。
const (
	// blPreferenceScore 是命中 BL 题材时判"女性"的置信度。
	blPreferenceScore = 70
	// glPreferenceScore 是命中 GL 题材时判"女性"的置信度，按需求定为 55。
	glPreferenceScore = 55
)

// inferGender 返回 AI 角色性别、用户角色性别、真人性别推断及其依据。
//
// basis 是本次结论的依据强度，调用方必须把它一路带到聚合层——
// 这是弱证据不被误当成强证据的唯一保证。basis 为空表示无结论。
//
// 优先级：自述 > 内容偏好（BL/GL）> 异性配对反推。
// 自述是用户直接声明，最可靠；内容偏好抓的是用户主动选的题材，
// 比单看某个角色性别取反更靠谱；反推是最弱的群体先验。
func inferGender(text string) (aiGender, userGender, guess string, score int, basis string) {
	aiGender = matchGender(text, aiRoleMarkers)
	userGender = matchGender(text, userRoleMarkers)

	// —— 第一优先级：自述 ——
	switch {
	case aiGender == GenderFemale && userGender == GenderMale:
		// 双向明确：AI 女 + 用户男。自述与角色配置一致，最强证据。
		return aiGender, userGender, GenderMale, 85, GenderBasisSelfReport
	case aiGender == GenderMale && userGender == GenderFemale:
		return aiGender, userGender, GenderFemale, 85, GenderBasisSelfReport
	case userGender != GenderUnknown:
		// 用户自述性别，与 AI 角色同性或 AI 性别未知。自述优先。
		return aiGender, userGender, userGender, 75, GenderBasisSelfReport
	}

	// —— 第二优先级：内容题材偏好 ——
	// 男男向内容受众以女性为主（强相关），女女向次之。二者都指向女性，
	// 命中即判女性；同时命中时取更高的 BL 权重。
	hasBL := containsAny(text, blMarkers)
	hasGL := containsAny(text, glMarkers)
	switch {
	case hasBL:
		return aiGender, userGender, GenderFemale, blPreferenceScore, GenderBasisPreference
	case hasGL:
		return aiGender, userGender, GenderFemale, glPreferenceScore, GenderBasisPreference
	}

	// —— 第三优先级：异性配对反推 ——
	switch {
	case aiGender == GenderFemale:
		// 仅知 AI 为女性角色：按社区经验的异性配对倾向反推男性用户。
		// 40 分是群体统计倾向，对单个用户判别力很弱，靠 basis 标记传下去。
		return aiGender, userGender, GenderMale, 40, GenderBasisInverse
	case aiGender == GenderMale:
		return aiGender, userGender, GenderFemale, 40, GenderBasisInverse
	default:
		return GenderUnknown, GenderUnknown, GenderUnknown, 0, ""
	}
}

func matchGender(text string, markers genderMarkers) string {
	female, _ := countKeywords(text, markers.Female)
	male, _ := countKeywords(text, markers.Male)
	switch {
	case female == 0 && male == 0:
		return GenderUnknown
	case female > male:
		return GenderFemale
	case male > female:
		return GenderMale
	default:
		// 命中数相同，无法区分。
		return GenderUnknown
	}
}

func inferRoleplayStyle(text string, turns int) string {
	if containsAny(text, roleplayStyleMarkers["card"]) {
		return "card"
	}
	if containsAny(text, roleplayStyleMarkers["novel"]) {
		return "novel"
	}
	if turns >= 4 {
		return "chat"
	}
	return ""
}

// systemPromptLooksLikeCharacter 判断长 system 提示词是否为角色设定，
// 用于给角色扮演打分加权：角色卡的 system 段通常又长又含人物属性列表。
func systemPromptLooksLikeCharacter(system string) bool {
	if len(system) < 300 {
		return false
	}
	attributeHits := 0
	for _, marker := range []string{
		"name:", "age:", "gender:", "appearance:", "personality:",
		"likes:", "dislikes:", "background:", "speech:", "relationship:",
		"姓名", "年龄", "性别", "外貌", "性格", "喜好", "厌恶", "背景", "语气",
	} {
		if strings.Contains(system, marker) {
			attributeHits++
		}
	}
	return attributeHits >= 3
}
