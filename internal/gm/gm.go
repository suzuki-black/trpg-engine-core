// Package gm は GM モジュール（GM AI）。
// 設計: docs/02-gm-prompt.md
//
// システム指示（人格・禁止事項）＋コンテキスト入力（章/シーン/行動宣言/判定結果/状態）
// を組み立てて LLM を呼び、状況描写＋行動候補を生成する。
package gm

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"trpg-engine-core/internal/dice"
	"trpg-engine-core/internal/llm"
	"trpg-engine-core/internal/scenario"
	"trpg-engine-core/internal/state"
)

// SystemPrompt は GM運用規範（principles.md）を埋め込んだもの。毎ターン GM に注入する。
// TRPGのGM原則（役割の分離・1ターン1ビート・宣言を繰り返さない 等）を記録した md を
// 単一の出典として、ここから読む。docs/02-gm-prompt.md も参照。
//
//go:embed principles.md
var SystemPrompt string

// GM は LLM クライアントを保持する。
type GM struct {
	llm       llm.Client
	qcRetries int // 非日本語混入時に書き直させる最大回数
}

func New(c llm.Client, qcRetries int) *GM { return &GM{llm: c, qcRetries: qcRetries} }

// BuildContext はコンテキスト入力を構造化テキストにする。docs/02-gm-prompt.md §2.3(2)
// docs/01-architecture.md §1.3(1): 章情報＋シーン＋行動宣言＋判定結果＋関連状態。
func BuildContext(ch *scenario.Chapter, sess *state.Session, action string, res *dice.CheckResult, npcLines []string, notes []string, facts []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[操作キャラ] %s（%s）← この名前で呼びかけること\n", sess.Player.Name, sess.Player.Class)
	fmt.Fprintf(&b, "[章] %s 「%s」\n", ch.ID, ch.Title)
	fmt.Fprintf(&b, "[章の目的] %s\n", ch.Goal)
	fmt.Fprintf(&b, "[クリア条件] %s\n", ch.ClearHint)
	fmt.Fprintf(&b, "[シーン] %s\n", sess.SceneSummary)
	if ch.Layout != "" {
		fmt.Fprintf(&b, "[位置関係] %s\n", ch.Layout)
	}
	for _, en := range ch.Entities {
		pos := ""
		if en.Position != "" {
			pos = "（" + en.Position + "）"
		}
		fmt.Fprintf(&b, "[登場物] %s%s: %s\n", en.Name, pos, en.Desc)
	}
	// 開示可能な事実だけを渡す（隠し事実はそもそも渡さない＝GMが漏らせない）。
	// 聞かれていないのに全部を羅列せず、必要に応じて使うこと。
	for _, f := range facts {
		fmt.Fprintf(&b, "[シーン情報（聞かれたら答えてよい事実。勝手に全部は明かさない）] %s\n", f)
	}
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
		fmt.Fprintf(&b, "[NPC発言（既に画面に表示済み。同じセリフを繰り返さない。"+
			"話者の様子・表情・場の反応だけを短く添えてよい）] %s\n", l)
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
		// Markdown の見出し記号（#, ##, ### …）の漏れを行頭から除く。
		t = strings.TrimLeft(t, "#")
		t = strings.TrimSpace(t)
		// モデルが付けがちな判定結果ラベル（［失敗］［成功］等）を行頭から除く。
		for _, p := range []string{
			"［失敗］", "[失敗]", "［成功］", "[成功]",
			"［クリティカル成功］", "［クリティカル失敗］", "[クリティカル成功]", "[クリティカル失敗]",
		} {
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
