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

**全キーは `?` が正典。** 起動して `?` を押すと、その時点で有効なキーが全部出る
（一覧は `internal/ui/keys.go` の `key.Binding` から生成しているので、handler が
照合しているものとズレない）。ここに表を置くとその写しが手書きで増えるだけなので、
置くのは取っ掛かりの5つだけにする。

| キー | 動作 |
|---|---|
| `?` | **キー一覧**（ここから全部辿れる） |
| `Space` | 詳細ペイン |
| `S` | 依存グラフ |
| `Enter` | move mode（`Enter` 確定・`Esc` 取消）。**peek を開いていると / Table では編集メニュー** |
| `q` | 終了 |

画面下部は1行だけで、そこに出るのは**画面に出ていないこと**（今入ったモードの
出口・失敗・読み込み実績）に限る。キー一覧は出さない — 部分的なキー列は、
読んだ人に「これで全部」と思わせる分だけ無い方がましだった（`>` があって `<` が
無く、blocker を辿ったら戻れないと読まれた）。

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
go run ./cmd/ridge -dump -plain -cols 240 -rows 60 # 1フレームを平文で出力
go run ./cmd/ridge -dump -peek               # 詳細ペインを開いた状態
go run ./cmd/ridge -dump -tree               # 依存ツリーを開いた状態
go run ./cmd/ridge -demo graph -dump         # 依存グラフ
go run ./cmd/ridge -demo move -dump          # move mode 中
go run ./cmd/ridge -demo drag -dump          # ドラッグ中
go run ./cmd/ridge -demo edit -dump          # フィールド編集メニュー
go run ./cmd/ridge -demo add -dump           # quick add（filter 文脈チップつき）
go run ./cmd/ridge -demo slice -dump         # slice パネル（label:ui 選択済み）
go run ./cmd/ridge -demo sort -dump          # Table を due ▲ でソートした状態
go run ./cmd/ridge -demo filter -dump        # フィルタ入力がキーボードを持っている状態
go run ./cmd/ridge -demo fail -dump          # 書き込みが拒否された ⚠ 行
go run ./cmd/ridge -demo help -dump          # `?` キー一覧オーバーレイ
go run ./cmd/ridge -readonly -dump           # schema gate で read-only の盤面
```

`-readonly` だけは model の状態ではなく **store の性質**なので `-demo` ではなく
フラグにしてある。実物を出すには古い schema の board が要る = 手では作れないので、
この経路が無いと read-only の1フレームは誰も目視できない（実際、その状態の警告を
消す退行を1度通した）。`-mock -readonly` で TUI としても触れる。

## 既知の課題

- 本文編集はファイル直書きなので shard の `updated` が進まない。furrow 側の
  `edit --body`（t-8q8c・2026-08-10 着地）が正しい経路だが**未リリース** —
  最新 release は v4.0.0（2026-08-09）で `unknown flag: --body`。release が出たら
  乗り換える（contract job が release を pin しているので、そこで自動的に分かる）。
- swimlane（group by）未実装。
- Table ビューに横スクロールが無い（ワイド前提の設計判断。要るなら既存依存の
  bubbles viewport v2 の `SoftWrap=false` + `XOffset` を配線する — 新規実装不要と
  確認済み。罠: `SetXOffset` は `SoftWrap=true` だと黙って no-op）。

## スタック

```
charm.land/bubbletea/v2      ランタイム
charm.land/lipgloss/v2       スタイル・レイアウト・コンポジタ（Layer / Hit）
charm.land/bubbles/v2        help / key / textinput / viewport
github.com/charmbracelet/x/ansi  幅を保つ切り詰め（CJK 必須。`len()` 禁止の相方）
```

v2 からモジュールパスが `github.com/charmbracelet/*` → `charm.land/*` に
移転している点に注意（v1 は旧パスのまま）。
