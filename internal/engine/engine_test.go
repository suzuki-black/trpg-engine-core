package engine

import (
	"context"
	"math/rand"
	"testing"

	"trpg-engine-core/internal/gm"
	"trpg-engine-core/internal/llm"
	"trpg-engine-core/internal/npc"
	"trpg-engine-core/internal/scenario"
	"trpg-engine-core/internal/state"
)

// newCombatEngine は第4章（ボス戦）から始まるエンジンを組む。
func newCombatEngine(t *testing.T, seed int64) (*Engine, *state.Session) {
	t.Helper()
	scn := scenario.ForgottenShrine()
	sess := state.NewSession()
	sess.Player = state.PlayerCharacter{
		Name: "アルド", Class: "戦士",
		Stats: map[string]int{"luck": 12, "attack": 18, "life": 50}, // 確実にボスを倒し切れる設定
	}
	sess.ChapterID = "ch04"
	sess.SceneSummary = scn.Chapter("ch04").SceneSummary
	mock := llm.NewMock()
	eng := New(scn, sess, rand.New(rand.NewSource(seed)), gm.New(mock), npc.New(mock))
	eng.InitNPCs() // ここでボスが初期化される
	return eng, sess
}

// 戦闘ルート: attack を繰り返すとボスHPが減り、最終的に撃破して章が進む。
func TestCombatRouteDefeatsBoss(t *testing.T) {
	eng, sess := newCombatEngine(t, 1)

	if !sess.Boss.Active || sess.Boss.HP != 12 {
		t.Fatalf("ボス初期化失敗: active=%v hp=%d", sess.Boss.Active, sess.Boss.HP)
	}

	ctx := context.Background()
	var defeated bool
	for i := 0; i < 50; i++ {
		res, err := eng.Step(ctx, "歪んだ精霊を攻撃する")
		if err != nil {
			t.Fatalf("Step エラー: %v", err)
		}
		if res.Check == nil || res.Check.ActionType != "attack" {
			t.Fatalf("attack として分類されていない: %+v", res.Check)
		}
		if res.Combat == "" {
			t.Fatalf("戦闘ログが空")
		}
		if sess.Flag("boss_resolved") {
			defeated = true
			break
		}
		if sess.Player.Stats["life"] <= 0 {
			t.Fatalf("ボス撃破前にプレイヤーが倒れた")
		}
	}
	if !defeated {
		t.Fatal("50ターン以内にボスを撃破できなかった")
	}
	if !sess.Flag("spirit_defeated") {
		t.Error("spirit_defeated フラグが立っていない")
	}
	if sess.Boss.HP != 0 {
		t.Errorf("ボスHP = %d, want 0", sess.Boss.HP)
	}
	// 撃破でクリアフラグが立ち、章が ch05 へ進んでいる。
	if sess.ChapterID != "ch05" {
		t.Errorf("撃破後の章 = %s, want ch05", sess.ChapterID)
	}
}

// 交渉ルート: clue_found があると第4章 talk の難易度が1段階下がる。
func TestNegotiationRouteEased(t *testing.T) {
	eng, sess := newCombatEngine(t, 1)
	sess.Flag("clue_found") // 参照のみ
	sess.SetFlag("clue_found", true)

	// 第4章 talk: base hard(15) → clue_found で normal(12) に緩和される。
	if dc := eng.difficultyFor("ch04", "talk"); dc != "normal" {
		t.Errorf("clue_found 時の talk 難易度 = %q, want normal", dc)
	}
	// clue_found 無しなら hard のまま。
	sess.SetFlag("clue_found", false)
	if dc := eng.difficultyFor("ch04", "talk"); dc != "hard" {
		t.Errorf("clue_found 無し時の talk 難易度 = %q, want hard", dc)
	}
}
