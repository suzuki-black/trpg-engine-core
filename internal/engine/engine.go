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
	SceneCleared bool   // この章の目標は達成済み＝「次へ」で次場面へ進める
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
		"調べ", "調査", "探索", "探る", "探す", "探し", "捜", "罠",
		"見極", "解く", "謎", "仕掛け", "紋章", "search",
	}
	// ask は「見れば分かる情報の確認」＝判定なし。search（隠れた物を探す＝判定）と区別する。
	askKW = []string{
		"見回", "見渡", "眺め", "観察", "様子", "周囲を", "あたりを見", "周りを見",
		"何がある", "何かある", "誰がいる", "見える", "見当た", "どこに", "どこだ",
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
	// ask を先に、search/attack/talk を後に走査することで、同位置の競合時は
	// より能動的な意図を優先する（例: 「見回して罠を調べる」は search）。
	scan(askKW, "ask", "")
	scan(searchKW, "search", "luck")
	scan(attackKW, "attack", "attack")
	scan(talkKW, "talk", "luck")

	if best < 0 {
		return "move", "", false // 移動・宣言など判定不要
	}
	// ask は判定なし（見れば分かる情報の確認）。他は判定あり。
	needCheck := action == "attack" || action == "search" || action == "talk"
	return action, stat, needCheck
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

	// 0a) この章は既に目標達成済みか。達成済みで「次へ」と言われたら次の場面へ進む。
	cleared := e.Sess.Flag(ch.ClearFlag)
	if cleared && isProceed(input) {
		if next := e.Scn.NextChapter(ch.ID); next != nil {
			e.Sess.ChapterID = next.ID
			e.Sess.SceneSummary = next.SceneSummary
			e.ensureNPCs(next)
			res.Action = "move"
			res.ChapterMoved = true
			res.NewChapter = next
			res.Progressed = true
			return res, nil
		}
		res.Ended = true
		res.Ending = e.computeEnding()
		res.Progressed = true
		return res, nil
	}

	// 0b) 雑談・突っ込み(ooc): 物語を進めず、GMが卓仲間として一言返すだけ。
	if actionType == "ooc" {
		note := "プレイヤーは物語内の行動ではなく、雑談・軽口・突っ込み・質問をした。" +
			"GMとして気さくに一言返し、物語は進めず、操作キャラに次の行動を促すこと。"
		cInput := gm.BuildContext(ch, e.Sess, input, nil, nil, []string{note}, nil)
		narr, err := e.GM.Narrate(ctx, cInput)
		if err != nil {
			return nil, err
		}
		res.Narration = e.cleanNarration(narr)
		return res, nil
	}

	// 0c) 質問・観察(ask): 判定なし・進行なし。今“開示可能な事実だけ”からGMが答える。
	if actionType == "ask" {
		note := "プレイヤーは場面について質問・観察した。" +
			"下の[シーン情報]の範囲だけで答えること。書かれていないことは『分からない/見当たらない』と返す。" +
			"判定はせず、物語も進めない。"
		cInput := gm.BuildContext(ch, e.Sess, input, nil, nil, []string{note}, e.revealableFacts(ch))
		narr, err := e.GM.Narrate(ctx, cInput)
		if err != nil {
			return nil, err
		}
		res.Narration = e.cleanNarration(narr)
		return res, nil
	}

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
		// 探索/会話に成功したら、その章の隠し事実（reveal: search/talk）を開示可能にする。
		if r.Outcome == dice.Success || r.Outcome == dice.CriticalSuccess {
			if actionType == "search" {
				e.Sess.SetFlag("searched_"+ch.ID, true)
			} else if actionType == "talk" {
				e.Sess.SetFlag("talked_"+ch.ID, true)
			}
		}
	}

	// 2.4) 判定が走ったターンは「まず行動の結末を描け」と必ず指示する。
	//      失敗時に結果を描かず投げ返す（「どうする?」だけ）のを防ぐ。
	var notes []string
	if check != nil {
		notes = append(notes, "今回の判定結果は『"+check.Outcome.JP()+
			"』。まずこの行動の結末を1〜2文で具体的に描写すること。"+
			"失敗なら、何が得られなかった／どう空振りしたかを必ず描く。最後に手番を返す。")
	}
	// 2.4b) この行動でシーンの目標が達成される見込みなら、その達成を必ず描かせる。
	//       「成功したのに要点をはぐらかす→中途半端なまま次へ」を防ぐ。
	if !e.Sess.Flag(ch.ClearFlag) && e.willSetClear(ch, actionType, input, check) {
		notes = append(notes, "この行動で、このシーンの目標『"+ch.Goal+"』が達成される。"+
			"その達成を描写にはっきり結実させること（NPCが実際に要点を語る／必要な物が手に入る等）。"+
			"曖昧にはぐらかして終わらせない。")
	}

	// 2.5) 戦闘ルート: ボスのいる章で attack はボスHPを削る複数回戦闘として解決する。
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

	// 4) GM 描写を生成（判定結果・NPC発言・特記・開示可能な事実を統合）
	cInput := gm.BuildContext(ch, e.Sess, input, check, npcLines, notes, e.revealableFacts(ch))
	narr, err := e.GM.Narrate(ctx, cInput)
	if err != nil {
		return nil, err
	}
	res.Narration = e.cleanNarration(narr)

	// 4b) 保険: 判定が走ったのに結果描写が無い（空 or「どうする?」だけ）場合は、
	//     行動種別と成否に応じた定型の結末文を補う。失敗を黙って投げ返さない。
	if check != nil && lacksResult(res.Narration) {
		res.Narration = outcomeFallback(actionType, check.Outcome)
	}

	// 5) フラグ更新（シナリオ管理の制約に従う）。
	e.applyProgress(ch, actionType, input, check)

	// 5b) 章の進行は「シーンを息させる」。目標達成しても即座に進めず、
	//     プレイヤーが「次へ」と決めるまで場面に留まれる（最終章は即エンディング）。
	if e.Sess.Flag(ch.ClearFlag) {
		if e.Scn.NextChapter(ch.ID) != nil {
			res.SceneCleared = true // 先へ進める状態（進むのは「次へ」入力時＝0a）
		} else if !cleared {
			res.Ended = true
			res.Ending = e.computeEnding()
		}
	}

	// 6) 透明性: 実質的な engagement があったかを判定し、無ければヒントを出す。
	res.Progressed = check != nil || res.SceneCleared || res.Ended ||
		res.Combat != "" || countTrueFlags(e.Sess.Flags) > trueFlagsBefore
	if !res.Progressed {
		res.Hint = "この行動は判定にも進行にも結びつきませんでした。" +
			"場面を知りたいなら『周りを見回す』『何がある?』、行動なら『誰に・何をするか』を具体的に。" +
			"\nいまの目標: " + ch.Goal
	}
	return res, nil
}

// cleanNarration は GM 出力から、小型モデルが混ぜがちな
// 「操作キャラのセリフ代弁」と「箇条書きの選択肢メニュー」を除去する。
// （リプレイ風にするため、行動候補メニューもプレイヤー代弁も出さない方針。）
// 全部消えてしまう場合は元の文を返す。
func (e *Engine) cleanNarration(narr string) string {
	pc := e.Sess.Player.Name
	lines := strings.Split(strings.TrimSpace(narr), "\n")
	kept := make([]string, 0, len(lines))
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		// 箇条書きの選択肢メニュー行を捨てる。
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "・") {
			continue
		}
		// 話者表記のない「セリフだけ」の行を捨てる（規範: セリフは話者名＋鉤括弧）。
		// 主人公の台詞の反響など、誰の発言か不明な裸の引用を除去する。
		if strings.HasPrefix(t, "「") && strings.HasSuffix(t, "」") {
			continue
		}
		if pc != "" && isPCAuthored(t, pc) {
			continue // 主人公の行動・セリフをGMが書いた行は捨てる（役割の簒奪を防ぐ）
		}
		kept = append(kept, ln)
	}
	out := strings.TrimSpace(strings.Join(kept, "\n"))
	if out == "" {
		return strings.TrimSpace(narr)
	}
	return out
}

// isPCAuthored は、その行が「主人公（pc）を主語に行動・発話させている」GMの逸脱
// かどうかを判定する。鉤括弧の中身（セリフ）を取り除いてから判定するので、
// NPCのセリフ内に主人公名が出てきても（例: ハル：「カイ、お前…」）誤って消さない。
// 「カイ、どうする?」のような“呼びかけ”は主人公の代弁ではないので残す。
func isPCAuthored(line, pc string) bool {
	if pc == "" {
		return false
	}
	// 「カイ：…」「カイ:…」の話者ラベル、または「カイ「…」」= 主人公のセリフ代弁。
	if strings.HasPrefix(line, pc+"：") || strings.HasPrefix(line, pc+":") {
		return true
	}
	if strings.Contains(line, pc+"「") {
		return true
	}
	// 鉤括弧の外側（地の文）で、主人公名に格助詞が続く＝主人公を主語・目的語・所有格に
	// したナレーション（例:「カイの声が震えた」「カイは歩み寄る」「カイの視線は…」）。
	// 一方「カイ、どうする?」のような呼びかけ（直後が読点・感嘆・疑問・空白）は残す。
	outside := stripQuoted(line)
	for i := 0; ; {
		j := strings.Index(outside[i:], pc)
		if j < 0 {
			break
		}
		i = i + j + len(pc)
		tail := outside[i:]
		if hasAnyPrefix(tail, "は", "が", "の", "を", "も", "に", "へ", "と", "で", "から", "まで", "なら") {
			return true // 主人公を主語・所有格にしたナレーション
		}
	}
	return false
}

// lacksResult は、GM描写が「行動の結末」を含まない（空、または手番を返す
// 一言だけ）かを判定する。手番返し行（"どうする" を含む）を除いて中身が無ければ true。
func lacksResult(narr string) bool {
	var meat []string
	handback := []string{"どうする", "どうしますか", "何をする", "次は何", "次の行動", "行動をお願い", "行動を教え", "決めてください", "決めてくれ"}
	for _, ln := range strings.Split(narr, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || containsAny(t, handback...) {
			continue
		}
		meat = append(meat, t)
	}
	return len(strings.TrimSpace(strings.Join(meat, ""))) == 0
}

// outcomeFallback は行動種別と判定結果から、最低限の結末文を返す（保険）。
func outcomeFallback(action string, o dice.Outcome) string {
	switch action {
	case "search":
		switch o {
		case dice.CriticalSuccess, dice.Success:
			return "目を凝らして調べると、手がかりらしきものが見えてきた。"
		case dice.CriticalFailure:
			return "探ろうとした拍子に手元が狂い、めぼしいものは何も得られないどころか、かえって時間を食ってしまった。"
		default:
			return "ざっと調べてみたが、これといったものは見つからなかった。"
		}
	case "talk":
		switch o {
		case dice.CriticalSuccess, dice.Success:
			return "言葉が届いたようだ。相手の態度が少しやわらいだ。"
		case dice.CriticalFailure:
			return "言葉は逆効果だったらしく、相手は明らかに態度を硬くした。"
		default:
			return "言葉を尽くしてみたが、相手の心は動かなかった。"
		}
	case "attack":
		switch o {
		case dice.CriticalSuccess, dice.Success:
			return "狙いは見事に決まった。"
		case dice.CriticalFailure:
			return "大きく体勢を崩し、反撃を許してしまった。"
		default:
			return "攻撃は空を切った。"
		}
	}
	switch o {
	case dice.CriticalSuccess, dice.Success:
		return "ひとまず、思惑どおりに事は運んだ。"
	case dice.CriticalFailure:
		return "それどころか、状況は少し悪くなった。"
	default:
		return "試みはうまくいかなかった。"
	}
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// stripQuoted は 「…」『…』 で囲まれた部分（セリフ）を取り除いた残りを返す。
func stripQuoted(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '「', '『':
			depth++
		case '」', '』':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// isProceed は「次の場面へ進む」意思表示か。シーン達成後の進行トリガに使う。
func isProceed(input string) bool {
	s := strings.TrimSpace(strings.ToLower(input))
	if s == "次" || s == "next" || s == "n" {
		return true
	}
	return containsAny(input, "次へ", "次の場面", "次に進", "先へ進", "先に進", "前へ進", "進もう", "出発")
}

// revealableFacts は、この章で今“開示してよい事実”だけを返す（隠し事実は含めない）。
func (e *Engine) revealableFacts(ch *scenario.Chapter) []string {
	var out []string
	for _, f := range ch.Facts {
		if e.factRevealable(ch, f) {
			out = append(out, f.Text)
		}
	}
	return out
}

func (e *Engine) factRevealable(ch *scenario.Chapter, f scenario.Fact) bool {
	switch {
	case f.Reveal == "" || f.Reveal == "always" || f.Reveal == "ask":
		return true
	case f.Reveal == "search":
		return e.Sess.Flag("searched_" + ch.ID)
	case f.Reveal == "talk":
		return e.Sess.Flag("talked_" + ch.ID)
	case strings.HasPrefix(f.Reveal, "flag:"):
		return e.Sess.Flag(strings.TrimPrefix(f.Reveal, "flag:"))
	}
	return false
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

// willSetClear は、この行動でこの章の clear_flag が立つ（＝目標達成）見込みかを予測する。
// applyProgress の前に「達成を描写せよ」と GM へ指示するために使う。
func (e *Engine) willSetClear(ch *scenario.Chapter, actionType, input string, check *dice.CheckResult) bool {
	for _, r := range ch.Rules {
		if ch.Boss != nil && r.On == "attack" {
			continue // ボス章の attack は戦闘で解決
		}
		if !ruleMatches(r, actionType, input, check) {
			continue
		}
		for _, f := range r.Sets {
			if f == ch.ClearFlag {
				return true
			}
		}
	}
	// ボス章: 攻撃でボスを倒し切ると defeat_sets が clear_flag を立てる。
	if ch.Boss != nil && actionType == "attack" && check != nil {
		// 実際の撃破判定は resolveCombat 内。ここでは「最後の一撃か」までは予測しない。
		return false
	}
	return false
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
