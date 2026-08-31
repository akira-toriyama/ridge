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
接続した: 読みは `board` / `ls -r ''` / `epic ls --all` の並列 3 exec + body ファイル
（実測 63-77ms / 914 tasks・cold 181ms）、書きは**楽観的キュー** — 盤面へ先に適用し、
`furrow set/done/check` を裏で直列に流し、失敗したら store 再読で巻き戻す
（書き実測 85-115ms・respace 時 280ms が根拠。`internal/ui/persist.go`）。
例外は **store-first** の書き込み（quick add と epic 管理）: 何を意味するかが
furrow 側にあるもの（id 発行・repo ごとの active 枠・導出値）は盤面に先取り適用せず、
着地後の再読で収束させる。

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

カード上で `S`（または `Shift+Space`）。そのタスクを起点に、**手前が blocker・
奥が「閉じると動き出すもの」**の階層グラフ。`Enter` でノードを新しい起点にして
辿れる（読むのではなく歩く）。

**向きは `o` で上下 / 左右を切り替える**（既定は上下）。位置と矢頭の両方が同じ
方向を指すので、どちらの向きでも矢印の意味を覚え直す必要はない。

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

`o` を押すと同じグラフが左右になる（`-dump -graphlr` で headless に出せる）:

```
 ╭────────────────╮                        ╭──────────────╮
 │ v t-ecfm       │───╮                ╭──▶│ x t-pk4f     │
 ╰────────────────╯   │   ┏━━━━━━━━━┓  │   ╰──────────────╯
                      ├──▶┃ t-jv3j  ┃──┤
 ╭────────────────╮   │   ┗━━━━━━━━━┛  │   ╭──────────────╮
 │ v t-g8bn       │───╯                ╰──▶│ x t-rmtc     │
 ╰────────────────╯                        ╰──────────────╯
```

**制約が入れ替わるのが左右版の勘所**。上下では幅が与えられて行数を交渉するが、
左右では高さが与えられて**幅**を交渉する — 240桁に階層をいくつ並べられるかが
箱の幅を決める。実データの ego graph は最長でも6段（最長鎖が5辺）で、6段 ×
下限幅がちょうど240桁に収まる。それを超える盤面では**入り切らない段を落として
ヘッダで件数を出す**（`z` で radius を狭められる）— 右端で黙って切らない。

### Map — 依存マップ

`T`（`t` = そのタスクの依存ツリー、の全体版）。**盤面の依存クラスタを全部
一画面に並べる**。Graph が「このタスクの周り」を答えるのに対し、Map は起点を
持たず「盤面は何と何が絡んでいるか」を答える。

**線は引かない。** インデントが深さ、`←` が blocker の名指しで、これで曖昧さは
ゼロになる（`←t-ehk7,t-t38k` は 2 本の辺そのもの）。2 経路で到達するノードも
**1 回しか出ない** — ツリー表示の唯一の弱点がここでは消える。実データ 658 タスクで
「全体を 1 枚の DAG に描く」を検証して捨てた経緯は furrow `t-5g52`。

`-dump -demo map` の抜粋（実出力は 240 桁 3 カラム。ここは幅を詰めて 2 カラム分だけ）:

```
 ── #1  4 nodes · depth 2 ────────────────────  ── #2  2 nodes · depth 1 ────────────────────
     t-ehk7 キャンプ献立プラン — 3泊分の夕食…       t-fn4k 装備の安全講習 — ナイフと火の…
 ▌   x t-jv3j 行程表 v2 — 阿蘇→高千穂を…  ←t-ehk7     x t-0esb 子どもの自由研究連動 —…  ←t-fn4k
       x t-pk4f 買い出し計画 — 道の駅ごとに… ←t-jv3j   1 unblocked · 1 blocked · t-fn4k frees 1
       x t-rmtc 予約の総ざらい — 温泉・…      ←t-jv3j
   1 unblocked · 3 blocked · t-ehk7 frees 3
```

俯瞰でしか出ない数字を各クラスタと全体で出す: **今すぐ着手できる数 / 塞がれている数
/ 最も多くを塞いでいるタスク / 最長チェーン**。数字はすべて**同じ画面の行の印から
数え直せる**（`unblocked + blocked + done = ノード数`）— 純トポロジーで数えると
`v` が並ぶ行の上に「7 unblocked」が出る。

- `z` で scope 切替。既定は **open**（done とその辺を落とす — 終わった依存は
  blocker ではない。全件だと生きたクラスタが死んだ塊に溶接される）、`all` で全部。
- `⏎` / `S` で、その行を起点にした Graph へ。`Esc` で **Map に戻る**（盤面には
  落とさない）。
- filter が効いていても**行は消さない**（消える辺は盤面についての嘘になる）。
  薄く落として、ヘッダで件数を出す。

### 詳細ペイン

`Space` で開く。解決済みの双方向依存リスト（`blocked by` / `blocks` を
ID+タイトル+レーンまで解決）、チェックリスト、本文。`t` で推移的ツリー。
`Enter` で**フィールド編集メニュー**: title / value / effort / labels / epic /
due / deps / repos / checklist（カーソルで項目選択・toggle/add/delete/reword）を
`furrow set / retitle / repo / check / dep` 相当の1書き込みで編集する（楽観的適用・
失敗時は store 再読でロールバック）。

### Boxes — 箱の俯瞰

`E`。**盤面の epic を全部、repo 別に並べる**。Graph / Map が task の依存を
答えるのに対し、これは「どの repo が今どの箱で作業しているか」を答える。

**graph にしないのは実測が理由。** 2026-08-28 の実盤面は箱 153 個
（open 117 / closed 36）に対し epic 間の dep edge が **4 本**（2 箱）しか無い。
ego graph も連結成分も、149 個の孤立ノードを並べて本体を埋めるだけになる。
4 本は Map と同じ `←id` インライン tag で足りる。repo で括るのは、全箱が持つ
唯一の軸だから（実盤面で repo 無しは 0・repo は 31）— そして furrow が repo
あたり active を1つに制限するので、`▶` が縦に読むだけでチェックリストになる。

`⏎` は**新しい絞り込み機構を作らず**、slice パネルと同じ `epic:<id>` term を
発行して盤面に戻る（closed な箱でも効く）。`m` でその箱のオーバーレイ、
`z` で closed 込み、`^u/^d` でページ。

実測の詰まり方（153 箱 / 31 repo）: 240桁 = 4カラム×58セル、320桁 = 6×51、
400桁 = 6×64。

### Roadmap — due タイムライン

`C`（calendar — `R` は sync が持っている）。**due を持つ open な task を
due 昇順に並べ、横軸 = 時間に `◆` を置く**。「何がいつ切れるか」を答えるビューで、
`furrow brief` の due 先頭・`-q is:overdue` の時間軸版にあたる。

- 横軸は 1 セル = 1 日（`z` で 週 / 月。境界は暦どおり — 月曜始まりの週・月初）
- `┊` = today の縦線。overdue の `◆` は danger 色、today と同セルは warn 色
- due 無しは出さない（GH も date 無しは帯に出ない）。done も出さない —
  果たされた約束は約束ではない
- `◆` の右に所属 epic の `▤` chip（epic は日付を持たないので、GH の vertical
  marker の代わりはこの行内 chip）
- filter は Map と同契約: 隠さず **mute** して header で数える
- `h`/`l` で窓を pan。窓の外へ出た `◆` は行端の `▸`/`◂` になる — 日付付きの行が
  無日付に見えてはいけない
- **読み専用**（v1）。drag での due 変更は読みの価値検証後（t-7t28）

`ridge -roadmap` で実盤面をこのビューから開ける。headless は
`-dump -roadmap`（day）と `-demo roadmapweek` / `-demo roadmapmonth`。

### 保存ビュー — タブ + views.toml

GitHub Projects の view タブ相当。**view = {layout, q, sort, slice} の束に
名前を付けたもの**で、正本は `~/.config/ridge/views.toml`（`XDG_CONFIG_HOME`
対応）の `[[view]]`。furrow の board には置かない — 見せ方は front-end の
所有物（vista と共有したくなったら furrow 側に要望を出し直す）。

```toml
[[view]]
name = "今週の締切"
layout = "roadmap"    # board | table | roadmap（省略 = board）
q = "is:actionable"   # furrow -q へそのまま渡る文字列
sort = "due asc"      # updated|created|value|effort|due [asc|desc]（効くのは table）
slice = "epic:e-xxxx" # repo|label|epic :値（slice パネルの選択と同じ）
```

- タブ帯はタイトル行の Board|Table の右。`1`-`9` で切替、`V` で現在の状態を
  active タブへ保存。タブが無い状態の `V` は "view N" で新規作成（GH の
  New view と同じく placeholder 名 — rename は views.toml を編集）。
- active タブから状態がずれるとタブに **●**（GH の未保存ドット）。digit の
  再押下で保存済みの束に巻き戻せる。
- **roadmap ビューの中でも `1`-`9` / `V` は効く** — roadmap は保存ビューに
  なれる唯一の全画面ビューなので、そのタイトル行にもタブ帯が出る。graph /
  map / boxes が layout に無いのは意図: graph は起点 task が要り、map / boxes
  は population の切替で、どれも「名前を付けて冷えた状態から再現する」対象では
  ない。
- 読み込みは起動時1回・書くのは `V` だけ（全量書き戻し — セッション中の手編集は
  次の `V` で消える）。TOML 構文エラーだけが起動失敗で、semantic な typo は
  1フィールド単位で clamp して status line に警告する。
- fixture 系（`-mock` / `-dump` / `-readonly`）は実 views.toml を読まず書けない。
  headless 検証面は `-demo views`（table + 未保存ドット）と `-demo viewsroad`
  （roadmap タブ）。

## キー

**全キーは `?` が正典。** 起動して `?` を押すと、その時点で有効なキーが全部出る
（一覧は `internal/ui/keys.go` の `key.Binding` から生成しているので、handler が
照合しているものとズレない）。ここに表を置くとその写しが手書きで増えるだけなので、
置くのは取っ掛かりだけにする（数を書くとその数が古くなる）。

| キー | 動作 |
|---|---|
| `?` | **キー一覧**（ここから全部辿れる） |
| `Space` | 詳細ペイン |
| `S` | 依存グラフ（1タスク起点。`o` で上下 / 左右） |
| `T` | 依存マップ（全クラスタ俯瞰） |
| `E` | 箱の俯瞰（全 epic を repo 別に。`⏎` でその箱に絞る・`z` で closed 込み） |
| `C` | roadmap（due タイムライン。`z` で day/week/month・`h`/`l` で pan） |
| `1`-`9` / `V` | 保存ビューの切替 / 保存（views.toml が正本。タブ無しの `V` = 新規） |
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
  quit は未完了の書き込みを flush してから終了する。**意味が furrow 側にある書き込み
  だけは先取りしない**（store-first）: 楽観適用するには ridge が furrow の規則を
  写す必要があり、それは front-end に業務ロジックを溜めることになる。
- **ロジックは furrow 側に置く**: 「仮に TUI を作るならロジックが冗長になるか」で
  迷ったら furrow へ。ridge と vista で同じものを二重に持たない。

## 検証

すべて headless で確認できる（GUI や端末を人が見る必要がない）:

```sh
go test ./...                      # 全テスト（furrow が PATH にあれば contract test も回る）
go run ./cmd/ridge -dump -plain -cols 240 -rows 60 # 1フレームを平文で出力
go run ./cmd/ridge -dump -peek               # 詳細ペインを開いた状態
go run ./cmd/ridge -dump -tree               # 依存ツリーを開いた状態
go run ./cmd/ridge -demo drag -dump          # 一時状態の例: ドラッグ中の1フレーム
go run ./cmd/ridge -h                        # -demo 全状態の一覧（正本 = ui.DemoNames）
go run ./cmd/ridge -readonly -dump           # schema gate で read-only の盤面
go run ./cmd/ridge -graphlr -dump -demo graphall  # 依存グラフを左右向きで（`o` と同じ状態）
go run ./cmd/ridge -dump -roadmap            # due タイムライン（週/月軸は -demo roadmapweek / roadmapmonth）
```

`-demo` の名前をここに列挙しない: 写しは必ず古くなる（実際、この節の旧一覧は
`edit` を「フィールド編集メニュー」と説明したまま checklist 直行に変わっていた）。
一覧は `-h` が、各状態の1行説明は `internal/ui/dump.go` の `demoState`
（各 case のコメント）が持つ。

`-graphlr` も `-demo` ではなくフラグ。グラフの向きは一時状態ではなく
**ビューの設定**（`-table` と同類）で、フラグにしておけば既存の graph 系 `-demo`
がそのまま両向きで出せる — 状態ごとに写しを作らずに済む。

`-readonly` だけは model の状態ではなく **store の性質**なので `-demo` ではなく
フラグにしてある。実物を出すには古い schema の board が要る = 手では作れないので、
この経路が無いと read-only の1フレームは誰も目視できない（実際、その状態の警告を
消す退行を1度通した）。`-mock -readonly` で TUI としても触れる。

### `-debuglog` — 操作履歴の構造化ログ

```sh
go run ./cmd/ridge -debuglog session.jsonl        # 実盤面 + 全イベント記録
go run ./cmd/ridge -mock -debuglog session.jsonl  # fixture でも記録できる
```

1 イベント 1 行の JSONL。層は 5 つ — **input**（key/mouse の生イベント）/
**mode**（mode・view の遷移）/ **apply**（gesture が queue に enqueue/refuse
したもの）/ **persist**（furrow exec・書き込みの成否と所要 ms・reload の
着地/skip）/ **status**（status line に出した note/fail の全文 — queue に
届かない拒否はここにしか現れない）。「操作したら盤面がこうなった」系の
バグ報告にはこのファイルを添付する。

**打鍵は 1 文字ずつそのまま記録される**（modal に打った title や filter 文も
含む）。入らないのは task の body 本文だけ — body は `$EDITOR` 側で編集され
event loop を通らない。他人に渡す前に中身を確認すること。

追記 open なので 1 ファイルに複数セッションを重ねられる（区切りは
`session/start` — 構築時に書くので必ず各セッションの先頭行）。`-perflog` は
別物として残る: あちらは latency 計測の素材（TSV 2 列・`-benchload` 対応）、
こちらは時系列の再構成用。

## 既知の課題

- 本文編集はファイル直書きなので shard の `updated` が進まない。furrow 側の
  `edit --body`（t-8q8c・2026-08-10 着地）が正しい経路で、v5.0.0 でリリース済み。
  残りは ridge 側の作業（furrowClient に stdin 経路が無い・空 body が exit 2）で
  t-t9ac。
- swimlane（group by）未実装。task の group by であって、`E` の箱の俯瞰とは別物。
- Table ビューに横スクロールが無い（ワイド前提の設計判断。要るなら既存依存の
  bubbles viewport v2 の `SoftWrap=false` + `XOffset` を配線する — 新規実装不要と
  確認済み。罠: `SetXOffset` は `SoftWrap=true` だと黙って no-op）。

## スタック

```
charm.land/bubbletea/v2      ランタイム
charm.land/lipgloss/v2       スタイル・レイアウト・コンポジタ（Layer / Hit）
charm.land/bubbles/v2        help / key / textinput / viewport
github.com/charmbracelet/x/ansi  幅を保つ切り詰め（CJK 必須。`len()` 禁止の相方）
github.com/pelletier/go-toml/v2  views.toml の読み書き（保存ビュー）
```

v2 からモジュールパスが `github.com/charmbracelet/*` → `charm.land/*` に
移転している点に注意（v1 は旧パスのまま）。
