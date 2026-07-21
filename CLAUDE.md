# ridge — repo conventions for Claude Code

用語は [glossary.md](glossary.md) が正本（追加・改名はコード変更と同一 PR）。
何のリポジトリかは [README.md](README.md)。

## この repo の立ち位置（最初に読む）

- **ridge は furrow の CLI/JSON クライアント。** furrow の Go パッケージを
  **import しない**。これは好みではなく
  [furrow の non-goals](https://github.com/akira-toriyama/furrow/blob/main/docs/non-goals.md)
  に書かれた建て付けで、GUI 版の [vista](https://github.com/akira-toriyama/vista)
  も同じ契約に従っている。
- **ロジックの正本は furrow 一本。** 「ridge に書くと vista にも同じものが要るか？」
  で迷ったら furrow へ PR。判断規範は furrow ボード `t-ehk7` の
  「仮に TUI を作るならロジックが冗長になるか」。ridge はその規範が予言した
  2人目の消費者であり、**front-end 側に業務ロジックを溜めない**のが本 repo の
  存在意義に直結する。
- **タスク管理は furrow**（`repos: akira-toriyama/ridge`）。GitHub issue は使わない。

## Go

- `go build ./...` / `go test ./...` が通ることを終了前に確認する。
  Go 1.25+ では `GOTOOLCHAIN=local`。
- house style は [go-dev skill] に従う（薄い main + `internal/`、typed exit code、
  stdlib のみのテスト）。**現状は POC 由来で flat な `package main`** なので、
  移行タスクが立っている。新規コードは移行後の形を意識して書く。
- テストは stdlib のみ（testify を入れない）。

## bubbletea v2 の罠（既知・再発見しないこと）

v1 から大きく変わっている。以下は実際に踏んで確認済み:

- **`View()` は `tea.View` 構造体を返す**（v1 は string）。`AltScreen` /
  `MouseMode` / `KeyboardEnhancements` は **毎フレーム宣言し直すフィールド**で、
  `NewProgram` のオプションではない。
- **`case tea.KeyPressMsg:` を使う。** `tea.KeyMsg` は press と release 両方に
  マッチする interface なので、type switch では `KeyPressMsg` より**後ろ**に
  置かないと押下を飲み込む。
- **`key.WithKeys("space")`**、`" "` ではない。**コンパイルエラーにならず黙って
  効かなくなる。**
- **`Shift+Space` は素の端末に届かない。** スペースは**テキストとして**送られ、
  テキストは修飾子を持たない。`View.KeyboardEnhancements` の
  `ReportAllKeysAsEscapeCodes`（Kitty プロトコル）を立てて初めて区別できる。
  非対応端末があるので、**修飾キー付きジェスチャには必ず素キーの別名を用意する**
  （グラフなら `S`）。
- **マウス**: `MouseModeCellMotion`（1002+1006）を使う。`AllMotion`（1003）は
  tmux/mosh でのサポートが悪く、ドラッグには不要。**ボタンは直前の
  `MouseClickMsg` から自分で覚える** — motion イベントのボタンを報告しない端末が
  ある。
- **`bubbles/v2` にマウス対応はほぼ無い**（viewport のホイールのみ）。list/table は
  ゼロ。ドラッグもヒットテストも自前。
- **`lipgloss/v2` のコンポジタを使う**: `Layer` の X/Y/**Z** と
  `Compositor.Hit(x,y)`。ゴースト・オーバーレイ・当たり判定がネイティブ。
  **`bubblezone` は使わない**（作者が v2 コンポジタと併用不可と警告している）。
- `lipgloss` v2 から `AdaptiveColor` が消えた。`DefaultStyles` 系は
  `isDark bool` を取る。

## CJK — 日本語が主体のボードである

- **幅は必ず `lipgloss.Width`（表示幅）で測る。`len()` は禁止。**
  タスクタイトルは日本語で、依存を持つものは中央値82セル・p90 133セル。
  1文字で2セル食うので、`len()` は必ず枠を壊す。
- 切り詰めもバイト単位で切らない（`ansi.Truncate` などを使う）。
- **枠付きの箱を並べるときは必ず複数幅で `-dump` して桁揃えを目視する。**
  ズレは1桁ずつ蓄積するので、狭い画面では気づかず広い画面で露見する。

## 描画とレイアウト

- **レイアウトと当たり判定は同じ計測結果から作る**（`layout.go`）。
  「描画は正しいがクリック位置がずれる」を構造的に防ぐため、カード高さは
  実際にレンダリングして測る。
- **カード高さのキャッシュはフレームを跨いで保持する**（`measurer`）。
  フレーム単位にすると、スクロール上限の計算が毎フレーム全カードを描画して
  658タスクで 36ms/frame になる（実測。修正後 3.3ms）。**キャッシュは
  `recompute()` で破棄** — 中身が変わったのに古い高さを使うと、描画と
  当たり判定がズレる。
- **ワイド前提**（240桁下限・400桁目標）。狭い端末のフォールバックは書かない。

## 検証（GUI/端末を人が見ないで済む形を保つ）

- `-dump` で TTY 無しに1フレーム出せる。`-plain` は ANSI 無しなので diff 可能。
- **ジェスチャ中の状態は `-demo` で1フレームに落とす**（`move` / `drag` /
  `graph` / `help`）。「ドロップ位置の印は出ているか」を人間の目に頼らない。
- **マウスは合成 SGR バイトを `tea.WithInput` に流して駆動できる**
  （`\x1b[<0;X;YM` 押下 / `\x1b[<32;X;Ym` 移動 / `\x1b[<0;X;Ym` 離す）。
  実 Program を回す e2e はこの方式。
- 新しい UI を足したら **`-dump` を複数幅で回してフレームを目で確認**する
  （前項の CJK 桁揃えのため）。

## Commits

gitmoji-driven（`<:gitmoji:>[(<scope>)][!] <subject>`）。
[CONTRIBUTING.md](https://github.com/akira-toriyama/.github/blob/main/CONTRIBUTING.md)。
subject / body は英語。1件 = 1 PR（squash）、docs は同一 PR で更新。
