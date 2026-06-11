// Package dice は D20 判定エンジン（D20 Resolver）を実装する。
// 設計: docs/04-d20-rules.md
//
// 決定論的モジュール。行動種別・使用ステータス・難易度クラスを受け取り、
// D20 の出目 + ステータス修正値 を DC と比較して outcome を返す。
// 物語的解釈は一切行わない（それは GM=LLM の仕事）。
package dice

import "math/rand"

// Outcome は判定結果の 4 段階。docs/00-overview.md の用語に一致。
type Outcome string

const (
	CriticalSuccess Outcome = "critical_success"
	Success         Outcome = "success"
	Failure         Outcome = "failure"
	CriticalFailure Outcome = "critical_failure"
)

// 日本語表示用ラベル。GM プロンプトの「判定結果」入力に使う。
func (o Outcome) JP() string {
	switch o {
	case CriticalSuccess:
		return "クリティカル成功"
	case Success:
		return "成功"
	case Failure:
		return "失敗"
	case CriticalFailure:
		return "クリティカル失敗"
	}
	return string(o)
}

// 難易度ランク → DC マッピング。docs/04-d20-rules.md §4.4
var DifficultyTable = map[string]int{
	"easy":      8,
	"normal":    12,
	"hard":      15,
	"very_hard": 18,
}

const (
	CriticalSuccessThreshold = 20 // この出目でDCに関わらずクリティカル成功
	CriticalFailureThreshold = 1  // この出目でクリティカル失敗
)

// CheckRequest は判定リクエスト。docs/01-architecture.md §1.5(C)
// 各フィールドは責務分担で決定される（§1.6）:
//
//	ActionType  … セッション管理が分類
//	Difficulty  … シナリオ管理が決定
//	UsedStat    … プレイヤー状態管理が特定
//	StatModifier… プレイヤー状態管理が提供
type CheckRequest struct {
	ActionType   string `json:"action_type"`
	UsedStat     string `json:"used_stat"`
	Difficulty   string `json:"difficulty_class"` // ランク名 ("hard") を想定
	StatModifier int    `json:"stat_modifier"`
}

// CheckResult は判定エンジンの返却。docs/00-overview.md §5 の返却例に一致。
type CheckResult struct {
	Roll       int     `json:"roll"`
	StatMod    int     `json:"stat_modifier"`
	Total      int     `json:"total"`
	DC         int     `json:"dc"`
	Outcome    Outcome `json:"outcome"`
	ActionType string  `json:"action_type"`
}

// Resolver は D20 の出目供給関数を保持する。
// 本番は乱数源、テストは固定値を注入でき、境界を決定論的に検証できる。
type Resolver struct {
	d20 func() int // 1..20 を返す
}

// NewResolver は乱数源から D20 を引く本番用コンストラクタ。
func NewResolver(rng *rand.Rand) *Resolver {
	return &Resolver{d20: func() int { return rng.Intn(20) + 1 }}
}

// newResolverWithRoll はテスト用。固定の出目関数を注入する。
func newResolverWithRoll(roll func() int) *Resolver {
	return &Resolver{d20: roll}
}

// dc はランク名 or 生の数値文字列を DC に解決する。未知なら normal 扱い。
func resolveDC(difficulty string) int {
	if v, ok := DifficultyTable[difficulty]; ok {
		return v
	}
	return DifficultyTable["normal"]
}

// Resolve は判定を実行する。基本式:
//
//	total = D20 + stat_modifier ; 成功 = total >= DC
//
// 出目20で無条件クリティカル成功、出目1で無条件クリティカル失敗。
func (r *Resolver) Resolve(req CheckRequest) CheckResult {
	roll := r.d20() // 1..20
	dc := resolveDC(req.Difficulty)
	total := roll + req.StatModifier

	var out Outcome
	switch {
	case roll >= CriticalSuccessThreshold:
		out = CriticalSuccess
	case roll <= CriticalFailureThreshold:
		out = CriticalFailure
	case total >= dc:
		out = Success
	default:
		out = Failure
	}

	return CheckResult{
		Roll:       roll,
		StatMod:    req.StatModifier,
		Total:      total,
		DC:         dc,
		Outcome:    out,
		ActionType: req.ActionType,
	}
}
