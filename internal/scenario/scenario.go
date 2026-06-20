// Package scenario は章構造・イベント・NPC人格テンプレート・進行ルールを保持する。
// 設計: docs/05-scenario.md（シナリオ「忘れられた祠の灯」）, docs/03-npc-templates.md
//
// シナリオは JSON で外部定義する（TODO#4）。進行ルール（どの行動でどのフラグが
// 立つか）・難易度・ボーナス・ボス・エンディングまでデータ化したので、エンジンは
// 特定シナリオに依存しない。既定シナリオはバイナリに埋め込んでいる。
package scenario

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"trpg-engine-core/internal/state"
)

// NPCTemplate は NPC 人格テンプレート。docs/03-npc-templates.md §3.2
type NPCTemplate struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Role         string         `json:"role"`
	Personality  string         `json:"personality"`
	Tone         string         `json:"tone"`
	PublicGoal   string         `json:"public_goal"`
	HiddenGoal   string         `json:"hidden_goal"` // 通常プレイヤーには明かさない
	InitAttitude state.Attitude `json:"init_attitude"`
}

// Rule は「行動と判定結果に応じてフラグを立てる／ダメージを与える」進行ルール。
// 章スキップや誤進行を避けるため、条件を満たした時のみ作用する。
type Rule struct {
	On          string   `json:"on"`           // talk|search|attack|move|any（既定 any）
	Outcome     string   `json:"outcome"`      // success|critsuccess|fail|critfail|any（既定 any）
	RequiresAll []string `json:"requires_all"` // 入力に全て含まれること
	RequiresAny []string `json:"requires_any"` // 入力にいずれか含まれること
	ExcludesAny []string `json:"excludes_any"` // 入力にいずれも含まれないこと
	Sets        []string `json:"sets"`         // 立てるフラグ
	Damage      int      `json:"damage"`       // プレイヤーへのライフ減少
}

// Bonus は特定フラグ保持時の判定補正（過去の選択が後の判定を有利にする）。
type Bonus struct {
	Flag     string `json:"flag"`     // このフラグが立っていると…
	On       string `json:"on"`       // この行動種別の判定に対して
	Ease     bool   `json:"ease"`     // 難易度を1段階緩和する
	Modifier int    `json:"modifier"` // 判定値に加える修正
}

// Boss は戦闘ルート用のボス定義（HP制）。撃破で DefeatSets のフラグが立つ。
type Boss struct {
	Name       string   `json:"name"`
	HP         int      `json:"hp"`
	DefeatSets []string `json:"defeat_sets"`
}

// Entity はシーンに居る／在る人・物・地物（質問への回答や描写に使う）。
type Entity struct {
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	Position string `json:"position"` // 位置関係（任意）
}

// Fact はシーンの事実（想定問答）。Reveal で開示条件を制御する。
//
//	always : 常に開示（場面説明にも含める）
//	ask    : 聞かれたら答えてよい
//	flag:X : フラグ X が立ってから開示
//	search : この章で探索(search)に成功してから開示（隠し情報）
//	talk   : この章で会話(talk)に成功してから開示
type Fact struct {
	Text   string `json:"text"`
	Reveal string `json:"reveal"`
}

// Chapter は章。順序とクリア条件は固定。進行ルール・シーン情報もデータで持つ。
type Chapter struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Goal          string   `json:"goal"`
	SceneSummary  string   `json:"scene_summary"`
	Layout        string   `json:"layout"` // 位置関係の説明（任意）
	NPCsPresent   []string `json:"npcs_present"`
	Entities      []Entity `json:"entities"` // 登場物（質問・描写の素材）
	Facts         []Fact   `json:"facts"`    // 事実リスト（想定問答・開示条件つき）
	ClearFlag     string   `json:"clear_flag"`
	ClearRequires []string `json:"clear_requires"` // clear_flag に加えて全て真が必要な前提フラグ（任意）
	ClearHint     string   `json:"clear_hint"`
	Difficulty    string   `json:"difficulty"`
	Rules         []Rule   `json:"rules"`
	Bonuses       []Bonus  `json:"bonuses"`
	Boss          *Boss    `json:"boss"`
}

// Ending は最終分岐。requires を満たす最初のものが採用される。
type Ending struct {
	RequiresAll []string `json:"requires_all"`
	RequiresAny []string `json:"requires_any"`
	Text        string   `json:"text"`
}

// StartState はシナリオ開始時のプレイヤー・世界状態（任意）。
// 省略時は CLI の既定値が使われる。シナリオを自己完結させるために使う。
type StartState struct {
	PlayerName  string         `json:"player_name"`
	PlayerClass string         `json:"player_class"`
	Stats       map[string]int `json:"stats"`
	Inventory   []string       `json:"inventory"`
	World       state.World    `json:"world"`
}

// Scenario は章リスト・NPC・エンディング・開始状態を保持する。
type Scenario struct {
	Title    string                 `json:"title"`
	World    string                 `json:"world"`
	Start    *StartState            `json:"start"`
	Chapters []Chapter              `json:"chapters"`
	NPCs     map[string]NPCTemplate `json:"npcs"`
	Endings  []Ending               `json:"endings"`
}

func (s *Scenario) Chapter(id string) *Chapter {
	for i := range s.Chapters {
		if s.Chapters[i].ID == id {
			return &s.Chapters[i]
		}
	}
	return nil
}

func (s *Scenario) NextChapter(id string) *Chapter {
	for i := range s.Chapters {
		if s.Chapters[i].ID == id && i+1 < len(s.Chapters) {
			return &s.Chapters[i+1]
		}
	}
	return nil
}

//go:embed scenarios/forgotten-shrine.json
var defaultJSON []byte

// LoadJSON は JSON バイト列から Scenario を構築・検証する。
func LoadJSON(b []byte) (*Scenario, error) {
	var s Scenario
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("シナリオの解析に失敗: %w", err)
	}
	// NPC の ID をマップのキーから補完する（JSON側で省略可能にする）。
	for id, npc := range s.NPCs {
		if npc.ID == "" {
			npc.ID = id
			s.NPCs[id] = npc
		}
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// Load はファイルパスから Scenario を読み込む。
func Load(path string) (*Scenario, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("シナリオファイルの読み込みに失敗: %w", err)
	}
	return LoadJSON(b)
}

// validate は最低限の整合性を検査する。
func (s *Scenario) validate() error {
	if len(s.Chapters) == 0 {
		return fmt.Errorf("章が定義されていない")
	}
	for _, ch := range s.Chapters {
		if ch.ID == "" {
			return fmt.Errorf("章IDが空の章がある")
		}
		if ch.ClearFlag == "" {
			return fmt.Errorf("章 %s に clear_flag がない", ch.ID)
		}
		for _, id := range ch.NPCsPresent {
			if _, ok := s.NPCs[id]; !ok {
				return fmt.Errorf("章 %s が未定義のNPC %q を参照している", ch.ID, id)
			}
		}
		if ch.Boss != nil && len(ch.Boss.DefeatSets) == 0 {
			return fmt.Errorf("章 %s のボスに defeat_sets がない", ch.ID)
		}
		for _, f := range ch.Facts {
			if !validReveal(f.Reveal) {
				return fmt.Errorf("章 %s の事実 %q に不正な reveal 値 %q", ch.ID, f.Text, f.Reveal)
			}
		}
	}
	if len(s.Endings) == 0 {
		return fmt.Errorf("エンディングが定義されていない")
	}
	return nil
}

// validReveal は事実の開示条件が既知の形式か（authoring のタイポ検出）。
//
//	"" / always / ask / search / talk / flag:<name>
func validReveal(r string) bool {
	switch r {
	case "", "always", "ask", "search", "talk":
		return true
	}
	return strings.HasPrefix(r, "flag:") && len(r) > len("flag:")
}

// ForgottenShrine は既定シナリオ「忘れられた祠の灯」（埋め込みJSONを解析）。
// 埋め込みデータは信頼できるため、壊れていればパニックさせる。
func ForgottenShrine() *Scenario {
	s, err := LoadJSON(defaultJSON)
	if err != nil {
		panic("既定シナリオの読み込みに失敗: " + err.Error())
	}
	return s
}
