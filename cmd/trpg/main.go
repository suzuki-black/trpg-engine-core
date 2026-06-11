// Command trpg は TRPGエンジンの CLI。
// 設計: docs/06-tech-stack.md（UI=CLI）, docs/01-architecture.md（全体フロー）
//
// 使い方:
//
//	go run ./cmd/trpg                 # Ollama があれば自動利用、無ければ mock
//	go run ./cmd/trpg -mock           # 強制的にオフライン mock
//	go run ./cmd/trpg -model qwen2.5:7b
//	go run ./cmd/trpg -seed 42        # 判定乱数を固定（再現用）
//	go run ./cmd/trpg -demo           # 自動デモ（既定のコマンド列を流す）
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"trpg-engine-core/internal/engine"
	"trpg-engine-core/internal/gm"
	"trpg-engine-core/internal/llm"
	"trpg-engine-core/internal/npc"
	"trpg-engine-core/internal/persist"
	"trpg-engine-core/internal/scenario"
	"trpg-engine-core/internal/state"
)

const defaultSavePath = "savegame.json"

func main() {
	var (
		endpoint = flag.String("endpoint", "http://localhost:11434", "Ollama endpoint")
		model    = flag.String("model", "qwen2.5:7b", "Ollama model")
		mock     = flag.Bool("mock", false, "force offline mock LLM")
		seed     = flag.Int64("seed", 0, "RNG seed (0 = time-based)")
		demo     = flag.Bool("demo", false, "run scripted demo turns")
		loadPath = flag.String("load", "", "起動時にセーブファイルから再開する")
	)
	flag.Parse()

	// --- LLM クライアント選択（Ollama 優先、無ければ mock） ---
	var client llm.Client
	if *mock {
		client = llm.NewMock()
	} else {
		o := llm.NewOllama(*endpoint, *model)
		if o.Available(context.Background()) {
			client = o
		} else {
			fmt.Println("（Ollama に接続できないため offline mock で起動します）")
			client = llm.NewMock()
		}
	}

	// --- RNG（判定エンジン用） ---
	s := *seed
	if s == 0 {
		s = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(s))

	// --- 構成（Go集約: すべて同一プロセス内） ---
	scn := scenario.ForgottenShrine()
	sess := state.NewSession()
	sess.Player = state.PlayerCharacter{
		Name:      "アルド",
		Class:     "戦士",
		Stats:     map[string]int{"luck": 12, "attack": 14, "life": 20},
		Inventory: []string{"たいまつ", "ロープ", "回復薬"},
	}
	sess.ChapterID = scn.Chapters[0].ID
	sess.SceneSummary = scn.Chapters[0].SceneSummary
	sess.World = state.World{TimeOfDay: "夕", Weather: "曇り", Alertness: "低", Ambient: "酒場のざわめき、薪の匂い"}

	eng := engine.New(scn, sess, rng, gm.New(client), npc.New(client))
	eng.InitNPCs()

	// -load 指定時はセーブから再開。
	if *loadPath != "" {
		loaded, err := persist.Load(*loadPath)
		if err != nil {
			fmt.Printf("ロード失敗: %v\n", err)
		} else {
			eng.LoadSession(loaded)
			sess = loaded
			fmt.Printf("（%s から再開しました）\n", *loadPath)
		}
	}

	printIntro(scn, sess, client)

	ctx := context.Background()
	if *demo {
		runDemo(ctx, eng, scn, sess)
		return
	}
	runREPL(ctx, eng, scn, sess)
}

func printIntro(scn *scenario.Scenario, sess *state.Session, client llm.Client) {
	fmt.Println("════════════════════════════════════════════")
	fmt.Printf("  TRPG: %s\n", scn.Title)
	fmt.Println("════════════════════════════════════════════")
	fmt.Printf("世界観: %s\n\n", scn.World)
	fmt.Printf("あなた: %s（%s）  Luck:%d Attack:%d Life:%d\n",
		sess.Player.Name, sess.Player.Class,
		sess.Player.Stats["luck"], sess.Player.Stats["attack"], sess.Player.Stats["life"])
	fmt.Printf("LLM: %s\n", client.Name())
	fmt.Println("コマンド: 行動を日本語で入力 / 'status' 状態 / 'save [file]' / 'load [file]' / 'quit' 終了")
	fmt.Println("--------------------------------------------")
	ch := scn.Chapter(sess.ChapterID)
	fmt.Printf("\n▼ 第1章「%s」\n%s\n\n", ch.Title, ch.SceneSummary)
}

func runREPL(ctx context.Context, eng *engine.Engine, scn *scenario.Scenario, sess *state.Session) {
	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !sc.Scan() {
			return
		}
		input := strings.TrimSpace(sc.Text())
		if input == "" {
			continue
		}
		switch {
		case input == "quit" || input == "exit":
			fmt.Println("セッションを終了します。")
			return
		case input == "status":
			printStatus(scn, sess)
			continue
		case input == "save" || strings.HasPrefix(input, "save "):
			path := strings.TrimSpace(strings.TrimPrefix(input, "save"))
			if path == "" {
				path = defaultSavePath
			}
			if err := persist.Save(path, eng.Sess); err != nil {
				fmt.Printf("セーブ失敗: %v\n", err)
			} else {
				fmt.Printf("（%s に保存しました）\n", path)
			}
			continue
		case input == "load" || strings.HasPrefix(input, "load "):
			path := strings.TrimSpace(strings.TrimPrefix(input, "load"))
			if path == "" {
				path = defaultSavePath
			}
			loaded, err := persist.Load(path)
			if err != nil {
				fmt.Printf("ロード失敗: %v\n", err)
			} else {
				eng.LoadSession(loaded)
				sess = loaded
				fmt.Printf("（%s から再開しました）\n", path)
				printStatus(scn, sess)
			}
			continue
		}
		if doTurn(ctx, eng, scn, sess, input) {
			return
		}
	}
}

func runDemo(ctx context.Context, eng *engine.Engine, scn *scenario.Scenario, sess *state.Session) {
	script := []string{
		"ミレーユに同行を頼み、一緒に行くことにする",
		"情報屋カラスに遺跡の場所を尋ねる",
		"森の道で、ならず者ゴルツに話しかけて味方に誘う",
		"遺跡の広間で、床の溝や壁の紋章を注意深く調べる",
		"精霊の事情を聞き、穏やかに説得して鎮めようとする",
		"村長に真実をありのまま報告する",
	}
	for _, in := range script {
		fmt.Printf("\n> %s\n", in)
		if doTurn(ctx, eng, scn, sess, in) {
			return
		}
	}
}

// doTurn は 1 ターン実行して表示する。ended なら true。
func doTurn(ctx context.Context, eng *engine.Engine, scn *scenario.Scenario, sess *state.Session, input string) bool {
	res, err := eng.Step(ctx, input)
	if err != nil {
		fmt.Printf("エラー: %v\n", err)
		return false
	}
	if res.Check != nil {
		c := res.Check
		fmt.Printf("\n🎲 [判定] %s  D20=%d %+d = %d  vs DC%d → %s\n",
			c.ActionType, c.Roll, c.StatMod, c.Total, c.DC, c.Outcome.JP())
	}
	if res.Combat != "" {
		fmt.Printf("%s\n", res.Combat)
	}
	for _, raw := range res.NPCRaw {
		fmt.Printf("\n💬 %s\n", indent(raw))
	}
	fmt.Printf("\n📖 %s\n", res.Narration)

	if res.ChapterMoved {
		fmt.Printf("\n──────── 章が進行 ────────\n▼ 第%s章「%s」\n%s\n",
			chapterNo(scn, res.NewChapter.ID), res.NewChapter.Title, res.NewChapter.SceneSummary)
	}
	if res.Ended {
		fmt.Printf("\n════════ エンディング ════════\n%s\n", res.Ending)
		fmt.Println("\n（おしまい）")
		return true
	}
	return false
}

func printStatus(scn *scenario.Scenario, sess *state.Session) {
	ch := scn.Chapter(sess.ChapterID)
	fmt.Printf("── 状態 ──\n章: %s「%s」\n", ch.ID, ch.Title)
	fmt.Printf("Life:%d Luck:%d Attack:%d\n",
		sess.Player.Stats["life"], sess.Player.Stats["luck"], sess.Player.Stats["attack"])
	if sess.Boss.Active {
		fmt.Printf("ボス %s: HP %d/%d\n", sess.Boss.Name, sess.Boss.HP, sess.Boss.MaxHP)
	}
	fmt.Printf("所持品: %s\n", strings.Join(sess.Player.Inventory, ", "))
	var flags []string
	for k, v := range sess.Flags {
		if v {
			flags = append(flags, k)
		}
	}
	fmt.Printf("フラグ: %s\n", strings.Join(flags, ", "))
	for id, n := range sess.NPCs {
		fmt.Printf("NPC %s: 態度=%s\n", scn.NPCs[id].Name, n.Attitude.JP())
	}
}

func chapterNo(scn *scenario.Scenario, id string) string {
	for i := range scn.Chapters {
		if scn.Chapters[i].ID == id {
			return fmt.Sprintf("%d", i+1)
		}
	}
	return "?"
}

func indent(s string) string {
	return strings.ReplaceAll(s, "\n", "\n   ")
}
