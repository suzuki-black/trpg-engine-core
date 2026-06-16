package engine

import (
	"context"
	"math/rand"
	"strings"
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
	eng := New(scn, sess, rand.New(rand.NewSource(seed)), gm.New(mock, 0), npc.New(mock, 0))
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
	// 撃破でクリアフラグは立つが、即座には進まず「先へ進める」状態。
	if sess.ChapterID != "ch04" {
		t.Errorf("撃破直後の章 = %s, want ch04（『次へ』まで留まる）", sess.ChapterID)
	}
	// 「次へ」で第5章へ。
	res, err := eng.Step(ctx, "次へ進む")
	if err != nil {
		t.Fatal(err)
	}
	if !res.ChapterMoved || sess.ChapterID != "ch05" {
		t.Errorf("『次へ』で進まない: 章=%s moved=%v", sess.ChapterID, res.ChapterMoved)
	}
}

// GMが主人公を描写した行を除去できるか（役割の簒奪防止）。所有格「カイの」も含む。
func TestIsPCAuthored(t *testing.T) {
	pc := "カイ"
	strip := []string{
		"カイの声が僅かに震えた。",
		"カイの視線は壁に留まる。",
		"カイの問いかけに、ハルは睨む。",
		"カイは身構えた。",
		"カイ：「やってやる」",
		"カイは歩み寄り、「待て」と言った。",
	}
	keep := []string{
		"カイ、どうする？",
		"ハル：「カイ、信用するな」",
		"赤い非常灯が点滅している。",
		"隔壁の陰に怯えたクルーがいる。",
	}
	for _, s := range strip {
		if !isPCAuthored(s, pc) {
			t.Errorf("除去すべきPC描写を残した: %q", s)
		}
	}
	for _, s := range keep {
		if isPCAuthored(s, pc) {
			t.Errorf("残すべき行を除去した: %q", s)
		}
	}
}

// 実プレイで分類ミスした台詞を回帰テストする（末尾重み付けで正される）。
func TestClassifyIntent(t *testing.T) {
	cases := []struct {
		input      string
		wantAction string
		wantCheck  bool
	}{
		// 文中に「調べ/探」があっても、文末の意図（話す）を優先する
		{"情報屋カラスに『異変について調べている。何か知らないか』と話しかける", "talk", true},
		{"用心棒を探していると言い、ゴルツに声をかける", "talk", true},
		// 以前は move 扱いで判定が出なかった交渉・攻撃の言い回し
		{"精霊に語りかけて穏やかに鎮めようと試みる", "talk", true},
		{"精霊にとどめの一撃を振り下ろす", "attack", true},
		// 素直なケース
		{"壁の紋章と床の仕掛けを調べる", "search", true},
		{"剣を抜いて精霊を攻撃する", "attack", true},
		{"そのまま森の道を進んで祠へ向かう", "move", false},
		// 誤爆しやすい語: 「面倒」に attack(倒) が反応しない
		{"面倒だが先へ進む", "move", false},
	}
	for _, c := range cases {
		action, _, check := classify(c.input)
		if action != c.wantAction || check != c.wantCheck {
			t.Errorf("classify(%q) = (%s, check=%v), want (%s, %v)",
				c.input, action, check, c.wantAction, c.wantCheck)
		}
	}
}

// 第5章エンディング: 「包み隠さず報告」は真実を語った扱い（"隠"の部分一致で誤判定しない）。
func TestChapter5TruthReporting(t *testing.T) {
	mkCh5 := func(input string) *Engine {
		scn := scenario.ForgottenShrine()
		sess := state.NewSession()
		sess.Player = state.PlayerCharacter{Stats: map[string]int{"luck": 12, "attack": 14, "life": 20}}
		sess.ChapterID = "ch05"
		sess.SceneSummary = scn.Chapter("ch05").SceneSummary
		sess.SetFlag("spirit_soothed", true) // 交渉で鎮めた前提
		mock := llm.NewMock()
		eng := New(scn, sess, rand.New(rand.NewSource(1)), gm.New(mock, 0), npc.New(mock, 0))
		if _, err := eng.Step(context.Background(), input); err != nil {
			t.Fatal(err)
		}
		return eng
	}

	// 「包み隠さず真実を報告」→ told_truth=true（最良エンディング条件）
	eng := mkCh5("村長にすべてを包み隠さず真実のまま報告する")
	if !eng.Sess.Flag("told_truth") {
		t.Error("『包み隠さず報告』が told_truth=false 扱いになっている")
	}
	if got := eng.computeEnding(); !strings.Contains(got, "最良") {
		t.Errorf("最良エンディングにならない: %s", got)
	}

	// 「真実は伏せて報告」→ told_truth=false（部分成功）
	eng2 := mkCh5("村長には都合の悪い真実は伏せて報告する")
	if eng2.Sess.Flag("told_truth") {
		t.Error("『伏せて報告』なのに told_truth=true になっている")
	}
}

// 第2章ゲート: 探索(search)では進まず、前進(move)で祠へ到達する（章スキップ防止）。
func TestChapter2GateRequiresAdvance(t *testing.T) {
	scn := scenario.ForgottenShrine()
	sess := state.NewSession()
	sess.Player = state.PlayerCharacter{Stats: map[string]int{"luck": 12, "attack": 14, "life": 20}}
	sess.ChapterID = "ch02"
	sess.SceneSummary = scn.Chapter("ch02").SceneSummary
	mock := llm.NewMock()
	eng := New(scn, sess, rand.New(rand.NewSource(1)), gm.New(mock, 0), npc.New(mock, 0))
	eng.InitNPCs()
	ctx := context.Background()

	// 探索しても reached_shrine は立たず、章は進まない。
	if _, err := eng.Step(ctx, "あたりの茂みや足跡を調べる"); err != nil {
		t.Fatal(err)
	}
	if sess.Flag("reached_shrine") {
		t.Error("探索で reached_shrine が立ってしまった（章スキップ防止が効いていない）")
	}
	if sess.ChapterID != "ch02" {
		t.Errorf("探索で章が進んだ: %s", sess.ChapterID)
	}

	// 前進すると祠に到達し「目標達成（先へ進める）」になるが、即座には進まない。
	res, err := eng.Step(ctx, "そのまま森の道を進んで祠へ向かう")
	if err != nil {
		t.Fatal(err)
	}
	if !sess.Flag("reached_shrine") {
		t.Error("前進で reached_shrine が立たない")
	}
	if !res.SceneCleared {
		t.Error("目標達成後に SceneCleared になっていない")
	}
	if sess.ChapterID != "ch02" {
		t.Errorf("達成しただけで章が進んだ: %s（『次へ』まで留まるはず）", sess.ChapterID)
	}

	// 「次へ」で次の場面（第3章）へ進む。
	res, err = eng.Step(ctx, "次へ")
	if err != nil {
		t.Fatal(err)
	}
	if !res.ChapterMoved || sess.ChapterID != "ch03" {
		t.Errorf("『次へ』で進まない: 章=%s moved=%v", sess.ChapterID, res.ChapterMoved)
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
