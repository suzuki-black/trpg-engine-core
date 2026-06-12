package llm

import (
	"context"

	"trpg-engine-core/internal/textqc"
)

// 再生成時に付け足す強い指示。日本語以外の混入を抑制する。
const reinforceJP = "\n\n【重要・前回の修正】前回の出力には日本語以外（外国語の単語・中国語の簡体字など）が混じっていました。" +
	"今回は必ず自然な日本語のみで、同じ内容を書き直してください。英単語・中国語の文字を使わないこと。"

// GenerateClean は Generate を呼び、出力に非日本語の混入があれば最大 maxRetries 回まで
// 「日本語で書き直せ」と促して再生成する。全試行が不完全なら、最も問題の少ない出力を返す。
// ignore は検査時に無視する部分文字列（NPC の "line:"/"tone:" ラベル等）。
//
// 戻り値: 採用した出力, 残った問題（クリーンなら nil）。
func GenerateClean(ctx context.Context, c Client, system, user string, maxRetries int, ignore ...string) (string, []string) {
	var best string
	var bestIssues []string
	haveBest := false

	for attempt := 0; attempt <= maxRetries; attempt++ {
		u := user
		if attempt > 0 {
			u = user + reinforceJP
		}
		out, err := c.Generate(ctx, system, u)
		if err != nil {
			continue
		}
		issues := textqc.Issues(out, ignore...)
		if len(issues) == 0 {
			return out, nil // クリーンなら即採用
		}
		if !haveBest || len(issues) < len(bestIssues) {
			best, bestIssues, haveBest = out, issues, true
		}
	}
	return best, bestIssues
}
