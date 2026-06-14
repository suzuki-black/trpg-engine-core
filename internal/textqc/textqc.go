// Package textqc は LLM 出力の言語品質チェック（日本語以外の混入検出）を担う。
// 小型ローカルモデルが時々混ぜる中国語（簡体字）・他言語スクリプト・ラテン語片を検出する。
//
// 注意: 日本語の漢字（新字体・常用漢字）と中国語の繁体字は字が重なるため、
// 文字単位で完全に区別はできない。ここでは「日本語では使われない簡体字」と
// 「日本語に現れないスクリプト」「まとまったラテン文字の語」を高信頼で拾う方針。
package textqc

import (
	"sort"
	"strings"
)

// simplifiedOnly は「日本語では使われない簡体字」の集合。
// いずれも日本語の新字体（例: 紅/溝/見/関）とは別字なので誤検出しにくい。
// 部首クラスタ単位で「日本語では使われない簡体字」を集める。日本語は対応する
// 旧字体/正字（紅 溝 聞 紋 …）を使うため、ここに挙げた簡体字グリフは日本語に出現しない。
// 完全網羅ではない（best-effort）が、よく漏れる字を系統的にカバーする。
var simplifiedOnly = func() map[rune]bool {
	const (
		misc  = "这么们个为说红沟见关问间难龙实现发边过进还应经给动严两卖买车东马鸟长门阳际习乐机气话语认识对开风飞鱼时图书纸园样别觉"
		speak = "让议论设访评词译试该详误请读课谁调谈谋谍谎谓谜谢谣谦谨" // 讠（言偏）
		metal = "钱钟铁银铜错锁锋镜锐键锦"                // 钅（金偏）
		food  = "饭饮饿馆饱"                       // 饣（食偏）
		door  = "闭闯闷闲闻阅阔阐"                    // 门（門構え）
		cart  = "转轮软较载输轨轩"                    // 车（車偏）
		horse = "驰驱骑验惊骂骏"                     // 马（馬偏）
		shell = "财货购贵费资赏赚贫贪贾赂"                // 贝（貝偏）
		silk  = "约级纯线练组细织终绍结续绿编绕绳绪缚"          // 纟（糸偏）
		misc2 = "雾务办节优杀类头测渐滨毕显爱丰场处备"          // その他よく漏れる簡体字
		zhPtc = "呢吗吧啦咯嘛呗咧"                    // 中国語の語気助詞（日本語では使わない）
	)
	m := map[rune]bool{}
	for _, set := range []string{misc, speak, metal, food, door, cart, horse, shell, silk, misc2, zhPtc} {
		for _, r := range set {
			m[r] = true
		}
	}
	return m
}()

// foreignScripts は日本語に現れないスクリプトの範囲（名前付き）。
type scriptRange struct {
	name   string
	lo, hi rune
}

var foreignScripts = []scriptRange{
	{"ハングル", 0xAC00, 0xD7A3},
	{"ハングル字母", 0x1100, 0x11FF},
	{"ハングル字母拡張", 0x3130, 0x318F},
	{"キリル文字", 0x0400, 0x04FF},
	{"ギリシャ文字", 0x0370, 0x03FF},
	{"アラビア文字", 0x0600, 0x06FF},
	{"ヘブライ文字", 0x0590, 0x05FF},
	{"タイ文字", 0x0E00, 0x0E7F},
	{"デーヴァナーガリー", 0x0900, 0x097F},
}

// latinWordThreshold: この長さ以上の連続ラテン文字を「外国語の単語」とみなす。
// "GM" や "DC"（2文字）は許容、"Tone"/"Favor"/"NPC" などは検出される。
const latinWordThreshold = 3

// Issues は s に含まれる非日本語要素を列挙する（空なら日本語として問題なし）。
// ignore で指定した部分文字列は検査前に取り除く（NPCの "line:"/"tone:" ラベル等）。
func Issues(s string, ignore ...string) []string {
	for _, tok := range ignore {
		s = strings.ReplaceAll(s, tok, "")
	}

	scriptHits := map[string]bool{}
	simplifiedHits := map[rune]bool{}
	latinRun, maxLatinRun := 0, 0

	for _, r := range s {
		// スクリプト判定
		for _, sr := range foreignScripts {
			if r >= sr.lo && r <= sr.hi {
				scriptHits[sr.name] = true
			}
		}
		// ラテン文字の連続長
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			latinRun++
			if latinRun > maxLatinRun {
				maxLatinRun = latinRun
			}
		} else {
			latinRun = 0
		}
		// 簡体字
		if simplifiedOnly[r] {
			simplifiedHits[r] = true
		}
	}

	var issues []string
	for name := range scriptHits {
		issues = append(issues, "非日本語スクリプト("+name+")")
	}
	if len(simplifiedHits) > 0 {
		issues = append(issues, "簡体字("+runesToString(simplifiedHits)+")")
	}
	if maxLatinRun >= latinWordThreshold {
		issues = append(issues, "ラテン文字語の混入")
	}
	sort.Strings(issues) // 出力順を安定させる
	return issues
}

// IsClean は非日本語要素が無ければ true。
func IsClean(s string, ignore ...string) bool {
	return len(Issues(s, ignore...)) == 0
}

func runesToString(m map[rune]bool) string {
	rs := make([]rune, 0, len(m))
	for r := range m {
		rs = append(rs, r)
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i] < rs[j] })
	return string(rs)
}
