# 7. 実装状況と、設計書との差分

`docs/00`〜`06` は最初に書いた**設計資料**です。その後の実装・試遊で方針が進化した箇所があります。
このドキュメントは「**いま何がどこに実装されているか**」と「**設計書からの主な差分**」を一覧にして、
コードとドキュメントの食い違いで混乱しないようにするためのものです。

## 7.1 コンポーネント → 実装パッケージ

| 設計上の役割 | 実装 |
| --- | --- |
| セッション管理（司令塔・1ターン進行） | [`internal/engine`](../internal/engine/engine.go) |
| プレイヤー状態管理 | [`internal/state`](../internal/state/state.go) |
| GMモジュール | [`internal/gm`](../internal/gm/gm.go)（人格＝[`internal/gm/principles.md`](../internal/gm/principles.md)） |
| NPCモジュール | [`internal/npc`](../internal/npc/npc.go) |
| 判定エンジン（D20） | [`internal/dice`](../internal/dice/resolver.go) |
| シナリオ管理 | [`internal/scenario`](../internal/scenario/scenario.go)（JSON。既定は埋め込み、外部は `scenarios/`） |
| 行動分類（意図解釈） | [`internal/intent`](../internal/intent/intent.go)（キーワード＋LLMハイブリッド） |
| 日本語以外の混入フィルタ | [`internal/textqc`](../internal/textqc/textqc.go) |
| LLMランタイム | [`internal/llm`](../internal/llm/client.go)（Ollama＋offline mock） |
| 永続化（セーブ/ロード） | [`internal/persist`](../internal/persist/persist.go) |
| CLI・行編集 | [`cmd/trpg`](../cmd/trpg/main.go)・[`lineedit.go`](../cmd/trpg/lineedit.go) |

## 7.2 実装済みの主な機能

- **D20判定エンジン**：難易度マッピング・クリティカル境界（単体テスト済み）。
- **データ駆動シナリオ**：章・NPC・進行ルール・ボーナス・ボス・エンディング・開始状態を JSON 化。
  `-scenario` で外部読み込み。既定「忘れられた祠の灯」は埋め込み、SF「軌道ステーション・アマツ」は `scenarios/` に同梱。
- **GM（リプレイ司会役）**：[`principles.md`](../internal/gm/principles.md) を毎ターン注入。役割分離・1ターン1ビート・宣言を繰り返さない・NPCは「」＋話者名。
- **意図分類**：漢字はキーワード即時、かな書き・口語は LLM 補助。雑談・突っ込みは `ooc`（判定なし）。
- **日本語フィルタ**：簡体字・他言語・ラテン語片を検出し日本語で再生成（`-qc-retries`）。
- **マルチセーブ＆オートセーブ**：`saves/` に名前付きスロット、章進行で自動保存。
- **行編集**：全角（CJK）幅を正しく扱う自前エディタ（`lineedit.go`）。raw モードは `golang.org/x/term`。
- **透明性**：行動の解釈・出目を表示。進展しない時はヒントと目標を提示。乱数モード（固定/ランダム）を起動時表示。

## 7.3 設計書からの主な差分（重要）

- **GMの出力**：設計書（`00`/`01`/`02`）は「行動候補メニューを2〜4個提示」とあるが、**現在は出さない**。
  メニューが“コマンド選択式AVG”感を生んだため、リプレイ風に開いた問いかけ（「○○、どうする？」）で締める方式へ変更。
- **GMプロンプト**：`docs/02-gm-prompt.md` の本文ではなく、[`internal/gm/principles.md`](../internal/gm/principles.md) が**実装上の正**。
- **シナリオ定義**：設計段階は Go の概念モデルだったが、**JSON で外部化**（`docs/00`§7・`docs/05` の章/分岐をデータ化）。
- **依存関係**：`docs/06` は「依存を持ち込まない」とあるが、現在は **`golang.org/x/term`（端末 raw モード）** に依存。
  行編集は自前実装（CJK幅対応）。純Go・単一バイナリは維持。

## 7.4 「シーンを息させる」拡張（実装済み・検証中）

試遊で判明した最大の構造的課題＝各章が「1回判定成功＝即・次章」の単一ゲートで、
**シーンに留まってロールプレイする余地が無い**問題。これに対して以下を実装した:

- **シーン情報**：章に `entities`（登場物）・`layout`（位置関係）・`facts`（事実、開示条件つき）を持てる。
  事実の開示条件 `reveal` は `always / ask / flag:X / search / talk`。
  **今開示してよい事実だけ**を GM に渡す（隠し事実は渡さない＝秘密漏洩を構造で防止）。
- **`ask`（質問・観察）**：見れば分かる情報の確認は**判定なし・進行なし**で、シーン情報から回答。
  `search` は「隠れた物を探す＝判定あり」と区別（成功すると `reveal:search` の隠し事実が開く）。
- **シーンを息させる**：目標達成（clear_flag）でも即進行せず「先へ進める」状態にし、
  プレイヤーが **「次へ」** と入力した時だけ次の場面へ進む（最終章は達成で即エンディング）。
- 例として `scenarios/orbital-amatsu.json` の第1章にシーン情報を付与済み（手書きの見本）。

実装: [`internal/scenario`](../internal/scenario/scenario.go)（スキーマ）・[`internal/engine`](../internal/engine/engine.go)（ask・開示・進行）・
[`internal/gm/principles.md`](../internal/gm/principles.md)（質問への回答・急かさない）。

## 7.5 次の一手（未着手）

- **全章へのシーン情報付与**：今は見本1章のみ。各章に登場物・事実を整備する。
- **判定基準の徹底**：「不確実で失敗が意味を持つ時だけ振る」をさらに磨く。
- **対話型シナリオ生成器**：上記スキーマを出力先に、生成器が事実リストを人間に質問しながら作る（「自動」指定可）。
- **AI仲間プレイヤー（Phase 2）**：卓に他PCを座らせ、自分の番で動き・茶々を入れる（和気藹々の本命）。
