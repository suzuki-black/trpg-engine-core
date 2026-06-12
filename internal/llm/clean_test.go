package llm

import (
	"context"
	"testing"
)

// scripted は呼ばれるたびに用意した応答を順に返すフェイククライアント。
type scripted struct {
	replies []string
	calls   int
}

func (s *scripted) Name() string { return "scripted" }
func (s *scripted) Generate(ctx context.Context, system, user string) (string, error) {
	r := s.replies[min(s.calls, len(s.replies)-1)]
	s.calls++
	return r, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 1回目に中国語混入、2回目で日本語 → クリーンな2回目を採用する。
func TestGenerateCleanRetriesUntilClean(t *testing.T) {
	c := &scripted{replies: []string{
		"红い光が沟を照らす", // dirty（簡体字）
		"紅い光が溝を照らす", // clean
	}}
	out, issues := GenerateClean(context.Background(), c, "sys", "user", 2)
	if len(issues) != 0 {
		t.Errorf("クリーンな出力を採用できていない: issues=%v", issues)
	}
	if out != "紅い光が溝を照らす" {
		t.Errorf("採用された出力 = %q", out)
	}
	if c.calls != 2 {
		t.Errorf("呼び出し回数 = %d, want 2（1回目dirty→再生成）", c.calls)
	}
}

// 全試行が dirty なら、問題カテゴリ数が最も少ない出力を返す（諦めても破綻しない）。
func TestGenerateCleanFallsBackToBest(t *testing.T) {
	c := &scripted{replies: []string{
		"红 tone です",  // 簡体字 + ラテン語 = 2カテゴリ
		"红 です",       // 簡体字のみ = 1カテゴリ（最良）
		"沟 Favor です", // 簡体字 + ラテン語 = 2カテゴリ
	}}
	out, issues := GenerateClean(context.Background(), c, "sys", "user", 2)
	if len(issues) == 0 {
		t.Fatal("dirtyのみのはずが issues が空")
	}
	if out != "红 です" {
		t.Errorf("最良（問題カテゴリ最少）の出力が選ばれていない: %q", out)
	}
}

// maxRetries=0 なら1回だけ生成（再生成しない）。
func TestGenerateCleanNoRetry(t *testing.T) {
	c := &scripted{replies: []string{"红い光"}}
	_, issues := GenerateClean(context.Background(), c, "sys", "user", 0)
	if len(issues) == 0 {
		t.Error("dirty を検出できていない")
	}
	if c.calls != 1 {
		t.Errorf("呼び出し回数 = %d, want 1", c.calls)
	}
}
