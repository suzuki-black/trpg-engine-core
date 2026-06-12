package textqc

import (
	"strings"
	"testing"
)

// 自然な日本語は問題なしと判定される（誤検出しない）。
func TestCleanJapanese(t *testing.T) {
	clean := []string{
		"夕暮れのリンデン村。村長の家と、薄暗い酒場。情報屋カラスがいる。",
		"霧の立ちこめる森の道。崩れかけた橋。ならず者ゴルツと遭遇しうる。",
		"あなたは膝をつき、扉の縁を丹念に指でなぞる。古い鋼線の罠だ。",
		"精霊はついに力尽き、崩れるように青白い光が薄れていく。",
		// 日本語の漢字（簡体字と紛らわしいが正しい日本語）
		"紅い光が溝を照らし、関所の門が見えた。問いを間に挟む。",
	}
	for _, s := range clean {
		if issues := Issues(s); len(issues) != 0 {
			t.Errorf("誤検出: %q → %v", s, issues)
		}
	}
}

// 日本語の漢字（簡体字の対応する正字・創作で使う字）を誤検出しないことを厳密に確認する。
// ここに挙げた字は全て日本語として正しく、issues は空でなければならない。
func TestNoFalsePositiveOnJapaneseKanji(t *testing.T) {
	jp := "" +
		"紅い溝の縁に篝火が揺れ、苔むした祠で歪んだ霊が灯を放つ。" + // 創作頻出
		"聞く 紋章 譲る 議論 設定 訪問 評価 詞 翻訳 該当 詳細 誤り 説明 請求 読書 課題 誰 調査 談話 謀略 諜報 謎 謝罪 謡 謙虚 謹んで " +
		"銭 鐘 鉄 銀 銅 錯誤 鎖 鋒 鏡 鋭利 鍵 錦 " +
		"飯 飲 餓 館 飽き " +
		"閉 闖 悶 閑 聞 閲 闊 闡 " +
		"転 輪 軟 比較 載 輸送 軌道 軒 " +
		"馳 駆 騎 経験 驚 罵 駿馬 " +
		"財 貨 購入 貴重 費用 資源 賞 賤 貧 貪欲 賈 賂 " +
		"約 階級 純粋 線 練習 組織 細 終 紹介 結 継続 緑 編集 繞 縄 緒 束縛"
	if issues := Issues(jp); len(issues) != 0 {
		t.Errorf("日本語漢字を誤検出: %v", issues)
	}
}

// 中国語（簡体字）の混入を検出する。
func TestDetectsSimplified(t *testing.T) {
	dirty := []string{
		"夕暮れ時特有的な静けさが漂う", // 时…ではないが 的 はJP。ここは簡体字無し→別ケース
		"红の光が沟を照らす",      // 红・沟 は簡体字
		"これは个人的な问题です",    // 个・问
	}
	// 红/沟 を含む2件目・3件目は検出されるべき。
	if IsClean("红の光が沟を照らす") {
		t.Error("简体字 红/沟 を検出できていない")
	}
	if IsClean("これは个人的な问题です") {
		t.Error("简体字 个/问 を検出できていない")
	}
	// 部首クラスタで拡充した字（以前は見逃していた）
	if IsClean("古びた纹章を闻いた") { // 纹(紋)・闻(聞)
		t.Error("简体字 纹/闻 を検出できていない")
	}
	_ = dirty
}

// 他言語スクリプトを検出する。
func TestDetectsForeignScripts(t *testing.T) {
	cases := map[string]string{
		"안녕하세요":  "ハングル",
		"Привет": "キリル文字",
		"สวัสดี": "タイ文字",
	}
	for s, want := range cases {
		issues := Issues(s)
		joined := strings.Join(issues, ",")
		if !strings.Contains(joined, want) {
			t.Errorf("%q: %q を検出できていない（%v）", s, want, issues)
		}
	}
}

// ラテン文字の語を検出（短い略語は許容）。
func TestLatinWords(t *testing.T) {
	if IsClean("彼の tone は冷静だった") { // "tone" は4文字→検出
		t.Error("ラテン語 tone を検出できていない")
	}
	if !IsClean("DCは12だ") { // "DC" は2文字→許容
		t.Error("短い略語 DC を誤検出した")
	}
}
