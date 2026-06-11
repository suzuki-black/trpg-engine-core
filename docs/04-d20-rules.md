# 4. D20判定ルールのJSON設計

## 4.1 目的

行動判定を**外部ロジック**（例：Go）で扱えるようにするため、D20ベースの判定ルールをJSON形式で表現する設計を作る。

- 行動種別ごとに、使用するステータスと難易度を定義できる。
- D20の出目 ＋ ステータス修正値 を、難易度（DC）と比較して成否を決定する。
- クリティカル成功 / クリティカル失敗の扱いも含める。

> 本章はあくまで「設計」である。JSONそのものは実装側が書く前提で、**どの項目を持たせるべきか／どういう値を入れるか**を整理する。

---

## 4.2 判定の基本式

```
判定値 = D20の出目 + 使用ステータスの修正値
成功条件 = 判定値 >= difficulty_class（DC）
```

- 出目が `critical_success_threshold`（例：20）に達した場合は、DCに関わらず**クリティカル成功**。
- 出目が `critical_failure_threshold`（例：1）以下の場合は、判定値に関わらず**クリティカル失敗**。

---

## 4.3 JSONで表現すべき項目の一覧

判定リクエスト1件は、以下の項目で表現する。

| 項目 | 型 | 内容 | 例 |
| --- | --- | --- | --- |
| action_type | 文字列 | 行動種別 | "attack", "search", "talk" |
| used_stat | 文字列 | 使用ステータス | "attack", "luck", "life" |
| difficulty_class | 文字列 または 数値 | 難易度（ランク名 or DC数値） | "hard" または 15 |
| critical_success_threshold | 数値 | クリティカル成功となる出目の下限 | 20 |
| critical_failure_threshold | 数値 | クリティカル失敗となる出目の上限 | 1 |

補足として、ルール定義側（システム設定）に以下を持たせると拡張しやすい。

| 項目 | 内容 |
| --- | --- |
| stat_modifier_rule | ステータス値 → 修正値への変換ルール |
| difficulty_table | 難易度ランク → DC数値 のマッピング表 |

### 判定リクエストのJSONイメージ

```json
{
  "action_type": "search",
  "used_stat": "luck",
  "difficulty_class": "hard",
  "critical_success_threshold": 20,
  "critical_failure_threshold": 1
}
```

### 判定結果のJSONイメージ（判定エンジンの戻り値）

```json
{
  "roll": 14,
  "stat_modifier": 2,
  "total": 16,
  "dc": 15,
  "outcome": "success"
}
```

`outcome` は `critical_success` / `success` / `failure` / `critical_failure` のいずれか。LLM（GM）にはこの `outcome` を中心に渡す。

---

## 4.4 難易度ランクの設計

難易度はランク名で指定し、内部でDC数値にマッピングする方針とする。
ランクで扱うことで、シナリオ作成者は数値を意識せず難易度を設定でき、バランス調整は `difficulty_table` の一括変更で行える。

| ランク | 意味 | DC（目安） |
| --- | --- | --- |
| easy | 容易。少し気をつければ成功する | 8 |
| normal | 普通。標準的な挑戦 | 12 |
| hard | 困難。熟練や幸運が要る | 15 |
| very_hard | 非常に困難。条件が揃わないと厳しい | 18 |

> DC数値はあくまで初期目安。プレイテストの結果に応じて `difficulty_table` を調整する。
> `difficulty_class` には、ランク名（"hard"）だけでなく、特殊な場面のために生のDC数値（例：17）も指定できるようにしておくと柔軟。

### ステータス修正値の方針（例）

ステータス値から修正値を導く変換ルールを別途定義する。一例として：

```
修正値 = floor((ステータス値 - 10) / 2)
```

（例：attack=14 → 修正値 +2／luck=8 → 修正値 -1）
この変換ルール自体も `stat_modifier_rule` として設定で持たせ、システム全体で統一する。

---

## 4.5 具体的なルール例

### 例1：攻撃行動

```json
{
  "action_type": "attack",
  "used_stat": "attack",
  "difficulty_class": "normal",
  "critical_success_threshold": 20,
  "critical_failure_threshold": 1
}
```

- 使用ステータス：attack
- 難易度：normal（DC 12目安）
- 出目20でクリティカル成功（大ダメージ等）、出目1でクリティカル失敗（隙を晒す等）。

### 例2：罠の探索

```json
{
  "action_type": "search",
  "used_stat": "luck",
  "difficulty_class": "hard",
  "critical_success_threshold": 20,
  "critical_failure_threshold": 1
}
```

- 使用ステータス：luck
- 難易度：hard（DC 15目安）
- クリティカル成功で罠の構造まで把握、クリティカル失敗で罠を作動させてしまう、など。

### 例3：交渉（会話）

```json
{
  "action_type": "talk",
  "used_stat": "luck",
  "difficulty_class": "normal",
  "critical_success_threshold": 20,
  "critical_failure_threshold": 1
}
```

- NPCとの交渉・説得に用いる。
- 結果に応じてNPCの開示情報量・態度が変わる（[03-npc-templates.md](03-npc-templates.md) の応答方針と連動）。

---

## 4.6 設計上の注意点

- 判定エンジンは**決定論的に**実装し、乱数（D20）と修正値の合算・閾値比較のみを担当する。物語的解釈はLLMに任せる。
- `outcome` の4段階（critical_success / success / failure / critical_failure）を共通言語とし、GMプロンプトの「判定結果」入力と一致させる。
- クリティカルの閾値（20 / 1）はルール全体で共通化し、リクエストごとに変えないのが基本。特殊ルールが必要な場面のみ上書きを許容する。
- 行動種別（action_type）と使用ステータス（used_stat）の対応は、シナリオ設計時に表として整理しておくと運用しやすい。
