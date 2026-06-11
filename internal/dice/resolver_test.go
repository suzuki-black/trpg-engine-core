package dice

import "testing"

// fixed は常に同じ出目を返すヘルパ。
func fixed(roll int) *Resolver {
	return newResolverWithRoll(func() int { return roll })
}

// 難易度ランク → DC マッピング。docs/04-d20-rules.md §4.4
func TestDifficultyMapping(t *testing.T) {
	cases := map[string]int{
		"easy":      8,
		"normal":    12,
		"hard":      15,
		"very_hard": 18,
	}
	for rank, wantDC := range cases {
		if got := resolveDC(rank); got != wantDC {
			t.Errorf("resolveDC(%q) = %d, want %d", rank, got, wantDC)
		}
	}
	// 未知ランクは normal(12) にフォールバック。
	if got := resolveDC("unknown"); got != 12 {
		t.Errorf("resolveDC(unknown) = %d, want 12 (normal fallback)", got)
	}
}

// クリティカル境界: 出目20は DC に関わらず CriticalSuccess、出目1は CriticalFailure。
func TestCriticalBoundaries(t *testing.T) {
	// 出目20: 難易度 very_hard でも修正値0でもクリティカル成功。
	got := fixed(20).Resolve(CheckRequest{Difficulty: "very_hard", StatModifier: 0})
	if got.Outcome != CriticalSuccess {
		t.Errorf("roll=20 → %s, want %s", got.Outcome, CriticalSuccess)
	}
	// 出目1: 大きな修正値で total が DC を超えてもクリティカル失敗。
	got = fixed(1).Resolve(CheckRequest{Difficulty: "easy", StatModifier: 100})
	if got.Outcome != CriticalFailure {
		t.Errorf("roll=1 (total=101 vs DC8) → %s, want %s", got.Outcome, CriticalFailure)
	}
}

// 通常の成功/失敗境界: total >= DC で成功、未満で失敗。normal=DC12。
func TestSuccessFailureBoundary(t *testing.T) {
	// roll=11, mod=+1 → total=12 == DC12 → 成功（>= 判定）。
	got := fixed(11).Resolve(CheckRequest{Difficulty: "normal", StatModifier: 1})
	if got.Outcome != Success {
		t.Errorf("total=12 vs DC12 → %s, want %s", got.Outcome, Success)
	}
	if got.Total != 12 || got.DC != 12 {
		t.Errorf("total/DC = %d/%d, want 12/12", got.Total, got.DC)
	}
	// roll=10, mod=+1 → total=11 < DC12 → 失敗。
	got = fixed(10).Resolve(CheckRequest{Difficulty: "normal", StatModifier: 1})
	if got.Outcome != Failure {
		t.Errorf("total=11 vs DC12 → %s, want %s", got.Outcome, Failure)
	}
}

// 修正値が結果に反映されることの確認（負の修正値で失敗に転ぶ）。
func TestStatModifierApplied(t *testing.T) {
	// roll=12, mod=-1 → total=11 < DC12 → 失敗。
	got := fixed(12).Resolve(CheckRequest{Difficulty: "normal", StatModifier: -1})
	if got.Total != 11 {
		t.Errorf("total = %d, want 11", got.Total)
	}
	if got.Outcome != Failure {
		t.Errorf("total=11 vs DC12 → %s, want %s", got.Outcome, Failure)
	}
}

// outcome の日本語ラベル。
func TestOutcomeJP(t *testing.T) {
	cases := map[Outcome]string{
		CriticalSuccess: "クリティカル成功",
		Success:         "成功",
		Failure:         "失敗",
		CriticalFailure: "クリティカル失敗",
	}
	for o, want := range cases {
		if got := o.JP(); got != want {
			t.Errorf("%s.JP() = %q, want %q", o, got, want)
		}
	}
}
