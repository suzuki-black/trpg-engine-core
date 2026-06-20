# セッション引き継ぎメモ（堅牢化フェーズ）

最終更新: 2026-06-20 / ブランチ: `main`（origin と同期済み）

## 0. このプロジェクト
Go製・シングルプレイのテキストTRPGエンジン。ローカルLLM（Ollama）がGM/NPCを演じ、
ダイス・フラグ・章進行は Go 側で決定論的に処理する。既定モデルは `qwen2.5:7b`
（手元に `qwen3:4b` / `llama3.2:3b` もあり。`qwen2.5:14b` は**未取得**）。

## 1. いまどこにいるか
「モデル固有の壊れ方を文字列で個別に潰す」やり方をやめ、**モデル非依存の堅牢化**へ移行中。
設計は [`docs/09-robustness.md`](09-robustness.md)。中心は「GM出力が満たすべき不変条件
（I1〜I7）を仕様化し、削るのではなく規約で弾く」。

### 着手順（docs/09 §9.11、合意済み）
1. ✅ **③不変条件バリデータ＋再生成ループ**（完了・コミット `375a014`）
2. ⬜ **②構造スロット試作**（次の作業）：GM地の文を JSON/GBNF でスロット化し、
   セリフ・複数ビート・メタを*デコード時点で*書けなくする。1経路で試作→品質を実測。
3. ⬜ **④複数モデル挙動テスト**：同一入力列を qwen/llama/gemma 等で流し、
   `validateGMBeat` の違反率をモデル別集計（密結合の検出）。`go test` とは分離（オプトイン）。
4. ⬜ **①前段の人格上書き対策**：docs/09 §9.10 の代替1〜3（データ枠＋明示ガード＋
   決定論前処理）で足りるか測り、前段LLM導入の要否を判断。**いきなりフィルタLLMは作らない**。

## 2. アーキテクチャの要点（次の人が踏まえること）
- **権限分離**：進行・判定・クリアは*エンジンの権威*（LLM出力に依存しない）。済み。
- **決定論クリーニングが堅牢化の主役**：`cleanNarration` がセリフ/PC代弁/メタ/未紹介NPC/
  区切り線/手番返しを除去する。**LLMに直させるより、まず確定的に整える**。
- **バリデータは「整えた後」を検査する**：`narrate()` は
  `GM.Narrate → cleanNarration(→firstBeat) → validateGMBeat(cleaned)` の順。
  クリーニングで直せない違反（＝空・言語逸脱＝GMが使える材料を返さなかった時）だけ
  作り直す（最大 `maxBeatRetries=1`）。**rawを検査して良回答を捨てない**のが肝（学習済みの罠）。
- **1ビート制約(I1)は行動描写のみ**：`narrate(..., oneBeat)` の `oneBeat=true` は行動結果、
  `false` は質問・観察への回答（複数文で事実を列挙してよい＝絞ると空になる）。
- **空応答を絶対に出さない**：`actionAck` / `outcomeFallback` / ask用フォールバックで必ず1文返す。

## 3. 主要ファイル / 関数
- [`internal/engine/engine.go`](../internal/engine/engine.go) … 中枢 `Step`。
  - `narrate`（生成＋検査＋再生成ループ）、`validateGMBeat(narr, oneBeat)`（I1〜I6）、
    `cleanNarration`（決定論クリーニング）、`firstBeat`/`capSentences`/`countSentences`、
    `isSeparatorLine`/`isMetaAck`/`isHandback`/`actionAck`、
    `visibleEntities`/`hiddenCharacterNames`/`revealEntities`（未紹介NPCのゲート）、
    `chapterCleared`/`willSetClear`/`advancesGoal`（内容に紐付いた章クリア）。
- [`internal/gm/gm.go`](../internal/gm/gm.go) … `BuildContext`（GMへの文脈組み立て）、
  `Narrate`（textqc内蔵）、`sanitizeGM`。GM規範は [`principles.md`](../internal/gm/principles.md)（go:embed）。
- [`internal/npc/npc.go`](../internal/npc/npc.go) … `Speak`（セリフ＋トーン、会話履歴渡し）。
- [`internal/llm/client.go`](../internal/llm/client.go) … Ollama薄クライアント。`HasModel`（起動時にモデル有無確認）。
- [`internal/scenario/`](../internal/scenario/) … シナリオ（JSON）。Chapter に `clear_requires` あり。
  既定 `scenarios/forgotten-shrine.json`（埋め込み）、SF版 `scenarios/orbital-amatsu.json`。
- 設計: [`docs/09-robustness.md`](09-robustness.md)（堅牢化）、[`docs/08-multi-persona.md`](08-multi-persona.md)（人格）。

## 4. 実行・テスト
```bash
cd ~/work/trpg-engine-core
go build ./... && go vet ./... && go test ./...     # 全テスト緑が前提
go run ./cmd/trpg -model qwen2.5:7b                  # 既定シナリオ（祠）
go run ./cmd/trpg -scenario scenarios/orbital-amatsu.json -model qwen2.5:7b  # SF版
go run ./cmd/trpg -mock                              # LLMなしのモック（高速・決定論テスト用）
ollama list                                          # 取得済みモデル確認
```

## 5. この数セッションで直したこと（参考）
- 未取得モデル指定の無言失敗 → 警告＋mockフォールバック（`HasModel`）。
- GMのPC代弁／未紹介NPC捏造（ミレーユ問題）／独走（複数ビート）／メタ混入（`---`/`[…]`/相づち）。
- 空応答で無言再プロンプト → 必ず1文返す。
- ダイス目だけの章クリア → 会話内容に紐付け＋`clear_requires`。
- NPC位置がプレイヤーに伝わらない → `scene_summary` に位置を明記。
- 不変条件バリデータ＋再生成ループ（③）。

## 6. 既知の注意点
- 7B は指示追従が弱く、決定論フィルタが受け皿。描写の*豊かさ*はモデル地力依存（規約は「壊れない」だけ保証）。
- レイテンシ：1ターンで 分類＋NPC＋GM（＋違反時の再生成）を叩く。重い手段を足す時は体感に注意。
- コミットは `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` を必ず付ける（ユーザー要望）。
- 直近コミット: `375a014`（③バリデータ）, `6095bd3`（docs/09）, `279d60a`/`fe23650`/`f7712db`（各種修正）。
