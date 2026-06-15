# 6. 技術スタック

## 6.1 結論

本プロジェクトは **Go 集約** を採用する。

このプロジェクトは、LLM側の賢さよりも「**章遷移・フラグ管理・判定・NPC態度の整合性**」といった**エンジン側の堅牢さ**が中心になる。LLM連携も薄く、RAGや埋め込みを使う予定もない。
よって、**型安全で壊れにくく、単一バイナリで配布できる Go 集約が最も自然で後悔しない選択**である。

将来 Python が必要になった場合は、**Python サイドカーを HTTP で足すだけ**で対応できる（中核には手を入れない）。

## 6.2 採用理由

| 観点 | Go集約が適合する理由 |
| --- | --- |
| 整合性が主役 | フラグ・章遷移・outcome を型で固め、状態の破綻を防げる |
| LLM連携が薄い | Ollama を HTTP で叩き、生成テキストを受けるだけで足りる（[02](02-gm-prompt.md)・[03](03-npc-templates.md) でLLMの役割は限定済み） |
| 判定エンジンと同一言語 | [04-d20-rules.md](04-d20-rules.md) の判定エンジンが Go 前提。中核も Go にすることで一本化できる |
| 配布 | 単一バイナリで配れる。依存は最小限 |
| 設計思想との一致 | 「決定論はエンジン、創造はLLM」という方針（[01-architecture.md](01-architecture.md)）に素直に乗る |

> 補足（実装後）: 当初は「依存ゼロ」を想定したが、端末の raw モード制御に **`golang.org/x/term`** を採用した。
> 行編集は全角（CJK）幅を正しく扱う自前の小型エディタ（[`cmd/trpg/lineedit.go`](../cmd/trpg/lineedit.go)）。純Go・単一バイナリは維持。詳細は [07-implementation-status.md](07-implementation-status.md)。

> RAG・埋め込み・モデル切替などLLM側の作り込みは現時点で計画にないため、Python の強みは活きない。必要になった時点でサイドカー方式で後付けする。

## 6.3 スタック構成

| レイヤ | 採用技術 | 役割 |
| --- | --- | --- |
| エンジン中核 | **Go** | セッション管理・シナリオ管理・状態管理（[01-architecture.md](01-architecture.md)） |
| 判定エンジン | **Go** | D20 Resolver。JSON I/O（CheckRequest → outcome、[04-d20-rules.md](04-d20-rules.md)） |
| LLM ランタイム | **Ollama**（ローカル, HTTP API） | GM LLM / NPC LLM の呼び出し |
| 状態保持 | **SQLite または JSON ファイル** | セッション状態（[00-overview.md](00-overview.md) §4 の保持項目） |
| UI（入出力） | **CLI（ターミナル）**、将来 TUI / Web | テキストベースの入出力 |

## 6.4 全体像

```
[CLI/TUI]  ←→  [エンジン中核: Go]
                  ├─ セッション管理 / シナリオ管理 / 状態管理
                  ├─→ [判定エンジン: Go]      … D20, JSON I/O
                  └─→ [Ollama HTTP API]       … GM LLM / NPC LLM
                          ↑ プロンプト: 02-gm-prompt.md / 03-npc-templates.md
[状態保存: SQLite or JSON]
```

## 6.5 将来の拡張方針

- **LLMを賢くしたくなった場合**（NPCの記憶・RAG・埋め込み・モデル切替など）：
  Python製の**LLMサイドカー**を立て、エンジン中核から**HTTP経由で呼び出す**。中核（Go）のロジックは変更しない。
- **UIをリッチにしたい場合**：
  Go の `net/http` ＋ 軽量フロント、または TUI ライブラリ（bubbletea 等）へ段階的に拡張する。

この方針により、初期はシンプルな Go 単一構成で始めつつ、必要に応じて疎結合に機能を足せる。
