// Package engine はセッション管理（司令塔）。
// 設計: docs/01-architecture.md §1.4（基本フロー）, §1.6（判定リクエストの入力決定）
//
// プレイヤー入力 → 行動種別の分類 → 判定エンジン → GM描写（＋NPC統合）→ 出力
// → フラグ/状態更新 → 章進行 という1ターンを統括する。
package engine

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	"trpg-engine-core/internal/dice"
	"trpg-engine-core/internal/gm"
	"trpg-engine-core/internal/npc"
	"trpg-engine-core/internal/scenario"
	"trpg-engine-core/internal/state"
)

type Engine struct {
	Scn      *scenario.Scenario
	Sess     *state.Session
	Resolver *dice.Resolver
	GM       *gm.GM
	NPC      *npc.NPC
}

// TurnResult は 1 ターンの出力（CLI へ渡す）。
type TurnResult struct {
	Check        *dice.CheckResult // 判定が走った場合のみ
	Narration    string
	NPCRaw       []string
	Combat       string // 戦闘の数値ログ（CLI 表示用。GM には渡さない）
	ChapterMoved bool
	NewChapter   *scenario.Chapter
	Ended        bool
	Ending       string
}

func New(scn *scenario.Scenario, sess *state.Session, rng *rand.Rand, g *gm.GM, n *npc.NPC) *Engine {
	return &Engine{
		Scn: scn, Sess: sess,
		Resolver: dice.NewResolver(rng),
		GM:       g, NPC: n,
	}
}

// classify は行動宣言を行動種別に分類する。docs/01-architecture.md §1.6
// （セッション管理の責務。簡易キーワードベース。LLMには成否を委ねない。）
// 戻り値: actionType, usedStat, 判定が必要か
func classify(input string) (string, string, bool) {
	s := strings.ToLower(input)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(input, w) || strings.Contains(s, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("攻撃", "斬", "戦う", "殴", "倒す", "attack"):
		return "attack", "attack", true
	case has("調べ", "探", "罠", "探索", "search", "観察"):
		return "search", "luck", true
	case has("交渉", "説得", "話", "尋ね", "聞", "頼", "talk", "脅"):
		return "talk", "luck", true
	case has("解く", "謎", "仕掛け", "紋章"):
		return "search", "luck", true
	default:
		return "move", "", false // 移動・宣言など判定不要
	}
}

// difficultyFor はシナリオ管理の責務として難易度を決める。docs/01-architecture.md §1.6
// docs/05-scenario.md の章ごとの想定難易度を反映。交渉は手がかり/同行で補正。
func (e *Engine) difficultyFor(chapterID, actionType string) string {
	base := map[string]string{
		"ch01": "normal", "ch02": "normal", "ch03": "hard", "ch04": "hard", "ch05": "normal",
	}[chapterID]
	if base == "" {
		base = "normal"
	}
	// 第4章・交渉ルートの成功補正。docs/05-scenario.md §5.4 第4章
	if chapterID == "ch04" && actionType == "talk" {
		if e.Sess.Flag("clue_found") {
			base = ease(base) // 1段階容易に
		}
	}
	return base
}

func ease(rank string) string {
	switch rank {
	case "very_hard":
		return "hard"
	case "hard":
		return "normal"
	case "normal":
		return "easy"
	}
	return rank
}

// Step は 1 ターンを実行する。
func (e *Engine) Step(ctx context.Context, input string) (*TurnResult, error) {
	ch := e.Scn.Chapter(e.Sess.ChapterID)
	res := &TurnResult{}

	// 1) 行動種別の分類（セッション管理）
	actionType, usedStat, needCheck := classify(input)

	// 2) 判定が必要なら CheckRequest を組み立てて判定エンジンへ（§1.6）
	var check *dice.CheckResult
	if needCheck {
		mod := 0
		if usedStat != "" {
			mod = e.Sess.Player.Modifier(usedStat) // プレイヤー状態管理が提供
		}
		// 第4章・交渉でミレーユ同行なら +2 補正。docs/05-scenario.md
		if ch.ID == "ch04" && actionType == "talk" && e.Sess.Flag("mireille_ally") {
			mod += 2
		}
		req := dice.CheckRequest{
			ActionType:   actionType,
			UsedStat:     usedStat,
			Difficulty:   e.difficultyFor(ch.ID, actionType), // シナリオ管理が決定
			StatModifier: mod,
		}
		r := e.Resolver.Resolve(req)
		check = &r
		res.Check = check
	}

	// 2.5) 第4章・戦闘ルート: attack はボスHPを削る複数回戦闘として解決する。
	var notes []string
	if ch.ID == "ch04" && actionType == "attack" && check != nil {
		note, log := e.resolveCombat(check)
		if note != "" {
			notes = append(notes, note)
		}
		res.Combat = log
	}

	// 3) NPC が必要なら NPC モジュールを呼ぶ（GMが必要と判断＝会話系行動かつ登場NPCあり）
	var npcLines []string
	if actionType == "talk" && len(ch.NPCsPresent) > 0 {
		target := e.pickNPC(ch, input)
		if target != "" {
			tmpl := e.Scn.NPCs[target]
			st := e.Sess.NPC(target)
			line, err := e.NPC.Speak(ctx, tmpl, st.Attitude, e.Sess.SceneSummary, input)
			if err == nil && line != "" {
				npcLines = append(npcLines, tmpl.Name+" → "+oneLine(line))
				res.NPCRaw = append(res.NPCRaw, tmpl.Name+":\n"+line)
				// 態度変化（§3.6）: 交渉成功で1段階上昇、クリティカル失敗で1段階下降
				if check != nil {
					switch check.Outcome {
					case dice.Success, dice.CriticalSuccess:
						st.Attitude = st.Attitude.Up()
					case dice.CriticalFailure:
						st.Attitude = st.Attitude.Down()
					}
				}
			}
		}
	}

	// 4) GM 描写を生成（判定結果・NPC発言・特記を統合）
	cInput := gm.BuildContext(ch, e.Sess, input, check, npcLines, notes)
	narr, err := e.GM.Narrate(ctx, cInput)
	if err != nil {
		return nil, err
	}
	res.Narration = strings.TrimSpace(narr)

	// 5) フラグ更新・章進行（シナリオ管理の制約に従う）
	e.applyProgress(ch, actionType, input, check)
	if e.Sess.Flag(ch.ClearFlag) {
		if next := e.Scn.NextChapter(ch.ID); next != nil {
			e.Sess.ChapterID = next.ID
			e.Sess.SceneSummary = next.SceneSummary
			e.ensureNPCs(next)
			res.ChapterMoved = true
			res.NewChapter = next
		} else {
			res.Ended = true
			res.Ending = e.computeEnding()
		}
	}
	return res, nil
}

// pickNPC は会話相手を決める（名指し優先、なければ章の先頭NPC）。
func (e *Engine) pickNPC(ch *scenario.Chapter, input string) string {
	for _, id := range ch.NPCsPresent {
		if strings.Contains(input, e.Scn.NPCs[id].Name) {
			return id
		}
	}
	if len(ch.NPCsPresent) > 0 {
		return ch.NPCsPresent[0]
	}
	return ""
}

// applyProgress は行動と判定結果からフラグを進める（骨格は固定、結果のみ反映）。
func (e *Engine) applyProgress(ch *scenario.Chapter, actionType, input string, check *dice.CheckResult) {
	ok := check != nil && (check.Outcome == dice.Success || check.Outcome == dice.CriticalSuccess)
	crit := check != nil && check.Outcome == dice.CriticalFailure

	switch ch.ID {
	case "ch01":
		if actionType == "talk" && ok {
			e.Sess.SetFlag("quest_accepted", true)
			e.Sess.SetFlag("location_known", true)
		}
		if strings.Contains(input, "ミレーユ") && strings.Contains(input, "同行") {
			e.Sess.SetFlag("mireille_ally", true)
		}
	case "ch02":
		if strings.Contains(input, "ゴルツ") && actionType == "talk" && ok {
			e.Sess.SetFlag("gorz_ally", true)
		}
		if crit { // 道中の失敗は消耗
			e.Sess.DamageLife(2)
		}
		// 戦う/迂回どちらでも、成功または前進で祠到達
		if ok || actionType == "move" {
			e.Sess.SetFlag("reached_shrine", true)
		}
	case "ch03":
		if actionType == "search" && ok {
			e.Sess.SetFlag("clue_found", true)
			e.Sess.SetFlag("inner_door_opened", true)
		}
		if crit {
			e.Sess.DamageLife(2) // 罠作動
		}
	case "ch04":
		// 交渉ルートは一撃で鎮める。戦闘ルート(attack)は resolveCombat が担当するため
		// ここでは扱わない。
		if actionType == "talk" && ok {
			e.Sess.SetFlag("spirit_soothed", true)
			e.Sess.SetFlag("boss_resolved", true)
		}
	case "ch05":
		// 報告すればエンディング。真実を語ったかをフラグ化。
		if strings.Contains(input, "真実") || strings.Contains(input, "語") || strings.Contains(input, "報告") {
			if !strings.Contains(input, "伏せ") && !strings.Contains(input, "隠") {
				e.Sess.SetFlag("told_truth", true)
			}
			e.Sess.SetFlag("ending_reached", true)
		}
	}
}

// resolveCombat は第4章の戦闘1ラウンドを解決する。HP制で複数ターン継続する。
// 戻り値: note（GMへ渡す物語的特記。数値なし）, log（CLI用の数値ログ）。
func (e *Engine) resolveCombat(check *dice.CheckResult) (note, log string) {
	b := &e.Sess.Boss
	if !b.Active { // 念のための保険（通常は ensureNPCs で初期化済み）
		*b = state.Boss{Name: "歪んだ精霊", HP: 12, MaxHP: 12, Active: true}
	}
	atkMod := e.Sess.Player.Modifier("attack")
	bossBefore := b.HP
	lifeBefore := e.Sess.Player.Stats["life"]

	switch check.Outcome {
	case dice.CriticalSuccess:
		dmg := (3 + atkMod) * 2
		b.HP -= dmg
		note = "精霊の核に渾身の一撃が突き刺さり、青白い光が激しく明滅した。"
	case dice.Success:
		dmg := 3 + atkMod
		if dmg < 1 {
			dmg = 1
		}
		b.HP -= dmg
		note = "精霊に確かな手応えの一撃が入り、嘆きの光が一段弱まった。"
	case dice.Failure:
		e.Sess.DamageLife(2)
		note = "攻撃は空を切り、精霊の冷たい波動が反撃となって体を打った。"
	case dice.CriticalFailure:
		e.Sess.DamageLife(4)
		note = "大きく体勢を崩したところへ、精霊の激しい奔流をまともに浴びてしまった。"
	}
	if b.HP < 0 {
		b.HP = 0
	}

	if b.HP == 0 {
		e.Sess.SetFlag("spirit_defeated", true)
		e.Sess.SetFlag("boss_resolved", true)
		note = "精霊はついに力尽き、崩れるように青白い光が薄れていく。決着のときだ。"
	} else if e.Sess.Player.Stats["life"] == 0 {
		// プレイヤー敗北。撤退扱いにし、戦闘を継続可能な状態に戻す（ボスは未撃破）。
		note = "意識が遠のき、あなたはその場に膝をつく。これ以上は戦えそうにない。"
	}

	log = fmt.Sprintf("⚔ %s HP %d→%d / %s Life %d→%d",
		b.Name, bossBefore, b.HP,
		e.Sess.Player.Name, lifeBefore, e.Sess.Player.Stats["life"])
	return note, log
}

// computeEnding は最終フラグからエンディングを決める。docs/05-scenario.md §5.4 第5章
func (e *Engine) computeEnding() string {
	s := e.Sess
	switch {
	case s.Flag("spirit_soothed") && s.Flag("told_truth"):
		return "【成功（最良）】交渉で精霊を鎮め、村に真実を語った。村は祠を弔いの場として敬い、平穏が戻った。"
	case s.Flag("spirit_soothed") || s.Flag("spirit_defeated"):
		return "【部分成功】異変は止んだが、戦いの傷あと、あるいは伏せた真実が、どこか後味を残した。"
	default:
		return "【失敗】青白い光は消えず、村には不安が残った。だが再び挑む余地はある。"
	}
}

func (e *Engine) ensureNPCs(ch *scenario.Chapter) {
	for _, id := range ch.NPCsPresent {
		if e.Sess.NPC(id) == nil {
			e.Sess.NPCs[id] = &state.NPCState{ID: id, Attitude: e.Scn.NPCs[id].InitAttitude}
		}
	}
	// 第4章に入ったらボス（戦闘ルート用）を初期化する。docs/05-scenario.md §5.4
	if ch.ID == "ch04" && !e.Sess.Boss.Active {
		e.Sess.Boss = state.Boss{Name: "歪んだ精霊", HP: 12, MaxHP: 12, Active: true}
	}
}

// InitNPCs は開始章の NPC を初期化する。
func (e *Engine) InitNPCs() {
	if ch := e.Scn.Chapter(e.Sess.ChapterID); ch != nil {
		e.ensureNPCs(ch)
	}
}

// LoadSession は読み込んだセッションに差し替える（セーブからの再開用）。
func (e *Engine) LoadSession(s *state.Session) {
	e.Sess = s
	e.InitNPCs()
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " / ")
	return strings.TrimSpace(s)
}
