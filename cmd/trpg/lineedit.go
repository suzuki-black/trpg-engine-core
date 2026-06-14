package main

import (
	"bufio"
	"os"
	"strconv"

	"golang.org/x/term"
)

// lineEditor は CJK（全角）の表示幅を自分で計算してカーソルを制御する1行入力エディタ。
// readline 等の幅計算に依存せず、East Asian Width どおり（全角=2セル）に描画する。
// 折り返しによるカーソル不具合を避けるため、入力は常に1行に収め、必要なら横スクロールする。
type lineEditor struct {
	in *bufio.Reader
}

func newLineEditor() *lineEditor {
	return &lineEditor{in: bufio.NewReader(os.Stdin)}
}

// runeWidth は文字の表示セル幅（0/1/2）を返す。
func runeWidth(r rune) int {
	if r == 0 {
		return 0
	}
	if r < 32 || r == 127 {
		return 0 // 制御文字
	}
	if isWide(r) {
		return 2
	}
	return 1
}

func runesWidth(rs []rune) int {
	w := 0
	for _, r := range rs {
		w += runeWidth(r)
	}
	return w
}

// isWide は East Asian Width が Wide/Fullwidth の範囲か（全角=2セル）を判定する。
// 「」（U+300C/300D）やかな・漢字・全角記号を含む。曖昧幅は1セル扱い（iTerm2 既定に一致）。
func isWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F, // ハングル字母
		r == 0x2329 || r == 0x232A,
		r >= 0x2E80 && r <= 0x303E,   // CJK部首〜CJK記号と約物（「」を含む）
		r >= 0x3041 && r <= 0x33FF,   // かな〜CJK互換
		r >= 0x3400 && r <= 0x4DBF,   // CJK拡張A
		r >= 0x4E00 && r <= 0x9FFF,   // CJK統合漢字
		r >= 0xA000 && r <= 0xA4CF,   // 彝（イ）
		r >= 0xAC00 && r <= 0xD7A3,   // ハングル音節
		r >= 0xF900 && r <= 0xFAFF,   // CJK互換漢字
		r >= 0xFE10 && r <= 0xFE19,   // 縦書き約物
		r >= 0xFE30 && r <= 0xFE6F,   // CJK互換形
		r >= 0xFF00 && r <= 0xFF60,   // 全角英数記号
		r >= 0xFFE0 && r <= 0xFFE6,   // 全角記号
		r >= 0x1F300 && r <= 0x1FAFF, // 絵文字
		r >= 0x20000 && r <= 0x3FFFD: // CJK拡張B以降
		return true
	}
	return false
}

// readLine はプロンプトを表示して1行を読む。第2戻り値が false なら入力終了。
// 端末を raw モードにし、矢印キー・バックスペース・Home/End 等を自前で処理する。
func (e *lineEditor) readLine(prompt string) (string, bool) {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return "", false
	}
	defer term.Restore(fd, old)

	promptRunes := []rune(prompt)
	promptW := runesWidth(promptRunes)
	termW := 80
	if w, _, err := term.GetSize(fd); err == nil && w > 8 {
		termW = w
	}

	buf := []rune{}
	cursor := 0 // buf 内のルーン位置
	start := 0  // 表示開始ルーン位置（横スクロール）

	redraw := func() {
		avail := termW - promptW - 1
		if avail < 1 {
			avail = 1
		}
		if cursor < start {
			start = cursor
		}
		for start < cursor && runesWidth(buf[start:cursor]) > avail {
			start++
		}
		// 表示できる範囲を avail セルまで詰める。
		end := start
		w := 0
		for end < len(buf) {
			cw := runeWidth(buf[end])
			if w+cw > avail {
				break
			}
			w += cw
			end++
		}
		cursorCells := promptW + runesWidth(buf[start:cursor])
		out := "\r\x1b[K" + prompt + string(buf[start:end]) + "\r"
		if cursorCells > 0 {
			out += "\x1b[" + strconv.Itoa(cursorCells) + "C"
		}
		os.Stdout.WriteString(out)
	}

	insert := func(r rune) {
		nb := make([]rune, 0, len(buf)+1)
		nb = append(nb, buf[:cursor]...)
		nb = append(nb, r)
		nb = append(nb, buf[cursor:]...)
		buf = nb
		cursor++
	}

	redraw()
	for {
		r, _, err := e.in.ReadRune()
		if err != nil {
			os.Stdout.WriteString("\r\n")
			return "", false
		}
		switch r {
		case '\r', '\n':
			os.Stdout.WriteString("\r\n")
			return string(buf), true
		case 3: // Ctrl-C
			os.Stdout.WriteString("\r\n")
			return "", false
		case 4: // Ctrl-D
			if len(buf) == 0 {
				os.Stdout.WriteString("\r\n")
				return "", false
			}
		case 127, 8: // Backspace
			if cursor > 0 {
				buf = append(buf[:cursor-1], buf[cursor:]...)
				cursor--
			}
		case 1: // Ctrl-A 行頭
			cursor = 0
		case 5: // Ctrl-E 行末
			cursor = len(buf)
		case 2: // Ctrl-B 左
			if cursor > 0 {
				cursor--
			}
		case 6: // Ctrl-F 右
			if cursor < len(buf) {
				cursor++
			}
		case 11: // Ctrl-K 行末まで削除
			buf = buf[:cursor]
		case 21: // Ctrl-U 行頭まで削除
			buf = append([]rune{}, buf[cursor:]...)
			cursor = 0
		case 27: // ESC シーケンス（矢印・Home/End・Delete）
			r2, _, err2 := e.in.ReadRune()
			if err2 != nil {
				break
			}
			if r2 == '[' || r2 == 'O' {
				r3, _, err3 := e.in.ReadRune()
				if err3 != nil {
					break
				}
				switch r3 {
				case 'D': // ←
					if cursor > 0 {
						cursor--
					}
				case 'C': // →
					if cursor < len(buf) {
						cursor++
					}
				case 'H': // Home
					cursor = 0
				case 'F': // End
					cursor = len(buf)
				case '1', '3', '4', '7', '8':
					// 末尾の '~' を読み捨てる
					e.in.ReadRune()
					switch r3 {
					case '1', '7':
						cursor = 0
					case '4', '8':
						cursor = len(buf)
					case '3': // Delete
						if cursor < len(buf) {
							buf = append(buf[:cursor], buf[cursor+1:]...)
						}
					}
				}
			}
		default:
			if r >= 32 {
				insert(r)
			}
		}
		redraw()
	}
}
