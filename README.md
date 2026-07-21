# ridge

**furrow の TUI front-end.** [furrow](https://github.com/akira-toriyama/furrow) を
CLI/JSON 契約経由で読み書きする、キーボード優先のカンバン。

GUI 版は [vista](https://github.com/akira-toriyama/vista)（Tauri v2 + React）。
ridge はその端末版で、**両方とも furrow の Go パッケージを import しない** —
`furrow ls --json` / `furrow set` を叩くクライアントである、というのが
[furrow の non-goals](https://github.com/akira-toriyama/furrow/blob/main/docs/non-goals.md)
に書かれた建て付け。

```sh
go run .            # 起動（今は mock データ）
go run . -dump      # TTY 無しで1フレーム出力
```

## 現在地 — POC から出発した

このリポジトリは、2026-07-20〜21 に furrow の
[`poc/tui-bubbletea-v2`](https://github.com/akira-toriyama/furrow/tree/poc/tui-bubbletea-v2)
ブランチで行った実現可能性検証のコードを出発点にしている。**動くが、データは
まだ mock。** 実 furrow に繋ぐのが最初の仕事。

POC が答えを出した3つの問い:

| 問い | 答え |
|---|---|
| GitHub Projects 相当のカンバンは端末で成立するか | **する** |
| マウス DnD はできるか | **できる**（自作 約200行） |
| 依存関係を分かりやすく見せられるか | **見せられる**（ライブラリ不要・全部自作） |

調査の全記録は furrow ボードの
[t-3c5p](https://github.com/akira-toriyama/projects/blob/main/.furrow/bodies/t-3c5p.md) と
[t-5g52](https://github.com/akira-toriyama/projects/blob/main/.furrow/bodies/t-5g52.md)。

## ビュー

### Board — カンバン

レーンが列。ヘッダに件数・WIP・value/effort 合計。カードは日本語タイトルを
折り返して表示し、`▸` actionable / `▤` epic（子進捗つき）/ `x1` blocked /
`[0/7]` チェックリスト / ラベルチップ / repo を載せる。

**move mode** が中心の操作。GitHub Projects の作法（`Enter` で持ち上げ → 矢印で
移動 → `Enter` 確定 / `Esc` 取消）で、これは furrow の sparse priority による
並べ替えに 1:1 で対応する。マウスでカードを掴んでドラッグもできる。

### Graph — 依存グラフ

カード上で `S`（または `Shift+Space`）。そのタスクを起点に、**上が blocker・
下が「閉じると動き出すもの」**の階層グラフ。`Enter` でノードを新しい起点にして
辿れる（読むのではなく歩く）。

```
 ╭────────────────╮   ╭────────────────╮
 │ v t-ecfm       │   │ v t-g8bn       │
 ╰────────────────╯   ╰────────────────╯
          ╷                    ╷
          ╰────────────────────┤
                               ▼
       ┏━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
       ┃ x t-jv3j ◉ focus          ┃
       ┗━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
                    ╷
       ├────────────┤
       ▼            ▼
 ╭──────────────╮  ╭──────────────╮
 │ x t-pk4f     │  │ x t-rmtc     │
 ╰──────────────╯  ╰──────────────╯
```

### 詳細ペイン

`Space` で開く。解決済みの双方向依存リスト（`blocked by` / `blocks` を
ID+タイトル+レーンまで解決）、チェックリスト、本文。`t` で推移的ツリー。

## キー

| キー | 動作 |
|---|---|
| `←→↑↓` / `hjkl` | カーソル移動 |
| `Enter` / `m` | move mode（`Enter` 確定・`Esc` 取消） |
| `K` / `J` | レーン内で1つ上/下へ |
| `[` / `]` | レーンを前/次へ |
| `Space` | 詳細ペイン |
| `t` | 依存ツリー（詳細ペイン内） |
| `S` / `Shift+Space` | **依存グラフ** |
| `>` / `<` | blocker へジャンプ / 戻る |
| `/` | フィルタ（`lane:ready repo:vista is:blocked -lane:done`） |
| `b` | blocked のみ表示 |
| `v` | Board ⇄ Table |
| `d` | done |
| `e` | 本文を `$EDITOR` で編集 |
| `M` | マウス追跡 ON/OFF |
| `?` | ヘルプ |
| `q` | 終了 |

グラフ内では `←→↑↓` でノード移動、`Enter` で再ルート、`z`/`1`/`2`/`3`/`0` で
ホップ半径、`Esc` で盤面へ。

## 設計方針

- **ワイド前提**: 想定ディスプレイは 3840×1620（32:9）。**240桁を下限・400桁を
  目標**とし、狭い端末向けのフォールバックは書かない。
- **キーボード優先**: マウスでできることには必ずキーボードの等価物がある。
  マウス追跡中は端末のテキスト選択が効かなくなるため（回避キーは端末依存:
  xterm/Ghostty/tmux=`Shift`、iTerm2=`Option`）、`M` で切れる。
- **楽観的 TUI**: 書き込みの完了を待たず先に画面を更新する。
- **ロジックは furrow 側に置く**: 「仮に TUI を作るならロジックが冗長になるか」で
  迷ったら furrow へ。ridge と vista で同じものを二重に持たない。

## 検証

すべて headless で確認できる（GUI や端末を人が見る必要がない）:

```sh
go test ./...                      # 全テスト
go run . -dump -plain -w 240 -h 60 # 1フレームを平文で出力
go run . -dump -peek               # 詳細ペインを開いた状態
go run . -dump -tree               # 依存ツリーを開いた状態
go run . -demo graph -dump         # 依存グラフ
go run . -demo move -dump          # move mode 中
go run . -demo drag -dump          # ドラッグ中
```

## 既知の課題

- **データが mock**（`fixture.go`）。実 furrow に未接続 — 最優先タスク。
- グラフの枠線が1桁ずれる行がある（構造は正しい、見た目のみ）。
- swimlane（group by）未実装。slice パネル（repo/label で絞る左パネル）未実装。
- Table ビューに横スクロールが無い（`bubbles/v2` の table が非対応）。
- ディレクトリ構成が flat（`package main` に全部）。house style の
  `internal/` + 薄い main へ寄せる必要がある。

## スタック

```
charm.land/bubbletea/v2  ランタイム
charm.land/lipgloss/v2   スタイル・レイアウト・コンポジタ（Layer / Hit）
charm.land/bubbles/v2    help / key / textinput / viewport
```

v2 からモジュールパスが `github.com/charmbracelet/*` → `charm.land/*` に
移転している点に注意（v1 は旧パスのまま）。
