# ridge

**furrow の TUI front-end.** [furrow](https://github.com/akira-toriyama/furrow) を
CLI/JSON 契約経由で読み書きする、キーボード優先のカンバン。

GUI 版は [vista](https://github.com/akira-toriyama/vista)（Tauri v2 + React）。
ridge はその端末版で、**両方とも furrow の Go パッケージを import しない** —
`furrow ls --json` / `furrow set` を叩くクライアントである、というのが
[furrow の non-goals](https://github.com/akira-toriyama/furrow/blob/main/docs/non-goals.md)
に書かれた建て付け。

```sh
go run ./cmd/ridge            # 起動（実 furrow の盤面。furrow が PATH に要る）
go run ./cmd/ridge -mock      # 内蔵 fixture で起動（furrow 不要）
go run ./cmd/ridge -dump      # TTY 無しで1フレーム出力（常に fixture）
go run ./cmd/ridge -benchload # 実盤面の読み込みレイテンシを実測して終了（読み取りのみ）
```

## 現在地 — POC から出発し、実 furrow に接続済み

このリポジトリは、2026-07-20〜21 に furrow の
[`poc/tui-bubbletea-v2`](https://github.com/akira-toriyama/furrow/tree/poc/tui-bubbletea-v2)
ブランチで行った実現可能性検証のコードを出発点にしている。t-s86r で実 furrow に
接続した: 読みは `board` / `ls -r ''` / `epic ls` の並列 3 exec + body ファイル
（実測 63-77ms / 914 tasks・cold 181ms）、書きは**楽観的キュー** — 盤面へ先に適用し、
`furrow set/done/check` を裏で直列に流し、失敗したら store 再読で巻き戻す
（書き実測 85-115ms・respace 時 280ms が根拠。`internal/ui/persist.go`）。

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
折り返して表示し、`▸` actionable / `▤` epic チップ（所属 epic のタイトルを解決）/ `x1` blocked /
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
`Enter` で**フィールド編集メニュー**: title / value / effort / labels / epic /
due / repos / checklist（カーソルで項目選択・toggle/add/delete/reword）を
`furrow set / retitle / repo / check` 相当の1書き込みで編集する（楽観的適用・
失敗時は store 再読でロールバック）。

## キー

| キー | 動作 |
|---|---|
| `←→↑↓` / `hjkl` | カーソル移動 |
| `Enter` / `m` | move mode（`Enter` 確定・`Esc` 取消）。peek が開いている時と Table 行では `Enter` = **フィールド編集メニュー** |
| `K` / `J` | レーン内で1つ上/下へ |
| `[` / `]` | レーンを前/次へ |
| `Space` | 詳細ペイン |
| `t` | 依存ツリー（詳細ペイン内） |
| `S` / `Shift+Space` | **依存グラフ** |
| `>` / `<` | blocker へジャンプ / 戻る |
| `/` | フィルタ = furrow `-q` パススルー（`lane:ready repo:vista is:blocked value:>=4 updated:>=-2w`。文法の正本は `furrow ls --help` の -q 節） |
| `b` | blocked のみ表示 |
| `s` | **slice パネル**（repo / label / epic の値で絞る左パネル。選択は filter と AND 合成・`Esc` でパネルを残して盤面へ・再選択で解除） |
| `v` | Board ⇄ Table |
| `o` | Table のソート（canonical → updated → created → value → effort → due を循環・再押しで昇降反転・ヘッダに `▲▼`）。ソート可能なヘッダのクリックでも同じ・`lane` クリックで canonical へ |
| `a` | quick add（フォーカス列へ起票。適用中 filter の label/epic/repo を継承 — チップで明示） |
| `d` | done |
| `e` | 本文を `$EDITOR` で編集 |
| `r` | store 再読 |
| `R` | `furrow sync`（git）→ 再読 |
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
- **楽観的 TUI**: 書き込みの完了を待たず先に画面を更新する。store への記録は
  直列キュー（同時 1 本 — 並べ替えの anchor が前の書き込みの結果に依存するため）。
  quit は未完了の書き込みを flush してから終了する。
- **ロジックは furrow 側に置く**: 「仮に TUI を作るならロジックが冗長になるか」で
  迷ったら furrow へ。ridge と vista で同じものを二重に持たない。

## 検証

すべて headless で確認できる（GUI や端末を人が見る必要がない）:

```sh
go test ./...                      # 全テスト（furrow が PATH にあれば contract test も回る）
go run ./cmd/ridge -dump -plain -w 240 -h 60 # 1フレームを平文で出力
go run ./cmd/ridge -dump -peek               # 詳細ペインを開いた状態
go run ./cmd/ridge -dump -tree               # 依存ツリーを開いた状態
go run ./cmd/ridge -demo graph -dump         # 依存グラフ
go run ./cmd/ridge -demo move -dump          # move mode 中
go run ./cmd/ridge -demo drag -dump          # ドラッグ中
go run ./cmd/ridge -demo edit -dump          # フィールド編集メニュー
go run ./cmd/ridge -demo add -dump           # quick add（filter 文脈チップつき）
go run ./cmd/ridge -demo slice -dump         # slice パネル（label:ui 選択済み）
go run ./cmd/ridge -demo sort -dump          # Table を due ▲ でソートした状態
```

## 既知の課題

- 本文編集はファイル直書きなので shard の `updated` が進まない（furrow 側の
  置換コマンド要望 t-8q8c が着地したら乗り換える）。
- swimlane（group by）未実装。
- Table ビューに横スクロールが無い（`bubbles/v2` の table が非対応）。

## スタック

```
charm.land/bubbletea/v2  ランタイム
charm.land/lipgloss/v2   スタイル・レイアウト・コンポジタ（Layer / Hit）
charm.land/bubbles/v2    help / key / textinput / viewport
```

v2 からモジュールパスが `github.com/charmbracelet/*` → `charm.land/*` に
移転している点に注意（v1 は旧パスのまま）。
