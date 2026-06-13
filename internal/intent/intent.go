// Package intent はプレイヤーの行動宣言を行動種別へ分類する。
// 設計: docs/01-architecture.md §1.6（セッション管理の責務）
//
// キーワード方式（漢字前提）は、ひらがな入力や口語・言い換えに弱い。
// そこで「キーワードで明確に判定できない時だけ LLM に意図を尋ねる」
// ハイブリッド方式を提供する。漢字の明確な行動は即時・決定論的に、
// それ以外（かな書き・曖昧）は LLM が補う。
package intent

import (
	"context"
	"strings"

	"trpg-engine-core/internal/llm"
)

// Func は分類関数。戻り値: actionType, usedStat, 判定が必要か。
type Func func(ctx context.Context, input string) (string, string, bool)

// KeywordFunc はキーワード分類（LLM非依存）の関数型。
type KeywordFunc func(input string) (string, string, bool)

const classifySystem = `あなたはテキストTRPGの行動分類器です。
プレイヤーの行動文を、次の4種類のいずれか「1語」だけで答えてください。説明・記号・改行は不要です。
- attack : 攻撃・戦闘・斬る・倒すなど、敵に物理的に仕掛ける行動
- search : 調べる・探す・探索・観察・罠の解除・鍵開け・謎解きなど
- talk   : 会話・挨拶・質問・交渉・説得・脅し・呼びかけなど、相手に言葉で働きかける行動
- move   : 移動・前進・その他、上のどれにも当てはまらず判定が不要な行動
必ず attack / search / talk / move のいずれか1語だけを出力してください。`

// NewLLM はハイブリッド分類器を返す。
// まず keyword で判定し、明確な行動が取れればそれを採用（高速・決定論）。
// keyword が move（=該当キーワードなし）の場合のみ LLM に意図を尋ねる。
// LLM が解釈不能なら move（判定なし）にフォールバックする。
func NewLLM(c llm.Client, keyword KeywordFunc) Func {
	return func(ctx context.Context, input string) (string, string, bool) {
		if action, stat, need := keyword(input); need {
			return action, stat, need
		}
		label := ask(ctx, c, input)
		switch label {
		case "attack":
			return "attack", "attack", true
		case "search":
			return "search", "luck", true
		case "talk":
			return "talk", "luck", true
		default: // move / 解釈不能
			return "move", "", false
		}
	}
}

// ask は LLM に分類を尋ね、応答から最初に現れたラベルを返す。
func ask(ctx context.Context, c llm.Client, input string) string {
	out, err := c.Generate(ctx, classifySystem, "プレイヤーの行動: "+input)
	if err != nil {
		return ""
	}
	out = strings.ToLower(out)
	best, label := len(out), ""
	for _, l := range []string{"attack", "search", "talk", "move"} {
		if i := strings.Index(out, l); i >= 0 && i < best {
			best, label = i, l
		}
	}
	return label
}
