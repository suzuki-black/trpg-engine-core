// Package state はセッション状態を保持する。
// 設計: docs/00-overview.md §4（保持項目）, docs/01-architecture.md §1.3(1)(2)
//
// 保持項目: プレイヤー状態 / NPC状態 / 現在の章・シーン /
//
//	発生済みイベント / フラグ / インベントリ / 世界状態
package state

// PlayerCharacter はプレイヤーキャラクター。docs/01-architecture.md §1.5(A)
type PlayerCharacter struct {
	Name      string         `json:"name"`
	Class     string         `json:"class"`
	Stats     map[string]int `json:"stats"` // luck, attack, life など
	Inventory []string       `json:"inventory"`
}

// Modifier はステータス値→修正値の変換。docs/04-d20-rules.md §4.4
//
//	修正値 = floor((stat - 10) / 2)
func (p *PlayerCharacter) Modifier(stat string) int {
	v := p.Stats[stat]
	d := v - 10
	if d >= 0 {
		return d / 2
	}
	// Go の整数除算は 0 方向に丸めるため、負側は floor になるよう調整
	return -((-d + 1) / 2)
}

// Attitude は NPC のプレイヤーへの態度（3段階）。docs/03-npc-templates.md §3.6
type Attitude string

const (
	Hostile  Attitude = "hostile"
	Neutral  Attitude = "neutral"
	Friendly Attitude = "friendly"
)

func (a Attitude) JP() string {
	switch a {
	case Hostile:
		return "敵対的"
	case Friendly:
		return "友好的"
	default:
		return "中立"
	}
}

// Up / Down は態度を1段階だけ動かす（2段階は飛ばさない）。§3.6
func (a Attitude) Up() Attitude {
	switch a {
	case Hostile:
		return Neutral
	case Neutral:
		return Friendly
	}
	return Friendly
}

func (a Attitude) Down() Attitude {
	switch a {
	case Friendly:
		return Neutral
	case Neutral:
		return Hostile
	}
	return Hostile
}

// NPCState は実行時の NPC 状態（態度はここで変化し次ターンへ引き継がれる）。
// 人格テンプレート本体は scenario パッケージが保持する。
type NPCState struct {
	ID       string   `json:"id"`
	Attitude Attitude `json:"attitude"`
}

// World は世界状態。docs/00-overview.md §4
type World struct {
	TimeOfDay     string `json:"time_of_day"` // 朝/昼/夕/夜
	Weather       string `json:"weather"`
	Alertness     string `json:"alertness"` // 警戒度
	DungeonDanger string `json:"dungeon_danger"`
	Ambient       string `json:"ambient"` // 光源・音・匂いなどの環境要素
}

// Boss は第4章の戦闘ルート用のボス状態。HP制で複数回 attack 判定する。
// docs/05-scenario.md §5.4 第4章（戦闘ルート）
type Boss struct {
	Name   string `json:"name"`
	HP     int    `json:"hp"`
	MaxHP  int    `json:"max_hp"`
	Active bool   `json:"active"` // 戦闘が初期化済みか
}

func (b *Boss) Alive() bool { return b.Active && b.HP > 0 }

// Session は 1 プレイ全体の状態。司令塔=セッション管理が保持する。
type Session struct {
	Player       PlayerCharacter      `json:"player"`
	NPCs         map[string]*NPCState `json:"npcs"`
	ChapterID    string               `json:"chapter_id"`
	SceneSummary string               `json:"scene_summary"`
	DoneEvents   map[string]bool      `json:"done_events"` // 発生済みイベント
	Flags        map[string]bool      `json:"flags"`
	World        World                `json:"world"`
	Boss         Boss                 `json:"boss"`
	Conversation []string             `json:"conversation"` // シーン内の会話履歴（章替えでリセット）
}

func NewSession() *Session {
	return &Session{
		NPCs:       map[string]*NPCState{},
		DoneEvents: map[string]bool{},
		Flags:      map[string]bool{},
	}
}

// --- ステータス更新ルール（docs/01-architecture.md §1.3(2)）---
// ライフ減少は判定/イベントに基づく。アイテム取得・消費はセッション管理が確定。

func (s *Session) DamageLife(n int) {
	s.Player.Stats["life"] -= n
	if s.Player.Stats["life"] < 0 {
		s.Player.Stats["life"] = 0
	}
}

func (s *Session) AddItem(item string) {
	s.Player.Inventory = append(s.Player.Inventory, item)
}

func (s *Session) SetFlag(name string, v bool) { s.Flags[name] = v }
func (s *Session) Flag(name string) bool       { return s.Flags[name] }
func (s *Session) MarkEvent(id string)         { s.DoneEvents[id] = true }
func (s *Session) EventDone(id string) bool    { return s.DoneEvents[id] }

func (s *Session) NPC(id string) *NPCState {
	if n, ok := s.NPCs[id]; ok {
		return n
	}
	return nil
}
