// Package persist はセッション状態の保存・読込を担う。
// 設計: docs/06-tech-stack.md §6.3（状態保持: SQLite または JSON）
//
// 初期実装は JSON ファイル。state.Session は全フィールドに json タグを持つため、
// そのままシリアライズできる（プレイヤー/NPC態度/章/フラグ/世界/ボスHP すべて含む）。
package persist

import (
	"encoding/json"
	"fmt"
	"os"

	"trpg-engine-core/internal/state"
)

// SaveFile はセーブデータのトップレベル構造。将来の互換のため version を持つ。
type SaveFile struct {
	Version int            `json:"version"`
	Session *state.Session `json:"session"`
}

const currentVersion = 1

// Save は session を path に JSON で書き出す。
func Save(path string, sess *state.Session) error {
	data, err := json.MarshalIndent(SaveFile{
		Version: currentVersion,
		Session: sess,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("セーブのシリアライズに失敗: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("セーブファイルの書き込みに失敗: %w", err)
	}
	return nil
}

// Load は path から session を復元する。
func Load(path string) (*state.Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("セーブファイルの読み込みに失敗: %w", err)
	}
	var sf SaveFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("セーブデータの解析に失敗: %w", err)
	}
	if sf.Session == nil {
		return nil, fmt.Errorf("セーブデータにセッションが含まれていない")
	}
	if sf.Version != currentVersion {
		return nil, fmt.Errorf("非対応のセーブバージョン: %d（対応: %d）", sf.Version, currentVersion)
	}
	// マップ類が nil の場合に備えて初期化（古い/手書きデータ対策）。
	s := sf.Session
	if s.NPCs == nil {
		s.NPCs = map[string]*state.NPCState{}
	}
	if s.Flags == nil {
		s.Flags = map[string]bool{}
	}
	if s.DoneEvents == nil {
		s.DoneEvents = map[string]bool{}
	}
	return s, nil
}
