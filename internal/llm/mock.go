package llm

import (
	"context"
	"strings"
)

// Mock は Ollama が無い環境でも動作確認できるオフライン実装。
// LLM らしい体裁の決定論的テキストを返す（プロンプト内のヒントを拾って整形）。
type Mock struct{}

func NewMock() *Mock { return &Mock{} }

func (m *Mock) Name() string { return "mock(offline)" }

func (m *Mock) Generate(ctx context.Context, system, user string) (string, error) {
	// NPC のシステムプロンプトは「あなたはTRPGのNPC「…」を演じます」で始まる。
	// GM は「あなたはテキストTRPGのゲームマスター」で始まる。先頭で確実に振り分ける。
	if strings.HasPrefix(system, "あなたはTRPGのNPC") {
		return mockNPC(user), nil
	}
	return mockGM(user), nil
}

func mockGM(user string) string {
	outcome := pick(user, "判定結果", "（判定なし）")
	action := pick(user, "プレイヤーの行動宣言", "あたりを見回す")

	var b strings.Builder
	switch {
	case strings.Contains(outcome, "クリティカル成功"):
		b.WriteString("あなたの行いは見事に決まった。狙い以上の成果が手の中に転がり込む。\n")
	case strings.Contains(outcome, "クリティカル失敗"):
		b.WriteString("ほんの一瞬の油断が裏目に出た。状況は一段険しさを増す。\n")
	case strings.Contains(outcome, "成功"):
		b.WriteString("落ち着いた手つきが功を奏した。事は思惑どおりに運ぶ。\n")
	case strings.Contains(outcome, "失敗"):
		b.WriteString("試みは空振りに終わった。だが、まだ手はある。\n")
	default:
		b.WriteString("あなたの行動に応じて、場の空気がわずかに動いた。\n")
	}
	b.WriteString("（" + strings.TrimSpace(action) + " ……の結果として、GMが情景を描写する箇所）\n\n")
	b.WriteString("あなたに取れそうな行動：\n")
	b.WriteString("- 周囲をさらに詳しく調べる\n")
	b.WriteString("- その場の人物に話しかける\n")
	b.WriteString("- 先へ進む\n")
	b.WriteString("（もちろん、自由に行動を宣言してもよい）")
	return b.String()
}

func mockNPC(user string) string {
	name := pick(user, "[NPC]", "あるNPC")
	return "line: 「……ふん。" + strings.TrimSpace(name) + "に何の用だ。話なら手短にな」\ntone: 様子見・中立"
}

// pick はプロンプト中の "ラベル] 値" 形式から値を1行だけ拾う簡易ヘルパ。
func pick(s, label, def string) string {
	idx := strings.Index(s, label)
	if idx < 0 {
		return def
	}
	rest := s[idx+len(label):]
	// "] " や ": " の後ろを取り、改行まで
	rest = strings.TrimLeft(rest, "]:： 　")
	if nl := strings.IndexAny(rest, "\n\r"); nl >= 0 {
		rest = rest[:nl]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return def
	}
	return rest
}
