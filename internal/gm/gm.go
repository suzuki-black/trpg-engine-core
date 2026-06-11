// Package gm は GM モジュール（GM AI）。
// 設計: docs/02-gm-prompt.md
//
// システム指示（人格・禁止事項）＋コンテキスト入力（章/シーン/行動宣言/判定結果/状態）
// を組み立てて LLM を呼び、状況描写＋行動候補を生成する。
package gm

import (
	"context"
	"fmt"
	"strings"

	"trpg-engine-core/internal/dice"
	"trpg-engine-core/internal/llm"
	"trpg-engine-core/internal/scenario"
	"trpg-engine-core/internal/state"
)

// SystemPrompt は GM の人格と禁止事項。docs/02-gm-prompt.md §2.2, §2.3(1)
const SystemPrompt = `あなたはテキストTRPGのゲームマスター（GM）です。
人格: 落ち着いて公平。プレイヤーの選択を尊重し、難易度は理不尽でなく緊張感がある程度。
描写は簡潔かつ情景的にし、最後に必ずプレイヤーの行動候補を2〜4個提示してください。

【言語】
- 出力は必ず自然な日本語のみ。英単語・中国語・他言語の単語を混ぜない。
- ローマ字や外国語の固有名詞も、文脈上どうしても必要な場合を除き使わない。

【数値・メカニクスの捏造禁止】
- HP・好感度・確率・DC・ポイントなどの数値やゲーム内部値を、自分で発明・表示しない。
- 「判定:成功/失敗」は与えられた結果としてのみ受け取り、その意味を物語の描写に翻訳する。
  数値で説明せず、情景・感触・反応として描く（例: 「掛け金が外れる手応えがあった」）。
- ステータスやサイコロの出目に言及しない。それらはエンジンが管理する裏側の値である。

【厳守する禁止事項】
- 判定の上書き禁止: 与えられた判定結果を覆さない。自分でサイコロを振らない。
- 未解放情報の禁止: 現在の章で解放されていない真相・伏線を明かさない。
- 未登場NPCの情報禁止: まだ登場していないNPCの存在・事情を漏らさない。
- 章順序・クリア条件の改変禁止。フラグの勝手な変更禁止。
- ボスの弱体化・撃破禁止 / NPCの死亡を判定なしに勝手に決めない。
- プレイヤーキャラクターの行動・発言・心情を勝手に決めない。

【出力形式】
状況描写（2〜5文程度）のあとに、必ず次の形式で行動候補を置く:
あなたに取れそうな行動：
- （候補1）
- （候補2）
- （候補3）`

// GM は LLM クライアントを保持する。
type GM struct {
	llm llm.Client
}

func New(c llm.Client) *GM { return &GM{llm: c} }

// BuildContext はコンテキスト入力を構造化テキストにする。docs/02-gm-prompt.md §2.3(2)
// docs/01-architecture.md §1.3(1): 章情報＋シーン＋行動宣言＋判定結果＋関連状態。
func BuildContext(ch *scenario.Chapter, sess *state.Session, action string, res *dice.CheckResult, npcLines []string, notes []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[章] %s 「%s」\n", ch.ID, ch.Title)
	fmt.Fprintf(&b, "[章の目的] %s\n", ch.Goal)
	fmt.Fprintf(&b, "[クリア条件] %s\n", ch.ClearHint)
	fmt.Fprintf(&b, "[シーン] %s\n", sess.SceneSummary)
	fmt.Fprintf(&b, "[世界状態] 時間帯:%s 天候:%s 警戒度:%s 環境:%s\n",
		dflt(sess.World.TimeOfDay, "夕"), dflt(sess.World.Weather, "曇り"),
		dflt(sess.World.Alertness, "低"), dflt(sess.World.Ambient, "静か"))
	if flags := activeFlags(sess); flags != "" {
		fmt.Fprintf(&b, "[有効フラグ] %s\n", flags)
	}
	fmt.Fprintf(&b, "[プレイヤーの行動宣言] %s\n", action)
	if res != nil {
		fmt.Fprintf(&b, "[判定結果] %s 判定：%s\n", res.ActionType, res.Outcome.JP())
	} else {
		fmt.Fprintf(&b, "[判定結果] （判定なし）\n")
	}
	for _, l := range npcLines {
		fmt.Fprintf(&b, "[NPC発言（このセリフを地の文に統合せよ）] %s\n", l)
	}
	for _, n := range notes {
		fmt.Fprintf(&b, "[特記（この状況を描写に反映せよ。数値には触れない）] %s\n", n)
	}
	return b.String()
}

// Narrate は GM の描写を生成する。
func (g *GM) Narrate(ctx context.Context, contextInput string) (string, error) {
	return g.llm.Generate(ctx, SystemPrompt, contextInput)
}

func dflt(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func activeFlags(sess *state.Session) string {
	var on []string
	for k, v := range sess.Flags {
		if v {
			on = append(on, k)
		}
	}
	return strings.Join(on, ", ")
}
