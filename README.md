# 🎲 trpg-engine-core

> A single-player text TRPG engine where the **Game Master and NPCs are played by an LLM**, while dice, flags, and story progression are kept deterministic in type-safe Go.

**Design philosophy: _the LLM creates, the engine decides._**
Narration and roleplay are delegated to a local LLM (via [Ollama](https://ollama.com/)); D20 resolution, difficulty, flags, and chapter progression are owned by Go and never left to the model. This keeps the story free-form **and** the rules consistent.

It runs out of the box — **no API keys, no cloud, dependency-light** (Go standard library plus `golang.org/x/term` for raw-mode terminal access; line editing is a small custom CJK-aware editor). If Ollama isn't running, it falls back to an offline mock so you can still see the machinery work.

> ⚠️ **Prototype.** This is an early proof-of-concept, published for feedback and experimentation.

---

## ✨ Features

- 🧠 **AI Game Master** — improvises scene description and choices, constrained by the current chapter and a strict do-not list (can't overturn dice, leak un-unlocked lore, or kill NPCs at will).
- 🗣️ **Natural-language input with feedback** — actions are classified by keyword first, then by the LLM for kana/casual phrasing. Every turn shows how your action was read, and if nothing happened, it tells you why and what the current objective is.
- 🔎 **Scenes you can explore** — ask/look around for free (no roll) and the GM answers from the scene's facts; secrets stay hidden until you investigate (a successful search/talk) or a flag unlocks them. Meeting a chapter's goal opens the way forward but doesn't yank you onward — linger and role-play, then say "次へ" to proceed.
- 🎭 **AI NPCs with persistent personalities** — each NPC has a personality, tone, public goal, hidden secret, and an attitude that shifts one step at a time. NPCs return *dialogue + tone only*; the GM weaves it into the prose.
- 🎲 **Deterministic D20 engine** — `roll + stat modifier vs DC`, with critical success/failure at 20/1. Fully unit-tested.
- 📖 **Data-driven, branching scenarios** — chapters, NPCs, progression rules, bonuses, boss, and endings are defined in JSON (no recompile). The engine is scenario-agnostic; load your own with `-scenario`. Fixed skeleton, improvised details; multiple routes (fight vs. negotiate) lead to 3 endings.
- ⚔️ **Boss combat** — HP-based multi-round attack rolls, with bonuses wired to earlier choices (clues found / allies recruited make negotiation easier).
- 💾 **Save & load** — full session state (player, NPC attitudes, chapter, flags, world, boss HP) to JSON, with named multi-slot saves and autosave on chapter transitions.
- 🪶 **Dependency-light** — pure Go, a single static binary; the only direct dependency is `golang.org/x/term` (raw mode). The line editor is a small built-in, with correct East Asian (full-width) cursor handling.

## 🚀 Quick Start

Requires [Go](https://go.dev/dl/) 1.21+. Optional: [Ollama](https://ollama.com/) with a Japanese-capable model.

```bash
# Clone
git clone git@github.com:suzuki-black/trpg-engine-core.git
cd trpg-engine-core

# Pull a model (recommended for Japanese)
ollama pull qwen2.5:7b

# Play (auto-uses Ollama if running, else offline mock)
go run ./cmd/trpg

# Scripted demo straight to the best ending
go run ./cmd/trpg -model qwen2.5:7b -seed 4 -demo

# No LLM? See the engine skeleton with the offline mock
go run ./cmd/trpg -mock -seed 4 -demo

# Play a different scenario — a sci-fi one ships as an example
go run ./cmd/trpg -scenario scenarios/orbital-amatsu.json
# (write your own using internal/scenario/scenarios/forgotten-shrine.json as a template)
```

Type actions in Japanese. **Asking/looking** ("周りを見る", "何がある?") is free (no roll) and answers from the scene's known facts; the scene **doesn't auto-advance** — say "次へ" to move on when you're ready. In-session commands: `status` / `saves` / `save [name]` / `load [name]` / `quit`. Saves live in `saves/`; chapters autosave to the `autosave` slot.

## 🏗️ Architecture

`Player input → classify action → D20 resolver → GM narration (+NPC) → update flags/state → advance chapter`

| Component | Package | Responsibility |
| --- | --- | --- |
| Session Manager (hub) | [`internal/engine`](internal/engine/engine.go) | Orchestrates a turn; classifies actions; advances chapters |
| Player State | [`internal/state`](internal/state/state.go) | Stats, inventory, NPC attitudes, world, boss HP |
| GM Module | [`internal/gm`](internal/gm/gm.go) | Builds the GM prompt; generates narration |
| NPC Module | [`internal/npc`](internal/npc/npc.go) | Expands persona templates; returns *dialogue + tone* |
| D20 Resolver | [`internal/dice`](internal/dice/resolver.go) | Deterministic dice, DC mapping, criticals |
| Scenario Manager | [`internal/scenario`](internal/scenario/scenario.go) | Chapters, clear conditions, NPC templates |
| LLM Runtime | [`internal/llm`](internal/llm/client.go) | Ollama HTTP client + offline mock |
| Persistence | [`internal/persist`](internal/persist/persist.go) | JSON save / load |
| CLI | [`cmd/trpg`](cmd/trpg/main.go) | Terminal UI |

Full design notes (concept → mechanics → tech choice) live in [`docs/`](docs/) (`00`–`06`); current implementation status and where it diverges from the original design is in [`docs/07-implementation-status.md`](docs/07-implementation-status.md).

## 🧪 Tests

```bash
go test ./...
```

Covers D20 critical boundaries & difficulty mapping, the boss-combat loop through defeat, route-difficulty easing, and save/load round-trips.

## 🗺️ Roadmap (TODO)

- [x] **Non-Japanese output filter** — detects simplified-Chinese / foreign scripts / stray Latin words in GM & NPC output and regenerates in Japanese ([`internal/textqc`](internal/textqc/textqc.go), tunable via `-qc-retries`).
- [x] **Multi-slot saves & autosave** — named save slots under `saves/`, listed by recency; autosaves on every chapter transition.
- [x] **Externalized scenarios** — chapters, NPCs, progression rules, bonuses, boss, and endings live in JSON; the engine is scenario-agnostic and loads files via `-scenario` ([`internal/scenario`](internal/scenario/scenario.go), default embedded).
- [x] **Scenes that breathe** — free ask/look from scene facts (no roll), secrets gated behind investigation, and advance-on-your-choice ("次へ") instead of auto-jumping on the first success.

Planned but **not yet implemented**:

- [ ] **Rich scene info for every chapter** — only one chapter is fleshed out so far.
- [ ] **Interactive scenario generator** — interviews the author for the fact list, auto-fills the rest.
- [ ] **AI co-players** — party members who act and banter on their own turns.
- [ ] **Tactical combat** — defend, item use, and NPC assists as combat options.

## 📄 License

[MIT](LICENSE) © 2026 suzuki-black.
The bundled scenario *“The Light of the Forgotten Shrine”* and its NPCs are original content. The `d20 + modifier vs DC` resolution is a generic game mechanic; no trademarked game system or rules text is used.

---

# 🎲 trpg-engine-core（日本語）

> **GM と NPC を LLM が演じる**、ソロプレイ用テキストTRPGエンジン。サイコロ判定・フラグ・物語進行は、型安全な Go の決定論ロジックが担当します。

**設計思想：_LLM は創造、エンジンは決定論。_**
描写とロールプレイはローカル LLM（[Ollama](https://ollama.com/)）に委ね、D20 判定・難易度・フラグ・章進行は Go が管理してモデルに渡しません。これにより、物語の自由度と**ルールの一貫性**を両立します。

依存は最小限（Go 標準ライブラリ＋端末 raw モード用の `golang.org/x/term` のみ。行編集は全角幅を正しく扱う自前の小型エディタ）で、**APIキー不要・クラウド不要**。Ollama が無い場合はオフラインの mock にフォールバックし、エンジンの動作だけでも確認できます。

> ⚠️ **試作品（プロトタイプ）です。** フィードバックと実験のために公開しています。

---

## ✨ 特徴

- 🧠 **AI ゲームマスター** — 現在の章と厳格な禁止事項（判定の上書き・未解放情報の漏洩・NPCの勝手な殺害の禁止）に縛られつつ、情景と選択肢を即興生成。
- 🗣️ **自然言語入力＋フィードバック** — 行動はまずキーワードで、漏れたら（ひらがな・口語）LLMで意図分類。毎ターン「どう解釈したか」を表示し、何も起きなかった時は理由と今の目標を伝える。
- 🔎 **探索できるシーン** — 観察・質問は判定なしで、GMがシーンの事実から回答。隠し情報は探索/会話の成功やフラグで初めて開く。章の目標を達成しても勝手に進まず、留まってロールしてから「次へ」で進める。
- 🎭 **人格が持続する AI NPC** — 性格・口調・表向きの目的・隠された秘密を持ち、態度は1段階ずつ変化。NPCは*セリフ＋トーンのみ*を返し、GMが地の文へ統合。
- 🎲 **決定論的な D20 エンジン** — `出目＋ステータス修正 vs DC`、20/1でクリティカル成功/失敗。単体テスト済み。
- 📖 **データ駆動の分岐シナリオ** — 章・NPC・進行ルール・ボーナス・ボス・エンディングを JSON で定義（再コンパイル不要）。エンジンはシナリオ非依存で、`-scenario` で自作も読み込み可能。骨格は固定・細部は即興、戦闘/交渉で3通りの結末へ。
- ⚔️ **ボス戦** — HP制の複数回 attack 判定。手がかり収集・仲間加入などの過去の選択が交渉を有利にする補正を実装。
- 💾 **セーブ＆ロード** — セッション全状態（プレイヤー/NPC態度/章/フラグ/世界/ボスHP）を JSON で保存。
- 🪶 **依存は最小限** — ほぼ Go 標準ライブラリ。単一バイナリ。直接依存は `golang.org/x/term`（raw モード）のみ。行編集は全角カーソルを正しく扱う自前の小型エディタ。

## 🚀 クイックスタート

[Go](https://go.dev/dl/) 1.21+ が必要。任意で日本語対応モデルを入れた [Ollama](https://ollama.com/)。

```bash
git clone git@github.com:suzuki-black/trpg-engine-core.git
cd trpg-engine-core

# モデル取得（日本語推奨）
ollama pull qwen2.5:7b

# プレイ（Ollama 起動中なら自動利用、無ければ offline mock）
go run ./cmd/trpg

# 最良エンディングまで自動デモ
go run ./cmd/trpg -model qwen2.5:7b -seed 4 -demo

# LLM 無しで骨格だけ確認
go run ./cmd/trpg -mock -seed 4 -demo

# 別シナリオで遊ぶ（SF版を同梱）
go run ./cmd/trpg -scenario scenarios/orbital-amatsu.json
# （雛形は internal/scenario/scenarios/forgotten-shrine.json を参照）
```

行動は日本語で入力。**観察・質問**（「周りを見る」「何がある?」）は判定なしで、シーンの既知の事実から答えます。目標を達成しても**自動では進まず**、納得したら「次へ」で次の場面へ。コマンド: `status` / `saves`（一覧） / `save [名前]` / `load [名前]` / `quit`。セーブは `saves/` に保存され、章進行ごとに `autosave` スロットへ自動保存されます。

## 🏗️ アーキテクチャ

`プレイヤー入力 → 行動分類 → D20判定 → GM描写(＋NPC) → フラグ/状態更新 → 章進行`

| コンポーネント | パッケージ | 責務 |
| --- | --- | --- |
| セッション管理（司令塔） | [`internal/engine`](internal/engine/engine.go) | 1ターンの統括・行動分類・章進行 |
| プレイヤー状態管理 | [`internal/state`](internal/state/state.go) | ステータス/所持品/NPC態度/世界/ボスHP |
| GMモジュール | [`internal/gm`](internal/gm/gm.go) | GMプロンプト組み立て・描写生成 |
| NPCモジュール | [`internal/npc`](internal/npc/npc.go) | 人格テンプレート展開・*セリフ＋トーン*返却 |
| D20判定エンジン | [`internal/dice`](internal/dice/resolver.go) | 決定論的サイコロ・DCマッピング・クリティカル |
| シナリオ管理 | [`internal/scenario`](internal/scenario/scenario.go) | 章・クリア条件・NPCテンプレート |
| LLMランタイム | [`internal/llm`](internal/llm/client.go) | Ollama HTTPクライアント＋offline mock |
| 永続化 | [`internal/persist`](internal/persist/persist.go) | JSON セーブ/ロード |
| CLI | [`cmd/trpg`](cmd/trpg/main.go) | ターミナルUI |

設計書（コンセプト→機構→技術選定）は [`docs/`](docs/)（`00`〜`06`）に収録。**現在の実装状況と設計書との差分**は [`docs/07-implementation-status.md`](docs/07-implementation-status.md) を参照。

## 🧪 テスト

```bash
go test ./...
```

D20のクリティカル境界・難易度マッピング、ボス戦の撃破までのループ、ルート難易度緩和、セーブ/ロード往復を検証します。

## 🗺️ ロードマップ（TODO）

- [x] **非日本語フィルタ** — GM/NPC出力の簡体字・他言語スクリプト・ラテン語片を検知し、日本語で再生成（[`internal/textqc`](internal/textqc/textqc.go)、`-qc-retries` で調整可）。
- [x] **マルチセーブ＆オートセーブ** — `saves/` に名前付きスロットで保存し新しい順に一覧表示。章進行ごとに自動保存。
- [x] **シナリオの外部化** — 章・NPC・進行ルール・ボーナス・ボス・エンディングを JSON 化。エンジンはシナリオ非依存で `-scenario` で読み込み（[`internal/scenario`](internal/scenario/scenario.go)、既定は埋め込み）。
- [x] **シーンを息させる** — 観察・質問はシーン情報から判定なしで回答、隠し情報は探索で開放、目標達成しても自動で進まず「次へ」で進行。

予定はありますが**未実装**：

- [ ] **全章へのシーン情報付与** — 今は見本1章のみ。
- [ ] **対話型シナリオ生成器** — 事実リストを作者に質問し、他は自動生成。
- [ ] **AI仲間プレイヤー** — 自分の番で動き・茶々を入れるパーティ。
- [ ] **戦闘の戦術化** — 防御・アイテム使用・NPC加勢を選択肢に追加。

## 📄 ライセンス

[MIT](LICENSE) © 2026 suzuki-black。
同梱シナリオ「忘れられた祠の灯」とNPCはオリジナルです。`d20＋修正値 vs DC` は一般的なゲーム機構で、商標化されたゲームシステムやルール本文は使用していません。
