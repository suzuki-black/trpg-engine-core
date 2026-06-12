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
	"path/filepath"
	"sort"
	"strings"
	"time"

	"trpg-engine-core/internal/state"
)

// SaveFile はセーブデータのトップレベル構造。将来の互換のため version を持つ。
type SaveFile struct {
	Version   int            `json:"version"`
	SavedAt   time.Time      `json:"saved_at"`
	ChapterID string         `json:"chapter_id"` // 一覧表示用（Session 内と冗長だが手軽）
	Session   *state.Session `json:"session"`
}

const currentVersion = 1

// now はテストで差し替え可能な時刻源。
var now = time.Now

// Save は session を path に JSON で書き出す。保存時刻を刻む。
func Save(path string, sess *state.Session) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("セーブ先ディレクトリの作成に失敗: %w", err)
		}
	}
	data, err := json.MarshalIndent(SaveFile{
		Version:   currentVersion,
		SavedAt:   now(),
		ChapterID: sess.ChapterID,
		Session:   sess,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("セーブのシリアライズに失敗: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("セーブファイルの書き込みに失敗: %w", err)
	}
	return nil
}

// SaveInfo は一覧表示用のセーブ要約。
type SaveInfo struct {
	Name      string // 拡張子を除いたスロット名
	Path      string
	ChapterID string
	SavedAt   time.Time
}

// List は dir 内の *.json セーブを新しい順に列挙する。dir が無ければ空を返す。
func List(dir string) ([]SaveInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var infos []SaveInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		var sf SaveFile
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &sf) // 壊れていても名前だけは出す
		}
		infos = append(infos, SaveInfo{
			Name:      strings.TrimSuffix(e.Name(), ".json"),
			Path:      path,
			ChapterID: sf.ChapterID,
			SavedAt:   sf.SavedAt,
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].SavedAt.After(infos[j].SavedAt) })
	return infos, nil
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
