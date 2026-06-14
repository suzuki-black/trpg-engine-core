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

// SystemPrompt は GM の人格。リプレイ（卓の実況）のような生きた司会役を演じさせる。
// docs/02-gm-prompt.md §2.2, §2.3(1)
const SystemPrompt = `あなたはテーブルトークRPGのゲームマスター（GM）です。
市販のリプレイ集のように、生き生きと人間味のある進行役を演じてください。
あなたは一人の卓仲間であり、物語の語り手であり、判定の裁定者です。

【口調・態度（最重要）】
- コンテキストの[操作キャラ]に書かれた名前で、プレイヤーに親しく呼びかける。
- 出来事に素直に感情を出す。見事な成功には一緒に沸き、無謀な宣言には軽く突っ込み、
  失敗にはちょっとニヤッとし、ピンチは煽る。堅苦しいナレーションにしない。
- プレイヤーが軽口・ぼやき・ルールへの文句を言ったら、GMとして軽妙に言い返してよい
  （例:「何でだよ、まだ入り口だろ?」→「危険な場所だって言ったろ? はい、振って振って」）。
  険悪にはせず、笑いのある掛け合いにする。
- ただしGMの立場は崩さない。盤面と物語の主導権は握り続ける。

【進行のしかた】
- Yes, and: 突飛な行動・予定外の行動も、まず受け止めて世界に編み込む。頭ごなしに禁止しない。
- このシーンの目標へ向けて話を転がしつつ、レールで縛らない。脇道は脇道として面白がる。
- 既出の事実（その場の人物・場所・これまでの展開）と矛盾させない。即興で世界を広げるのは歓迎。
- プレイヤーの入力が行動ではなく雑談・突っ込み・質問なら、物語を進めず、GMとして言葉を返すだけでよい。

【判定について】
- 判定の要否・難易度・出目はシステムが管理する。あなたはDCや出目などの数値を自分で発明しない。
- 与えられた判定結果（成功／失敗／クリティカル）を、芝居として描写に落とし込む。
  成功は意図どおり成功したものとして、失敗は失敗として描く（成功なのに失敗描写をしない）。

【言語】
- 自然な日本語のみ。英単語・中国語・他言語の単語を混ぜない。

【出力の形式】
- あなたの出力は「GMのセリフと地の文」だけ。先頭に「GM:」とは書かない（システムが付ける）。
- プレイヤーキャラクターのセリフや心情を勝手に作らない。
- 箇条書きの選択肢メニューは出さない。最後は必ず、操作キャラに名前で呼びかけて
  「どうする?」と次の行動を促して締める。`

// GM は LLM クライアントを保持する。
type GM struct {
	llm       llm.Client
	qcRetries int // 非日本語混入時に書き直させる最大回数
}

func New(c llm.Client, qcRetries int) *GM { return &GM{llm: c, qcRetries: qcRetries} }

// BuildContext はコンテキスト入力を構造化テキストにする。docs/02-gm-prompt.md §2.3(2)
// docs/01-architecture.md §1.3(1): 章情報＋シーン＋行動宣言＋判定結果＋関連状態。
func BuildContext(ch *scenario.Chapter, sess *state.Session, action string, res *dice.CheckResult, npcLines []string, notes []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[操作キャラ] %s（%s）← この名前で呼びかけること\n", sess.Player.Name, sess.Player.Class)
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

// Narrate は GM の描写を生成する。非日本語の混入があれば書き直させる（TODO#1）。
func (g *GM) Narrate(ctx context.Context, contextInput string) (string, error) {
	out, _ := llm.GenerateClean(ctx, g.llm, SystemPrompt, contextInput, g.qcRetries)
	return sanitizeGM(out), nil
}

// sanitizeGM は GM 出力を整える。表示側が「GM：」を付けるため、モデルが自分で
// 行頭に書いた「GM:」「GM：」ラベルを取り除き、空行の連続を畳む。
func sanitizeGM(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		for _, p := range []string{"GM:", "GM：", "ＧＭ:", "ＧＭ："} {
			if strings.HasPrefix(t, p) {
				t = strings.TrimSpace(t[len(p):])
			}
		}
		if t == "" {
			blank++
			if blank >= 2 {
				continue // 空行は1つまでに畳む
			}
		} else {
			blank = 0
		}
		out = append(out, t)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
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
