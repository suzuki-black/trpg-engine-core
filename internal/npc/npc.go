// Package npc は NPC モジュール（NPC AI）。
// 設計: docs/03-npc-templates.md, docs/01-architecture.md §1.3(4)
//
// 人格テンプレートをシステムプロンプトに展開し、状況＋プレイヤー発言から
// 「セリフ＋トーン」のみを返す。物語進行・フラグ更新は行わない。
package npc

import (
	"context"
	"fmt"
	"strings"

	"trpg-engine-core/internal/llm"
	"trpg-engine-core/internal/scenario"
	"trpg-engine-core/internal/state"
)

type NPC struct {
	llm llm.Client
}

func New(c llm.Client) *NPC { return &NPC{llm: c} }

// systemPrompt は人格テンプレートを展開する。hidden_goal は「自分からは明かすな」と注記。
func systemPrompt(t scenario.NPCTemplate, att state.Attitude) string {
	return fmt.Sprintf(`あなたはTRPGのNPC「%s」を演じます。これはフィクションのキャラクター設定です。
役割: %s
性格: %s
口調: %s
表向きの目的: %s
隠された目的（自分からは決して明かさない。判定成功や信頼の進展でのみ断片的に漏れる）: %s
現在のプレイヤーへの態度: %s

ルール:
- 性格と口調を一貫させる。態度は急変させず段階的に。
- 自分が知り得ない情報（他NPCの秘密・未到達の章）は知らないものとして扱う。
- 進行制御（フラグ更新・場面描写）はしない。あなたは発言だけを返す。
- 出力は必ず次の2行のみ:
line: 「（セリフ）」
tone: （感情・態度。例 冷静/苛立ち/友好的/警戒）`,
		t.Name, t.Role, t.Personality, t.Tone, t.PublicGoal, t.HiddenGoal, att.JP())
}

// Speak はセリフ＋トーンを生成する。docs/01-architecture.md §1.3(4): 返却はセリフ＋トーンのみ。
func (n *NPC) Speak(ctx context.Context, t scenario.NPCTemplate, att state.Attitude, sceneSummary, playerSays string) (string, error) {
	sys := systemPrompt(t, att)
	user := fmt.Sprintf("[NPC] %s\n[状況] %s\n[プレイヤーの発言・行動] %s\n上記に対し、line と tone の2行で応答してください。",
		t.Name, sceneSummary, playerSays)
	out, err := n.llm.Generate(ctx, sys, user)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
