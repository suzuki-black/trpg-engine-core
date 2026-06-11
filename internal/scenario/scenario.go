// Package scenario は章構造・イベント・NPC人格テンプレートを保持する。
// 設計: docs/05-scenario.md（シナリオ「忘れられた祠の灯」）,
//
//	docs/03-npc-templates.md（NPC人格テンプレート）
//
// 章順序・クリア条件・骨格は固定。細部は GM/NPC=LLM が即興補完する。
package scenario

import "trpg-engine-core/internal/state"

// NPCTemplate は NPC 人格テンプレート。docs/03-npc-templates.md §3.2
type NPCTemplate struct {
	ID           string
	Name         string
	Role         string
	Personality  string
	Tone         string
	PublicGoal   string
	HiddenGoal   string // 通常プレイヤーには明かさない
	InitAttitude state.Attitude
}

// Chapter は章。順序とクリア条件は固定。docs/05-scenario.md §5.4
type Chapter struct {
	ID           string
	Title        string
	Goal         string
	SceneSummary string
	NPCsPresent  []string // この章に登場する NPC ID
	ClearFlag    string   // このフラグが立つと次章へ進める
	ClearHint    string   // GM に渡すクリア条件の説明
}

// Scenario は章リストと NPC テンプレートを保持する。
type Scenario struct {
	Title    string
	World    string
	Chapters []Chapter
	NPCs     map[string]NPCTemplate
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

// ForgottenShrine は実例シナリオ「忘れられた祠の灯」。docs/05-scenario.md
func ForgottenShrine() *Scenario {
	return &Scenario{
		Title: "忘れられた祠の灯",
		World: "中世風ファンタジー。深い森のふもとの小さな村『リンデン』と、" +
			"近くの古い遺跡『忘れられた祠』。夜ごと祠から青白い光が漏れ、村人は災いを恐れている。",
		NPCs: map[string]NPCTemplate{
			"karasu": {
				ID: "karasu", Name: "カラス", Role: "街の情報屋。酒場を根城にする",
				Personality:  "抜け目なく、皮肉屋。だが約束は守る",
				Tone:         "ぶっきらぼうで、軽口を挟む",
				PublicGoal:   "情報を売って日銭を稼ぐ",
				HiddenGoal:   "遺跡に眠る品の価値を知っており、プレイヤーより先に在処を確かめたい",
				InitAttitude: state.Neutral,
			},
			"mireille": {
				ID: "mireille", Name: "ミレーユ", Role: "諸国を巡る旅の僧侶",
				Personality:  "穏やかで思いやり深いが、芯は強い",
				Tone:         "丁寧で柔らかい",
				PublicGoal:   "困っている人々を助け、遺跡に巣食う災いを鎮めたい",
				HiddenGoal:   "かつてこの遺跡で仲間を失っており、その真相を確かめたい私情がある",
				InitAttitude: state.Friendly,
			},
			"gorz": {
				ID: "gorz", Name: "ゴルツ", Role: "流れの傭兵。見た目はならず者だが根は善人",
				Personality:  "粗暴に見えて面倒見がよい。情に弱い",
				Tone:         "乱暴で短気だが、時折優しさが滲む",
				PublicGoal:   "金になる依頼を探している",
				HiddenGoal:   "故郷の村を救う金を貯めており、本当は危険な仕事を避けたい",
				InitAttitude: state.Neutral,
			},
		},
		Chapters: []Chapter{
			{
				ID: "ch01", Title: "村での依頼",
				Goal:         "依頼を受注し、祠についての情報を集める",
				SceneSummary: "夕暮れのリンデン村。村長の家と、薄暗い酒場。情報屋カラスがいる。",
				NPCsPresent:  []string{"karasu", "mireille"},
				ClearFlag:    "location_known",
				ClearHint:    "依頼を受諾し、情報屋カラスから祠の場所を聞き出せばクリア。",
			},
			{
				ID: "ch02", Title: "遺跡への道中",
				Goal:         "祠まで無事にたどり着く",
				SceneSummary: "霧の立ちこめる森の道。崩れかけた橋。ならず者ゴルツと遭遇しうる。",
				NPCsPresent:  []string{"gorz"},
				ClearFlag:    "reached_shrine",
				ClearHint:    "森を抜け、祠の入口に到達すればクリア。戦う/迂回する/ゴルツを仲間にする等で分岐。",
			},
			{
				ID: "ch03", Title: "遺跡内部の探索",
				Goal:         "祠の最奥へ進む手段を見つける",
				SceneSummary: "苔むした石の広間。床に溝、壁に紋章。罠と仕掛けがある。",
				NPCsPresent:  []string{},
				ClearFlag:    "inner_door_opened",
				ClearHint:    "罠を抜け、紋章の謎を解いて最奥の扉を開ければクリア。手がかり(clue_found)も拾える。",
			},
			{
				ID: "ch04", Title: "対決または交渉",
				Goal:         "異変の元凶（歪んだ精霊）と決着をつける",
				SceneSummary: "祠の最奥。青白い光を放つ、歪んだ精霊が漂う。嘆きの声が響く。",
				NPCsPresent:  []string{},
				ClearFlag:    "boss_resolved",
				ClearHint: "精霊と決着。戦闘(spirit_defeated)または交渉(spirit_soothed)。" +
					"clue_found があれば説得が1段階容易、mireille_ally がいれば交渉に有利。",
			},
			{
				ID: "ch05", Title: "結末 — 村への帰還",
				Goal:         "結果を村に持ち帰り、物語を締める",
				SceneSummary: "夜明けのリンデン村。村長が結果を待っている。",
				NPCsPresent:  []string{},
				ClearFlag:    "ending_reached",
				ClearHint:    "村長へ報告し、真実をどこまで語るか選べばエンディング。",
			},
		},
	}
}
