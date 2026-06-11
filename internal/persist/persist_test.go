package persist

import (
	"path/filepath"
	"testing"

	"trpg-engine-core/internal/state"
)

// セーブ→ロードで主要な状態が往復すること。
func TestSaveLoadRoundTrip(t *testing.T) {
	sess := state.NewSession()
	sess.Player = state.PlayerCharacter{
		Name: "アルド", Class: "戦士",
		Stats:     map[string]int{"luck": 12, "attack": 14, "life": 17},
		Inventory: []string{"たいまつ", "回復薬"},
	}
	sess.ChapterID = "ch04"
	sess.SceneSummary = "祠の最奥"
	sess.SetFlag("clue_found", true)
	sess.SetFlag("mireille_ally", true)
	sess.MarkEvent("met_karasu")
	sess.NPCs["mireille"] = &state.NPCState{ID: "mireille", Attitude: state.Friendly}
	sess.Boss = state.Boss{Name: "歪んだ精霊", HP: 5, MaxHP: 12, Active: true}
	sess.World = state.World{TimeOfDay: "夜", Weather: "霧"}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := Save(path, sess); err != nil {
		t.Fatalf("Save 失敗: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失敗: %v", err)
	}

	if got.ChapterID != "ch04" {
		t.Errorf("ChapterID = %q, want ch04", got.ChapterID)
	}
	if got.Player.Stats["life"] != 17 {
		t.Errorf("life = %d, want 17", got.Player.Stats["life"])
	}
	if !got.Flag("clue_found") || !got.Flag("mireille_ally") {
		t.Errorf("フラグが復元されていない: %+v", got.Flags)
	}
	if !got.EventDone("met_karasu") {
		t.Error("発生済みイベントが復元されていない")
	}
	if n := got.NPC("mireille"); n == nil || n.Attitude != state.Friendly {
		t.Errorf("NPC態度が復元されていない: %+v", n)
	}
	if got.Boss.HP != 5 || !got.Boss.Active {
		t.Errorf("ボス状態が復元されていない: %+v", got.Boss)
	}
	if got.World.Weather != "霧" {
		t.Errorf("世界状態が復元されていない: %+v", got.World)
	}
}

// 非対応バージョンや欠損ファイルはエラーになる。
func TestLoadErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("存在しないファイルでエラーにならなかった")
	}
}
