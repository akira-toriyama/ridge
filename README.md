# ridge

**furrow の TUI front-end.** [furrow](https://github.com/akira-toriyama/furrow) を
CLI/JSON 契約経由で読み書きする、キーボード優先のカンバン。GUI 版は
[vista](https://github.com/akira-toriyama/vista)。furrow の Go パッケージを
import しない契約とその理由は [CLAUDE.md](CLAUDE.md)、用語は
[glossary.md](glossary.md) が正本。

```sh
go run ./cmd/ridge            # 起動（実 furrow の盤面。furrow が PATH に要る）
go run ./cmd/ridge -mock      # 内蔵 fixture で起動（furrow 不要）
go run ./cmd/ridge -dump      # TTY 無しで1フレーム出力（常に fixture）
go run ./cmd/ridge -benchload # 実盤面の読み込みレイテンシを実測して終了（読み取りのみ）
```

## 現在地

実 furrow に接続済み（t-s86r）。読みは `board --json` / `ls -r '' --json` /
`epic ls -r '' --all --json` の並列 3 exec + body ファイル（レイテンシは
`-benchload` が実盤面で測る）。書きは**楽観的キュー**（glossary の
「persist キュー」）で、`furrow set / done / check / retitle / repo / ref / dep /
note / review / edit --body` を裏で直列に流す — 一覧は `internal/board/provider.go`
の Persist* が正本。例外は **store-first** の書き込み（quick add と epic 管理 —
glossary の「store-first」）。

## ビュー

各ビューの語義と設計理由は glossary が正本。ここは「何を答えるか」と操作だけ。

### Board — カンバン

レーンが列。ヘッダに件数・WIP・value/effort 合計。カードは日本語タイトルを
折り返して表示し、`▸` actionable / `▤` epic チップ / `x1` blocked /
`[0/7]` チェックリスト / ラベルチップ / repo を載せる。

**move mode** が中心の操作。GitHub Projects の作法（`Enter` で持ち上げ → 矢印で
移動 → `Enter` 確定 / `Esc` 取消）で、furrow の sparse priority による
並べ替えに 1:1 で対応する。マウスでカードを掴んでドラッグもできる。

### Graph — 依存グラフ

カード上で `S`（または `Shift+Space`）。そのタスクを起点に、**手前が blocker・
奥が「閉じると動き出すもの」**の階層グラフ。`Enter` でノードを新しい起点にして
辿れる（読むのではなく歩く）。`o` で上下 / 左右を切り替える（既定は上下。
2 つの向きの契約は glossary の「orientation」）。

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

入り切らない段は落としてヘッダで件数を出す（`z` で radius を狭められる）—
右端で黙って切らない。

### Map — 依存マップ

`T`（`t` = そのタスクの依存ツリー、の全体版）。**盤面の依存クラスタを全部
一画面に並べる**。Graph が「このタスクの周り」を答えるのに対し、Map は起点を
持たず「盤面は何と何が絡んでいるか」を答える。線は引かず、インデントが深さ・
`←` が blocker の名指し（glossary の「Map」「cluster」「scope」）。

`-dump -demo map` の抜粋（実出力は 240 桁 3 カラム。ここは幅を詰めて 2 カラム分だけ）:

```
 ── #1  4 nodes · depth 2 ────────────────────  ── #2  2 nodes · depth 1 ────────────────────
     t-ehk7 キャンプ献立プラン — 3泊分の夕食…       t-fn4k 装備の安全講習 — ナイフと火の…
 ▌   x t-jv3j 行程表 v2 — 阿蘇→高千穂を…  ←t-ehk7     x t-0esb 子どもの自由研究連動 —…  ←t-fn4k
       x t-pk4f 買い出し計画 — 道の駅ごとに… ←t-jv3j   1 unblocked · 1 blocked · t-fn4k frees 1
       x t-rmtc 予約の総ざらい — 温泉・…      ←t-jv3j
   1 unblocked · 3 blocked · t-ehk7 frees 3
```

- `z` で scope 切替（既定 **open**・`all` で全部）。
- `⏎` / `S` で、その行を起点にした Graph へ。`Esc` で **Map に戻る**。
- filter が効いていても行は消さず、薄く落としてヘッダで件数を出す。

### 詳細ペイン

`Space` で開く。解決済みの双方向依存リスト（`blocked by` / `blocks` を
ID+タイトル+レーンまで解決）、チェックリスト、本文。`t` で推移的ツリー。
`Enter` で**フィールド編集メニュー**（glossary の「編集メニュー」）: title /
value / effort / labels / epic / due / deps / repos / refs / checklist。

### Boxes — 箱の俯瞰

`E`。**盤面の epic を全部、repo 別に並べる**。Graph / Map が task の依存を
答えるのに対し、これは「どの repo が今どの箱で作業しているか」を答える
（graph にしない理由は glossary の「箱の俯瞰」）。`⏎` でその箱に絞って盤面へ
（`epic:<id>` の slice term）、`m` でその箱のオーバーレイ、`z` で closed 込み、
`^u/^d` でページ。

### Roadmap — due タイムライン

`C`。**due を持つ open な task を due 昇順に並べ、横軸 = 時間に `◆` を置く**。
「何がいつ切れるか」を答えるビューで、`furrow brief` の due 先頭・
`-q is:overdue` の時間軸版にあたる（glossary の「Roadmap」「zoom」）。
`z` で day / week / month、`h`/`l` で窓を pan。読み専用。

`ridge -roadmap` で実盤面をこのビューから開ける。headless は
`-dump -roadmap`（day）と `-demo roadmapweek` / `-demo roadmapmonth`。

### Swim — swimlane（group by）

`W`。**盤面のレーンを横軸、group by の値を縦軸の「帯」にした2次元グリッド**。
`furrow ls --tree` に2つ目の軸を与えたもので、既定の軸も **box**（`tab` で
repo / label へ）。帯は既定で畳んであり（畳んだフレーム = 盤面のヒストグラム）、
`space` で開く。`⏎` でその帯に絞って盤面へ、`z` で scope open/all。読み専用
（帯・rail の語義と読み専用の理由は glossary の「Swimlane」「帯」「rail」）。

headless は `-dump -demo swim`（既定）/ `swimopen`（帯を開いた状態）/
`swimrepo`（repo 軸）/ `swimall`（scope all）。

### 保存ビュー — タブ + views.toml

GitHub Projects の view タブ相当（語義は glossary の「保存ビュー」「未保存ドット」）。
ファイルは `~/.config/ridge/views.toml`（`XDG_CONFIG_HOME` 対応）。

```toml
[[view]]
name = "今週の締切"
layout = "roadmap"    # board | table | roadmap（省略 = board）
q = "is:actionable"   # furrow -q へそのまま渡る文字列
sort = "due asc"      # updated|created|value|effort|due [asc|desc]（効くのは table）
slice = "epic:e-xxxx" # repo|label|epic :値（slice パネルの選択と同じ）
```

- タブ帯はタイトル行の Board|Table の右。`1`-`9` で切替、`V` で現在の状態を
  active タブへ保存。タブが無い状態の `V` は "view N" で新規作成（rename は
  views.toml を編集）。上限は 9 個。
- active タブから状態がずれるとタブに **●**。digit の再押下で保存済みの束に
  巻き戻せる。roadmap ビューの中でも `1`-`9` / `V` は効く。
- fixture 系（`-mock` / `-dump` / `-readonly`）は実 views.toml を読まず書けない。
  headless 検証面は `-demo views` / `-demo viewsroad` / `-demo viewsmany`。

## キー

**全キーは `?` が正典** — 一覧は `internal/ui/keys.go` の `key.Binding` から
生成しているので、handler が照合しているものとズレない。ここは取っ掛かりだけ。

| キー | 動作 |
|---|---|
| `?` | **キー一覧**（ここから全部辿れる） |
| `Space` | 詳細ペイン |
| `S` | 依存グラフ（1タスク起点。`o` で上下 / 左右） |
| `T` | 依存マップ（全クラスタ俯瞰） |
| `E` | 箱の俯瞰（全 epic を repo 別に。`⏎` でその箱に絞る・`z` で closed 込み） |
| `C` | roadmap（due タイムライン。`z` で day/week/month・`h`/`l` で pan） |
| `W` | swimlane（レーン×group by。`space` で帯を開閉・`tab` で軸・`⏎` でその帯に絞る） |
| `1`-`9` / `V` | 保存ビューの切替 / 保存（views.toml が正本。タブ無しの `V` = 新規） |
| `Enter` | move mode（`Enter` 確定・`Esc` 取消）。**peek を開いていると / Table では編集メニュー** |
| `f` | revisit lens（`furrow revisit` が flag した open task だけに絞る。peek に理由。`-revisit` で headless） |
| `i` | 選択タスクを reviewed に stamp（`furrow review <id>`。`updated` は動かない） |
| `q` | 終了 |

画面下部は1行だけで、そこに出るのは**画面に出ていないこと**（今入ったモードの
出口・失敗・読み込み実績）に限る。キー一覧は出さない — 部分的なキー列は、
読んだ人に「これで全部」と思わせる分だけ無い方がましだった。

## 設計方針

- **ワイド前提**: 狭い端末は対象外（想定ディスプレイと桁数の下限・目標は
  [CLAUDE.md](CLAUDE.md)）。
- **キーボード優先**: マウスでできることには必ずキーボードの等価物がある。
  マウス追跡中は端末のテキスト選択が効かなくなるため（回避キーは端末依存:
  xterm/Ghostty/tmux=`Shift`、iTerm2=`Option`）、`M` で切れる。
- **楽観的 TUI**: 書き込みの完了を待たず先に画面を更新し、quit は未完了の
  書き込みを flush してから終了する。意味が furrow 側にある書き込みだけは
  先取りしない（glossary の「persist キュー」「store-first」）。
- **ロジックは furrow 側に置く**（判断規範は [CLAUDE.md](CLAUDE.md)）。

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

`-dump` / `-demo` / `-graphlr` / `-readonly` の語義と、後二者がなぜ `-demo` でなく
フラグかは glossary の「内部」節。各 `-demo` の1行説明は `internal/ui/dump.go` の
`demoState`（各 case のコメント）が持つ。

### `-debuglog` — 操作履歴の構造化ログ

```sh
go run ./cmd/ridge -debuglog session.jsonl        # 実盤面 + 全イベント記録
go run ./cmd/ridge -mock -debuglog session.jsonl  # fixture でも記録できる
```

「操作したら盤面がこうなった」系のバグ報告にはこのファイルを添付する
（層と hook 点は glossary の「-debuglog」）。**打鍵は 1 文字ずつそのまま記録
される**（modal に打った title や filter 文も含む。入らないのは body 本文だけ）—
他人に渡す前に中身を確認すること。

## 既知の課題

- swimlane（`W`）は**保存ビューの layout に入っていない**。束が
  `{layout, q, sort, slice}` なのに対し、このビューの状態は軸と scope を持つので、
  `layout = "swim"` だけ保存すると「保存したビューが復元されない」保存になる
  （map / boxes を除いてあるのと同じ理由）。
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
