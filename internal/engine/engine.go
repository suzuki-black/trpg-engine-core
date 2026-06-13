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
	// Classifier は行動分類を差し替えるためのフック（LLM補助分類など）。
	// nil の場合はキーワード分類（ClassifyKeyword）を使う。
	Classifier func(ctx context.Context, input string) (action, stat string, needCheck bool)
}

// TurnResult は 1 ターンの出力（CLI へ渡す）。
type TurnResult struct {
	Action       string            // 分類された行動種別（attack/search/talk/move）
	Check        *dice.CheckResult // 判定が走った場合のみ
	Narration    string
	NPCRaw       []string
	Combat       string // 戦闘の数値ログ（CLI 表示用。GM には渡さない）
	ChapterMoved bool
	NewChapter   *scenario.Chapter
	Ended        bool
	Ending       string
	Progressed   bool   // 判定・章進行・フラグ変化など実質的な engagement があったか
	Hint         string // 進展しなかった時のユーザー向けヒント
}

func New(scn *scenario.Scenario, sess *state.Session, rng *rand.Rand, g *gm.GM, n *npc.NPC) *Engine {
	return &Engine{
		Scn: scn, Sess: sess,
		Resolver: dice.NewResolver(rng),
		GM:       g, NPC: n,
	}
}

// 行動種別ごとのキーワード。日本語は主動詞が文末に来やすいため、
// 「文中で最も後ろに現れたキーワードの種別」を採用する（末尾重み付け）。
// これにより「…調べていると話しかける」(=talk) のような誤爆を防ぐ。
var (
	attackKW = []string{
		"攻撃", "斬りかか", "斬りつけ", "斬り", "斬る", "斬", "切りつけ", "切り伏せ",
		"戦う", "殴", "打ち倒", "倒す", "討", "刺し", "突き刺", "振り下ろ", "叩き",
		"一撃", "とどめ", "迎え撃", "attack",
	}
	searchKW = []string{
		"調べ", "調査", "探索", "探る", "探す", "探し", "捜", "罠", "観察",
		"見極", "見渡", "見回", "解く", "謎", "仕掛け", "紋章", "search",
	}
	talkKW = []string{
		"交渉", "説得", "説き", "話しか", "話す", "話", "尋ね", "訊", "聞き", "聞く", "聞",
		"問いかけ", "問い", "頼み込", "頼む", "頼", "語りかけ", "語りか", "呼びかけ",
		"声をかけ", "なだめ", "宥め", "慰め", "鎮め", "挑発", "脅し", "脅", "talk",
	}
)

// classify は行動宣言を行動種別に分類する。docs/01-architecture.md §1.6
// （セッション管理の責務。LLMには成否を委ねない。）
// 末尾重み付け: 最後に現れたキーワードの種別を主たる意図とみなす。
// 戻り値: actionType, usedStat, 判定が必要か
func classify(input string) (string, string, bool) {
	s := strings.ToLower(input)
	best := -1
	action, stat := "move", ""

	scan := func(kws []string, a, st string) {
		for _, kw := range kws {
			if i := strings.LastIndex(s, strings.ToLower(kw)); i > best {
				best, action, stat = i, a, st
			}
		}
	}
	// search を先に、talk/attack を後に走査することで、同位置の競合時は
	// より能動的な意図（攻撃・会話）を優先する。
	scan(searchKW, "search", "luck")
	scan(attackKW, "attack", "attack")
	scan(talkKW, "talk", "luck")

	if best < 0 {
		return "move", "", false // 移動・宣言など判定不要
	}
	return action, stat, true
}

// difficultyFor は章データの難易度に、保持フラグによる緩和ボーナスを適用して返す。
// docs/01-architecture.md §1.6
func (e *Engine) difficultyFor(chapterID, actionType string) string {
	ch := e.Scn.Chapter(chapterID)
	if ch == nil {
		return "normal"
	}
	base := ch.Difficulty
	if base == "" {
		base = "normal"
	}
	for _, b := range ch.Bonuses {
		if b.Ease && b.On == actionType && e.Sess.Flag(b.Flag) {
			base = ease(base) // 1段階容易に
		}
	}
	return base
}

// bonusModifier は章データのフラグ依存ボーナス（判定値修正）を合算する。
func (e *Engine) bonusModifier(ch *scenario.Chapter, actionType string) int {
	mod := 0
	for _, b := range ch.Bonuses {
		if b.Modifier != 0 && b.On == actionType && e.Sess.Flag(b.Flag) {
			mod += b.Modifier
		}
	}
	return mod
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
	trueFlagsBefore := countTrueFlags(e.Sess.Flags)

	// 1) 行動種別の分類（セッション管理）。Classifier 未設定ならキーワード分類。
	var actionType, usedStat string
	var needCheck bool
	if e.Classifier != nil {
		actionType, usedStat, needCheck = e.Classifier(ctx, input)
	} else {
		actionType, usedStat, needCheck = classify(input)
	}
	res.Action = actionType

	// 2) 判定が必要なら CheckRequest を組み立てて判定エンジンへ（§1.6）
	var check *dice.CheckResult
	if needCheck {
		mod := 0
		if usedStat != "" {
			mod = e.Sess.Player.Modifier(usedStat) // プレイヤー状態管理が提供
		}
		// 章データのフラグ依存ボーナス（同行・手がかり等）。docs/05-scenario.md
		mod += e.bonusModifier(ch, actionType)
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

	// 2.5) 戦闘ルート: ボスのいる章で attack はボスHPを削る複数回戦闘として解決する。
	var notes []string
	if ch.Boss != nil && actionType == "attack" && check != nil {
		note, log := e.resolveCombat(ch, check)
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

	// 6) 透明性: 実質的な engagement があったかを判定し、無ければヒントを出す。
	res.Progressed = check != nil || res.ChapterMoved || res.Ended ||
		res.Combat != "" || countTrueFlags(e.Sess.Flags) > trueFlagsBefore
	if !res.Progressed {
		res.Hint = "この行動は判定にも進行にも結びつきませんでした。" +
			"『誰に・何をするか』を具体的に書くと判定が起きます" +
			"（例: 『カラスに祠の場所を尋ねる』『扉を調べる』『敵を攻撃する』）。\n" +
			"いまの目標: " + ch.Goal
	}
	return res, nil
}

func countTrueFlags(flags map[string]bool) int {
	n := 0
	for _, v := range flags {
		if v {
			n++
		}
	}
	return n
}

// ClassifyKeyword はキーワードによる行動分類（LLM非依存）。
// Classifier フックのフォールバックとして外部から利用できる。
func ClassifyKeyword(input string) (action, stat string, needCheck bool) {
	return classify(input)
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

// applyProgress は章データの進行ルールを評価し、合致したルールの効果（フラグ／
// ダメージ）を適用する。戦闘(attack)はボスのいる章では resolveCombat が担当する。
func (e *Engine) applyProgress(ch *scenario.Chapter, actionType, input string, check *dice.CheckResult) {
	for _, r := range ch.Rules {
		// ボス章の attack はルールではなく戦闘で解決するため、ここでは無視する。
		if ch.Boss != nil && r.On == "attack" {
			continue
		}
		if !ruleMatches(r, actionType, input, check) {
			continue
		}
		for _, f := range r.Sets {
			e.Sess.SetFlag(f, true)
		}
		if r.Damage > 0 {
			e.Sess.DamageLife(r.Damage)
		}
	}
}

// ruleMatches は進行ルールが現在の行動・入力・判定結果に合致するか判定する。
func ruleMatches(r scenario.Rule, actionType, input string, check *dice.CheckResult) bool {
	if r.On != "" && r.On != "any" && r.On != actionType {
		return false
	}
	if !outcomeMatches(r.Outcome, check) {
		return false
	}
	for _, s := range r.RequiresAll {
		if !strings.Contains(input, s) {
			return false
		}
	}
	if len(r.RequiresAny) > 0 && !containsAny(input, r.RequiresAny...) {
		return false
	}
	if len(r.ExcludesAny) > 0 && containsAny(input, r.ExcludesAny...) {
		return false
	}
	return true
}

func outcomeMatches(want string, check *dice.CheckResult) bool {
	switch want {
	case "", "any":
		return true
	case "success":
		return check != nil && (check.Outcome == dice.Success || check.Outcome == dice.CriticalSuccess)
	case "critsuccess":
		return check != nil && check.Outcome == dice.CriticalSuccess
	case "fail":
		return check != nil && (check.Outcome == dice.Failure || check.Outcome == dice.CriticalFailure)
	case "critfail":
		return check != nil && check.Outcome == dice.CriticalFailure
	}
	return false
}

// resolveCombat はボス戦の1ラウンドを解決する。HP制で複数ターン継続する。
// ボス定義は章データから来る（シナリオ非依存）。
// 戻り値: note（GMへ渡す物語的特記。数値なし）, log（CLI用の数値ログ）。
func (e *Engine) resolveCombat(ch *scenario.Chapter, check *dice.CheckResult) (note, log string) {
	b := &e.Sess.Boss
	if !b.Active && ch.Boss != nil { // 念のための保険（通常は ensureNPCs で初期化済み）
		*b = state.Boss{Name: ch.Boss.Name, HP: ch.Boss.HP, MaxHP: ch.Boss.HP, Active: true}
	}
	atkMod := e.Sess.Player.Modifier("attack")
	bossBefore := b.HP
	lifeBefore := e.Sess.Player.Stats["life"]

	switch check.Outcome {
	case dice.CriticalSuccess:
		b.HP -= (3 + atkMod) * 2
		note = "渾身の一撃が" + b.Name + "を大きくよろめかせた。"
	case dice.Success:
		dmg := 3 + atkMod
		if dmg < 1 {
			dmg = 1
		}
		b.HP -= dmg
		note = b.Name + "に確かな手応えの一撃が入り、その勢いが一段弱まった。"
	case dice.Failure:
		e.Sess.DamageLife(2)
		note = "攻撃は空を切り、" + b.Name + "の反撃を受けてしまった。"
	case dice.CriticalFailure:
		e.Sess.DamageLife(4)
		note = "大きく体勢を崩したところへ、" + b.Name + "の強烈な反撃をまともに浴びた。"
	}
	if b.HP < 0 {
		b.HP = 0
	}

	if b.HP == 0 {
		if ch.Boss != nil {
			for _, f := range ch.Boss.DefeatSets {
				e.Sess.SetFlag(f, true)
			}
		}
		note = b.Name + "はついに力尽き、崩れ落ちようとしている。決着のときだ。"
	} else if e.Sess.Player.Stats["life"] == 0 {
		// プレイヤー敗北。撤退扱いにし、戦闘を継続可能な状態に戻す（ボスは未撃破）。
		note = "意識が遠のき、あなたはその場に膝をつく。これ以上は戦えそうにない。"
	}

	log = fmt.Sprintf("⚔ %s HP %d→%d / %s Life %d→%d",
		b.Name, bossBefore, b.HP,
		e.Sess.Player.Name, lifeBefore, e.Sess.Player.Stats["life"])
	return note, log
}

// computeEnding は章データのエンディング定義を順に評価し、最初に条件を満たすものを返す。
func (e *Engine) computeEnding() string {
	for _, end := range e.Scn.Endings {
		ok := true
		for _, f := range end.RequiresAll {
			if !e.Sess.Flag(f) {
				ok = false
				break
			}
		}
		if ok && len(end.RequiresAny) > 0 && !flagAny(e.Sess, end.RequiresAny) {
			ok = false
		}
		if ok {
			return end.Text
		}
	}
	return "（物語は静かに幕を閉じた。）"
}

func flagAny(s *state.Session, flags []string) bool {
	for _, f := range flags {
		if s.Flag(f) {
			return true
		}
	}
	return false
}

func (e *Engine) ensureNPCs(ch *scenario.Chapter) {
	for _, id := range ch.NPCsPresent {
		if e.Sess.NPC(id) == nil {
			e.Sess.NPCs[id] = &state.NPCState{ID: id, Attitude: e.Scn.NPCs[id].InitAttitude}
		}
	}
	// ボスのいる章に入ったら戦闘用ボスを初期化する（章データ駆動）。
	if ch.Boss != nil && !e.Sess.Boss.Active {
		e.Sess.Boss = state.Boss{Name: ch.Boss.Name, HP: ch.Boss.HP, MaxHP: ch.Boss.HP, Active: true}
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

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
