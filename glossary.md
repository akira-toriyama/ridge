# ridge — 用語集

ユーザーと Claude Code の認識ズレ防止が目的。用語の追加・改名はコード変更と
**同一 PR** で反映する。furrow 由来の語は
[furrow の glossary](https://github.com/akira-toriyama/furrow/blob/main/docs/glossary.md)
が正本で、ここには **ridge 側で意味が生まれた語**だけを置く。

## 立ち位置

| 用語 | 意味 |
|---|---|
| **ridge** | furrow の TUI front-end。この repo。 |
| **vista** | furrow の GUI front-end（Tauri v2 + React）。ridge の兄弟。 |
| **front-end** | furrow を **CLI/JSON 経由で**駆動するもの。furrow の Go パッケージは import しない。 |
| **Provider** | ridge がタスクを読み書きする唯一の口（interface）。port は `internal/board` が宣言し、adapter は `internal/store/furrowstore`（`furrow` を exec する実装）と `internal/store/memstore`（fixture）の 2 つ。mutation は Persist 契約 — **Model がローカル適用済みの変更を store に記録するだけ**で、適用そのものはしない。例外は **store-first** の族（下）。 |
| **store-first**（書き込み） | 楽観適用を**しない**書き込み。quick add と epic 族（`EpicAdd`/`EpicSet`/`EpicActivate`/`EpicDeactivate`/`EpicDepAdd`/`EpicDepRm`）が該当。理由は「その書き込みが何を意味するかが furrow 側にある」こと — activate は repo ごとの枠を**奪わず拒否**する・add は id を発行する・progress/stuck/open_deps は furrow 導出。よって盤面は何も変えず、persist キューに載せて**着地後の再読で収束**する。拒否時のロールバックは不要（適用していない）が、**同じ排出内で先に着地した store-first 書き込みがあるなら再読は必要** — でないとその変更は次の `r` まで盤面に出ない（`persistOp.noLocal` / `unreadLanded`）。 |
| **persist キュー** | 楽観的書き込みの直列キュー（`internal/ui/persist.go`）。同時 in-flight は 1 本 — 並べ替えの anchor（`--before <id>`）が直前の書き込みの結果に依存するため。失敗したら残りを破棄して store 再読 = ロールバック。quit は排出を待つ。 |
| **reconcile** | persist キュー排出後の無言の store 再読。respace された priority・closed 刻印など store 側の真実に盤面を収束させる。 |

## ビュー

| 用語 | 意味 |
|---|---|
| **Board** | カンバン。レーンが列、カードがタスク。既定のビュー。 |
| **Table** | 平坦な表形式のビュー。`v` で Board と切り替え。列は id/lane/印/v-e/title/repo/epic/labels/due/updated/deps。 |
| **Graph** | **依存グラフ**。1タスクを起点に、blocker と「閉じると動き出すもの」を軸の両側に分けた階層図（どちらの軸に並べるかは **orientation**）。`S` / `Shift+Space`。 |
| **orientation**（Graph の向き） | Graph の階層をどの画面軸に並べるか。**top-down**（既定・上が blocker / 下が「閉じると動き出すもの」）と **left-right**（左が blocker / 右）の2値で、`o` で切替（`-graphlr` で headless）。**位置と矢頭の両方**が常に同じ方向を指す（矢頭は `▼` / `▶`）ので、向きを変えても矢印の意味を覚え直さなくていい。与えられた軸を使い切り**もう一方を交渉する**関係も入れ替わる: top-down は幅が与えられてタイトル行数を交渉し、left-right は高さが与えられて箱の**幅**を交渉する。幅に入り切らない段は落として件数をヘッダに出し、**描く窓はカーソルに追随する**（選択が窓の外へ出ない）。セッションを跨いで保存しない（hop radius と同じ）。 |
| **Map**（依存マップ） | **全依存クラスタを一画面で俯瞰するビュー**。Graph が「1タスク起点」なのに対し、こちらは起点を持たない。`T`（`t` = そのタスクの依存ツリー、の全体版）。線は引かず、**インデント = 深さ・`←` = blocker の名指し**で表す。 |
| **Roadmap** | **due タイムライン**。due を持つ **open** な task を due 昇順の行にし、横軸 = 時間に `◆` を置く全画面ビュー。`C`（calendar — `R` は sync、`D` は小文字 `d`=done が write なので不採用）。due 無しは出さない（GH 同）・done も出さない（果たされた約束は約束ではない）。`┊` = today 縦線、`◆` の右に所属 epic の `▤` chip（epic は日付を持たないので GH の vertical marker は成立しない — 行内 chip が最小形）。filter は Map と同契約（隠さず mute + header で件数）。読み専用 — due の drag 変更は価値検証後（t-7t28）。 |
| **zoom**（Roadmap の） | 1 セルが暦のどれだけか。day（既定）/ week / month の 3 値で、境界は**暦どおり**（月曜始まりの週・月初）。`z` で循環 — 全画面ビューの `z` = 「どれだけ画面に載るか」の踏襲。h/l で窓を pan（1 押し = zoom の自然な一期間: 7日/4週/3月）。セッションを跨いで保存しない（hop radius と同じ）。 |
| **peek**（詳細ペイン） | 選択中タスクの詳細を横に出すオーバーレイ。`Space`。 |
| **cluster**（依存クラスタ） | 依存辺で繋がったタスクの連結成分。Map のパネル 1 枚 = 1 クラスタ。実データでは未完了分で 9 個・中央値 2 ノード。正本は `internal/board/cluster.go`（`Graph.Clusters`）— furrow に同形の口が無いので ridge が topology だけ自前で出す。 |
| **scope**（Map の） | Map が何を数えるか。`open` = done を辺ごと落とす（既定 — 終わった依存は blocker ではない）/ `all` = 全部。`z` で切替。 |

## mode（キーボードの所有者）

現在 mode はタイトル行右端の ⟨…⟩ トークンで**常時**表示される（full-screen の 4 つ —
graph・Map・Boxes・Roadmap — だけは自前のタイトル行 = 全ビューのタブ帯 + 自分のトークン）。`?` help はこの mode 名で節分けされ、今いる mode の節に
「you are here」が付く。トークンの正式語はこの表が正本。

| 用語 | トークン | 意味 |
|---|---|---|
| **normal mode** | ⟨NORMAL⟩ | 既定の mode。カーソル移動・ビュー切替・各 modal への入口。`?` はここ・move mode・graph で開ける（modal 入力中は不可 — キーボードは modal のもの）。 |
| **move mode** | ⟨MOVE⟩ | カードを持ち上げて置き直す（下の操作表を参照）。 |
| **filter mode** | ⟨FILTER⟩ | `/` で filter bar が入力を専有。 |
| **edit mode** | ⟨EDIT⟩ | 編集メニュー（edit overlay）が専有。 |
| **add mode** | ⟨ADD⟩ | quick add modal が専有。 |
| **slice mode** | ⟨SLICE⟩ | slice パネルが専有。 |
| **epic mode** | ⟨EPIC⟩ | epic オーバーレイが専有（`epicmode.go`）。slice パネルの epic 軸から入り、`esc` はパネルに戻る。 |
| **graph** | ⟨GRAPH⟩ | mode enum 外だがキーボードを専有する full-screen view — 実質 8 つ目。 |
| **dep map** | ⟨MAP⟩ | 同じく mode enum 外の full-screen view（`T`）。`?` の節名はこの行の語**そのまま**（`keys.go` の `helpSection.title`）。 |
| **box overview** | ⟨BOXES⟩ | 同じく mode enum 外の full-screen view（`E`）。PR #60 で入ったがこの表への追記が漏れていた（Roadmap の PR で補記）。 |
| **roadmap** | ⟨ROADMAP⟩ | 同じく mode enum 外の full-screen view（`C`）。 |
| **drag** | ⟨DRAG⟩ | mode ではない（`dragState`）が、gesture 中はトークンが出る。 |

## 操作

| 用語 | 意味 |
|---|---|
| **move mode** | GitHub Projects 由来の並べ替え操作。`Enter`/`m` で持ち上げ（**ただし peek が開いている / Table ビューでは同じキーが編集メニューを開く** — `model.go` の分岐が正本）→ 矢印・hjkl で 1 歩、大文字 K/J/H/L でその方向の端まで → `Enter` 確定 / `Esc` 取消。furrow の sparse priority 並べ替えに 1:1 対応。 |
| **drag**（DnD） | マウスでカードを掴んで運ぶ。move mode のマウス版で、確定経路は同一（`commitMove`）。 |
| **ghost** | ドラッグ中にカーソルに追従する半透明のカード。lipgloss の Layer（Z=99）。 |
| **drop indicator** | ドロップ先を示す印。Layer だが **ID を持たない**ので `Compositor.Hit` に拾われない（＝クリックを吸わない）。 |
| **drag threshold** | 「掴んだ」と判定するまでの最小移動距離。これが無いと1セルの震えが移動として確定してしまう（lazygit が実際に踏んだバグ）。 |
| **jump-to-blocker** | `>` で最初の未完了 blocker へカーソルを飛ばし、`<` で戻る。スタックなので何段でも潜れる。 |
| **sync（`R`）** | `furrow sync`（git の commit/pull/push）→ store 再読。自動では走らない — v1 の決定（t-s86r）。`r` は再読のみ。 |
| **filter（-q パススルー）** | filter bar は furrow `-q` への素通し。ridge は raw 文字列と store の返した id 集合（verdict）だけを持ち、文法は furrow 一本（t-ehk7）。タイプ中・拒否時は直前の verdict を保持して ⚠ を出す（盤面を空にしない）。memstore は -dump/テスト用の evaluator。語彙（`furrow vocab query-is`/`query-presence`）と一致規則は実 furrow で実測して合わせてあり、honour できない構文（ordinal/date 比較・graph qualifier）は furrow 同様に**拒否**する — 黙って 0 件を返さない。 |
| **編集メニュー（edit overlay）** | peek/Table の `Enter`／`m` で開く field 編集 modal（`editmode.go`）。menu → sub-editor（1..5 picker / toggle list / deps list / refs list / text input / checklist カーソル）の2段。適用は楽観的 + persist キュー、`furrow set` 相当は 1 write に合成。deps は一方通行の list（⏎/x で外す・`a` + task id で張る。acyclic/存在検査は furrow が正本で、ridge はミラー検証のみ）。refs も同型の一方通行 list（⏎/x で外す・`a` + file:line/URL。furrow `ref` 相当 — 並びは追加順の sequence で、sort しない）。 |
| **箱の俯瞰（box overview）** | `E` で開く全画面ビュー（`boxboard.go` / `boxboardview.go`）。**全部の箱を repo 別に並べる**。graph ではないのは実測が理由 — 2026-08-28 の実盤面は箱 153 個（open 117 / closed 36）に対し epic 間 dep edge が **4 本**（2 箱）しかなく、ego graph も連結成分も 149 個の孤立ノードを埋めるだけになる。4 本は dep map と同じ `←id` インライン tag で足りる。repo で括るのは、全箱が持つ唯一の軸であり（実盤面で repo 無しは 0・repo は 31）、furrow が repo あたり active を1つに制限するので `▶` が縦に「各 repo が今どの箱で作業しているか」のチェックリストになるから。2 repo にまたがる箱は**両方に出る**（片方から消すとその repo の一覧が黙って不完全になる）。行は `slice パネル` と同じ文法（suffix を先に測り、残りを title に渡す）。`⏎` は既存の `epic:` slice term を発行するだけ（新しい絞り込み機構を作らない。closed な箱でも効く）、`m` でオーバーレイ、`z` で closed 込み、`^u/^d` でページ。 |
| **epic 母集団** | epic の読みは `epic ls --all` で、**closed な箱も盤面に載る**（`EpicInfo.Closed`、ゼロ値 = open）。ただし面ごとに見せる母集団が違う: `Board.Epics()` は **open のみ**（task の epic 付け替えリストがこの slice を**添字で引く**ので、closed が混じると終わった箱を行き先に出し、以降の id が全部ずれる。slice パネルは id で引くが、終わった箱を既定で並べない理由は同じ）、`Board.EpicsAll()` が全部、`Board.Epic(id)` は closed も解決する。closed を持つ理由は2つ — dep が「閉じた箱」なのか「解決できない id」なのか区別できること、`epic reopen` に指す先ができること。 |
| **slice パネル** | `s` で開く左パネル（`slicemode.go`）。軸は repo / label / epic（既定は **open な box のみ**。`z` で closed 込みに広げる — dep map の scope 切替と同じキー・同じ語で、**前のセッションで閉じた箱に辿り着く唯一の道**。closed 行は `v` 印。epic 行は store の progress/stuck つき + `▶` active / `◆` pinned。epic dep が open な box を待つ間は `→N` — furrow 導出の `open_deps` をそのまま数える。「open after those close」の情報エッジで、強制ではない）。選択 = -q term の発行で、typed filter と AND 合成（GH の slice 仕様）。radio 動作（再選択で解除・軸切替で解除）。パネルを閉じても選択は残り、filter bar に `slice <term>` として見える。`g`/`G` で端へ。epic 軸では `m` で epic オーバーレイ・`A` で新規 box（他の軸では**理由を述べて断る** — dead key を作らない）。キーの告知はパネルの note が唯一の場所（modal なので `?` が打てず HelpSections も modal を載せない）なので、軸切替のたびに書き直す。 |
| **epic オーバーレイ** | slice パネルの epic 軸から `m` で開く box 管理 modal（`epicmode.go`）。`furrow epic add/set/activate/deactivate/dep` 相当。行は title / goal / active / standing / pinned / labels / repos / deps / meta で、上に furrow 導出の `d/t done` と STUCK（編集不能なので行にしない）。書き込みは全部 **store-first** — 盤面は着地まで古い値のまま見せ、note が「何を待っているか」を言う。in-flight 中の二度押しは拒否する（行がまだ書き込み前の値なので、二度目は見えていない盤面を狙うことになる。furrow 側は同じ box の再 activate を exit 0・`changed:[]` で受けるので、拒否の理由は furrow ではなく画面）。`active` 行は押す**前**に前提条件を出す（`no — slot held by <id>` / `no — attach a repo first`）— furrow は repo ごとの枠を奪わず拒否するので、exit 2 が初耳になってはいけない。activate は `--reason` の入力が確認も兼ね（本文の activation log に残る）、deactivate は確認ステージ + furrow の `previous` 提案を note に出す。`closed` 行は **1 行で `done`/`reopen` の両方**を担う（箱の状態を読んでどちらの verb かを決める。2行にすると常にどちらかが死に行になる）。confirm 段は furrow が言わないことを言う — **furrow は member が open のままの箱を exit 0 で閉じる**ので、残数を出すのはこの行だけの仕事。`reopen` の文言は furrow 自身の「OPEN and INACTIVE」で、活性化は連鎖しない。`done` した箱は `Epic()` が解決し続けるのでオーバーレイは自分の書き込みで閉じず、`reopen` が1キーで届く。 |
| **sort（Table）** | `o` で canonical → updated → created → value → effort → due を循環（各キーは自然な向きで入り、再押しで昇降反転）。現在地は対応列ヘッダの `▲▼` + フィルタバーの `sort <key> ▲▼` 常時表示（created / effort は列が無いので後者のみ）。ソート可能なヘッダセルのクリックでも同じ（同一セル再クリックで反転・`lane` で canonical へ復帰）。並びは同一スナップショットへのローカル安定ソートで、未設定値（due 無し等）は**両方向とも末尾**。ソート中は `K`/`J` の並べ替えを拒否（GH 同）。task 起票時の指定は `s` だったが slice パネルと衝突するため `o`。 |
| **canonical（順）** | Table の既定並び = 盤面そのもの（lane 順 → lane 内 priority 順）。field ソートの不在であり、方向を持たない。 |
| **note 追記（`n`）** | 選択タスクの body に1段落追記する入力（`furrow note` 相当 — 追記 + `updated` 前進を1コマンドで）。edit overlay の input stage を直接開く軽経路で、`e` の $EDITOR 全開はそのまま別に残る。esc/空 ⏎ は何も足さずに閉じ、適用も1段落ごとに閉じる。 |
| **quick add** | `a` で開く起票 modal（`addmode.go`）。`furrow add -s <フォーカス列>` に写像し、適用中 filter の単一値 `label:`/`epic:`/`repo:`（と `is:draft`）を継承（チップ表示 — 黙って付けない。GH の filtered-metadata 継承則）。確定後は再読 → 新カードを選択（filter が隠すなら pin）。 |
| **inline トークン（quick add）** | タイトル行に混ぜて打つ詳細指定（`addparse.go`）。`value:4 effort:2 due:+1d dep:t-x check:"…" ref:… is:draft` = `furrow add` の同名 flag に写像し、-q と重なる語彙（value/due/is:draft）は同綴り。`is:` の他の値は導出状態で起票に stamp できないので理由つき拒否。引用符が特別なのは**2箇所だけ** — 語頭（その語は常にタイトル文字列 = `value:` を含むタイトルの逃げ道）と token key の `:` 直後（空白入り check を運ぶ）— それ以外は文字どおりで、`Don't` も `彼は"これ"` も無傷で store に届く。継承側キー（`label:`/`epic:`/`repo:`）と focus 由来キー（`status:`/`lane:` — 起票先はフォーカス列）は黙殺せず**各自の理由つきで拒否**。チップ行に毎キー live echo し、不正トークン（数値外・1..5 外・due 文法・ref の CSV 禁字含む）は ⚠ 行 + modal 内拒否（行は生存）。複数行貼り付け起票（`--stdin` 相当）は対象外 — CLI が適切。 |
| **draft** | repo を1つも attach していないタスク — furrow の定義そのまま（`add --draft` で生まれ、`furrow ls` は既定で隠す）。ridge は常に読み（load の空 `-r`）常に出す: カード/Table は repo 枠に dim の `draft`、peek/graph は `draft (no repo)`。絞り込みは `is:draft` パススルー。起票は inline トークン `is:draft` か filter `is:draft` 下の継承（draft ビューでの素の起票は repo 付きで生まれて**その場から消える**ので、継承が既定）。`--draft` は `-r` と衝突（furrow の拒否）で、継承 repo との衝突は ⚠ 行 + modal 内拒否。**昇格（promote）= 編集メニューの repos attach**（`furrow repo --add`）で、draft 専用の操作は無い。 |
| **pin** | フィルタで隠れている blocker へジャンプしたとき、そのカードだけ一時的に盤面へ差し込むこと。飛んだ先が空振りにならないようにする。 |

## 表示要素

| 記号 | 意味 |
|---|---|
| `▸` | **actionable** — next レーンにあり、すべての依存が完了済み（＝今すぐ着手できる）。 |
| `x` / `x1` | **blocked** — 未完了の blocker がある（数字はその件数）。**隠さず印を付ける**（隠すのは `furrow next` の役目）。 |
| `▶` / `◆` / `v` | slice パネルの epic 行の lifecycle 印。`▶` = その repo が今それで作業している box（`furrow brief` と同じ字）、`◆` = pinned、`v` = closed（`z` で広げたときだけ出る。印は排他ではない — furrow は close 時に `active` は落とすが `pinned` は残す（実測）ので `v ◆` は正当な並び）。どれも `epic ls --all --json` の `active`/`pinned`/`closed` をそのまま出す。 |
| `▤` | **epic チップ**。epic は lane を持たない別エンティティ（`EpicInfo`）で、カードには所属 epic のタイトルを解決して表示する。epic が stuck なら warn 色。peek には `(done/total)` と STUCK、epic が open な dep を待つ間は resolved な `epic waits on` 行（open は `id (d/t) title`・stuck なら `id (d/t) STUCK title`（warn 色）・furrow が `open_deps` から解決済みの dep は、**盤面がその箱を closed として持っていれば `(closed)`・持っていなければ furrow のより弱い語 `(satisfied)`**。open/満了の判定は furrow 導出値で、ridge は再計算しない）。 |
| `v` | done。 |
| `◆`（Roadmap の行内） / `┊` | due の位置（overdue = danger 色・today と同セル = warn 色）/ today の縦線。窓の外へ pan された `◆` は行端の `▸`/`◂` になる — 日付付きの行が無日付に見えてはいけない。 |
| `[0/7]` | チェックリストの進捗。 |
| `v5 e4` | value / effort（各 1..5）。 |
| `◉ focus` | Graph の起点ノード。 |
| `↕ both directions` | Graph で、起点の上流にも下流にも現れるノード（left-right では `↔`）。箱幅が足りないときは `◉ focus` → repo チップ → この印の順に落ちる — id とレーンだけは必ず残す。 |
| `↩` | 既出ノード（同じノードに2経路で到達した＝DAG である印）。ツリー表示で重複を避ける。 |

## 内部

| 用語 | 意味 |
|---|---|
| **measurer** | カード高さのキャッシュ。**フレームを跨いで持つ**（フレーム単位だと 658タスクで 36ms/frame）。`recompute()` で破棄。 |
| **ego-graph**（起点グラフ） | あるタスクから N ホップ以内の依存部分グラフ。実データでは最大12ノード・最大5段・1段の最大幅4。 |
| **hop radius** | ego-graph を何ホップまで辿るか。`z` / `1` `2` `3` `0` で切替。Graph のもう1つのつまみが **orientation**（`o`）。 |
| **re-root** | Graph 上のノードを新しい起点にすること（`Enter`）。「読む」ではなく「歩く」ための操作で、静止画にはできない。 |
| **`-dump`** | TTY 無しで1フレームを標準出力に書いて終了するフラグ。headless 検証の土台。 |
| **`-demo`** | 手では捉えにくい一時状態（drag 中・edit の sub-editor・失敗表示など）を1フレームに固定して `-dump` する。名前一覧の正本は `ui.DemoNames`（`ridge -h` もそこから出る。ここに写しを置いたら2度古くなった）。 |
| **`-readonly`** | fixture を schema gate で read-only にした盤面を出す。model の状態ではなく store の性質なので `-demo` ではなくフラグ。書き込みは全部拒否される。 |
| **`-debuglog`** | 操作履歴の JSONL 記録（1 イベント 1 行・追記 open・各セッションの先頭は `session/start`）。層は input / mode / apply / persist / status の 5 つ。hook 点は 3 つ — `Update` の単一経路（input・mode・apply・persist の queue イベント）/ store の perf hook（exec。goroutine 跨ぎ、mutex で直列化）/ `note`/`fail` の funnel（status）。打鍵は全文残る — 記録しないのは body 本文のみ。`-perflog`（latency 計測の TSV）とは役割が別で併用可。`-dump`/`-benchload` とは組めない（記録すべきセッションが無い）。 |
