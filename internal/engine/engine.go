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
		// 第4章の同行ボーナス。docs/05-scenario.md
		// 交渉はミレーユ同行で +2、戦闘はゴルツ加勢で +2。
		if ch.ID == "ch04" && actionType == "talk" && e.Sess.Flag("mireille_ally") {
			mod += 2
		}
		if ch.ID == "ch04" && actionType == "attack" && e.Sess.Flag("gorz_ally") {
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
		// 祠への到達は「前進する(move)」か「戦って突破(attack成功)」で確定する。
		// 探索(search)や会話だけでは勝手に進まない（章スキップ防止）。
		if actionType == "move" || (actionType == "attack" && ok) {
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
		// 報告すればエンディング。真実を語ったか／伏せたかをフラグ化する。
		reported := containsAny(input, "真実", "語", "報告", "伝え", "話")
		if reported {
			// 「隠す/伏せる」系は伏匿。ただし「隠さず/包み隠さず/隠さない」は否定＝真実を話す。
			hiding := containsAny(input, "伏せ", "隠し", "隠す", "黙", "嘘", "偽")
			if strings.Contains(input, "隠さ") { // 隠さず・隠さない 等
				hiding = false
			}
			if !hiding {
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

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
