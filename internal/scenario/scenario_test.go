package scenario

import (
	"encoding/json"
	"strings"
	"testing"
)

// 内蔵シナリオが読み込めて、期待する構造を持つ。
func TestDefaultScenarioLoads(t *testing.T) {
	s := ForgottenShrine()
	if s.Title == "" || len(s.Chapters) != 5 {
		t.Fatalf("内蔵シナリオが不正: title=%q chapters=%d", s.Title, len(s.Chapters))
	}
	// NPC の ID がキーから補完されている。
	if s.NPCs["karasu"].ID != "karasu" {
		t.Errorf("NPC ID がキーから補完されていない: %q", s.NPCs["karasu"].ID)
	}
	// 第4章にボスがあり、撃破フラグが定義されている。
	ch4 := s.Chapter("ch04")
	if ch4 == nil || ch4.Boss == nil || ch4.Boss.HP != 12 {
		t.Fatalf("第4章のボス定義が不正: %+v", ch4)
	}
	if len(ch4.Boss.DefeatSets) == 0 {
		t.Error("ボスに defeat_sets がない")
	}
	if len(s.Endings) == 0 {
		t.Error("エンディングが無い")
	}
}

// JSON 往復で内容が保たれる。
func TestScenarioRoundTrip(t *testing.T) {
	s := ForgottenShrine()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := LoadJSON(b)
	if err != nil {
		t.Fatalf("再読み込み失敗: %v", err)
	}
	if got.Title != s.Title || len(got.Chapters) != len(s.Chapters) {
		t.Error("往復で内容が変わった")
	}
}

// 不正なシナリオは検証で弾かれる。
func TestValidationRejectsBadScenario(t *testing.T) {
	cases := map[string]string{
		"章なし":          `{"title":"t","endings":[{"text":"e"}]}`,
		"clear_flag欠落": `{"title":"t","chapters":[{"id":"c1"}],"endings":[{"text":"e"}]}`,
		"未定義NPC参照":     `{"title":"t","chapters":[{"id":"c1","clear_flag":"f","npcs_present":["nobody"]}],"endings":[{"text":"e"}]}`,
		"エンディングなし":     `{"title":"t","chapters":[{"id":"c1","clear_flag":"f"}]}`,
	}
	for name, js := range cases {
		if _, err := LoadJSON([]byte(js)); err == nil {
			t.Errorf("%s: エラーになるべきだが通った", name)
		}
	}
}

// 不正な JSON 構文はエラー。
func TestInvalidJSON(t *testing.T) {
	if _, err := LoadJSON([]byte("{not json")); err == nil || !strings.Contains(err.Error(), "解析") {
		t.Errorf("構文エラーが報告されない: %v", err)
	}
}
