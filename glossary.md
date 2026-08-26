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
| **Graph** | **依存グラフ**。1タスクを起点に、上が blocker・下が「閉じると動き出すもの」の階層図。`S` / `Shift+Space`。 |
| **peek**（詳細ペイン） | 選択中タスクの詳細を横に出すオーバーレイ。`Space`。 |
| **依存マップ** | *(未実装)* 全依存クラスタを一画面で俯瞰するビュー。Graph が「1タスク起点」なのに対し、こちらは「全体」。 |

## mode（キーボードの所有者）

現在 mode はタイトル行右端の ⟨…⟩ トークンで**常時**表示される（graph だけは自前のタイトル行
— Graph タブ + ⟨GRAPH⟩）。`?` help はこの mode 名で節分けされ、今いる mode の節に
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
| **slice パネル** | `s` で開く左パネル（`slicemode.go`）。軸は repo / label / epic（epic 行は store の progress/stuck つき + `▶` active / `◆` pinned。epic dep が open な box を待つ間は `→N` — furrow 導出の `open_deps` をそのまま数える。「open after those close」の情報エッジで、強制ではない）。選択 = -q term の発行で、typed filter と AND 合成（GH の slice 仕様）。radio 動作（再選択で解除・軸切替で解除）。パネルを閉じても選択は残り、filter bar に `slice <term>` として見える。`g`/`G` で端へ。epic 軸では `m` で epic オーバーレイ・`A` で新規 box（他の軸では**理由を述べて断る** — dead key を作らない）。キーの告知はパネルの note が唯一の場所（modal なので `?` が打てず HelpSections も modal を載せない）なので、軸切替のたびに書き直す。 |
| **epic オーバーレイ** | slice パネルの epic 軸から `m` で開く box 管理 modal（`epicmode.go`）。`furrow epic add/set/activate/deactivate/dep` 相当。行は title / goal / active / standing / pinned / labels / repos / deps / meta で、上に furrow 導出の `d/t done` と STUCK（編集不能なので行にしない）。書き込みは全部 **store-first** — 盤面は着地まで古い値のまま見せ、note が「何を待っているか」を言う。in-flight 中の二度押しは拒否する（行がまだ書き込み前の値なので、二度目は見えていない盤面を狙うことになる。furrow 側は同じ box の再 activate を exit 0・`changed:[]` で受けるので、拒否の理由は furrow ではなく画面）。`active` 行は押す**前**に前提条件を出す（`no — slot held by <id>` / `no — attach a repo first`）— furrow は repo ごとの枠を奪わず拒否するので、exit 2 が初耳になってはいけない。activate は `--reason` の入力が確認も兼ね（本文の activation log に残る）、deactivate は確認ステージ + furrow の `previous` 提案を note に出す。**`done`/`reopen` は無い** — `reopen` が pin 済み furrow release（v4.0.0）に存在せず、`done` だけ出すと epic 読みが open-only なので片道扉になる。両方 t-sq02（closed 母集団を持つ側）へ。 |
| **sort（Table）** | `o` で canonical → updated → created → value → effort → due を循環（各キーは自然な向きで入り、再押しで昇降反転）。現在地は対応列ヘッダの `▲▼` + フィルタバーの `sort <key> ▲▼` 常時表示（created / effort は列が無いので後者のみ）。ソート可能なヘッダセルのクリックでも同じ（同一セル再クリックで反転・`lane` で canonical へ復帰）。並びは同一スナップショットへのローカル安定ソートで、未設定値（due 無し等）は**両方向とも末尾**。ソート中は `K`/`J` の並べ替えを拒否（GH 同）。task 起票時の指定は `s` だったが slice パネルと衝突するため `o`。 |
| **canonical（順）** | Table の既定並び = 盤面そのもの（lane 順 → lane 内 priority 順）。field ソートの不在であり、方向を持たない。 |
| **note 追記（`n`）** | 選択タスクの body に1段落追記する入力（`furrow note` 相当 — 追記 + `updated` 前進を1コマンドで）。edit overlay の input stage を直接開く軽経路で、`e` の $EDITOR 全開はそのまま別に残る。esc/空 ⏎ は何も足さずに閉じ、適用も1段落ごとに閉じる。 |
| **quick add** | `a` で開く起票 modal（`addmode.go`）。`furrow add -s <フォーカス列>` に写像し、適用中 filter の単一値 `label:`/`epic:`/`repo:` を継承（チップ表示 — 黙って付けない。GH の filtered-metadata 継承則）。確定後は再読 → 新カードを選択（filter が隠すなら pin）。 |
| **inline トークン（quick add）** | タイトル行に混ぜて打つ詳細指定（`addparse.go`）。`value:4 effort:2 due:+1d dep:t-x check:"…" ref:…` = `furrow add` の同名 flag に写像し、-q と重なる語彙（value/due）は同綴り。`"`/`'` で空白入り check を運び、**引用で始まる語は常にタイトル文字列**（`value:` を含むタイトルの逃げ道）。継承側キー（`label:`/`epic:`/`repo:`/`status:`/`lane:`）は黙殺せず**理由つきで拒否**。チップ行に毎キー live echo し、不正トークンは ⚠ 行 + modal 内拒否（行は生存）。複数行貼り付け起票（`--stdin` 相当）は対象外 — CLI が適切。 |
| **pin** | フィルタで隠れている blocker へジャンプしたとき、そのカードだけ一時的に盤面へ差し込むこと。飛んだ先が空振りにならないようにする。 |

## 表示要素

| 記号 | 意味 |
|---|---|
| `▸` | **actionable** — next レーンにあり、すべての依存が完了済み（＝今すぐ着手できる）。 |
| `x` / `x1` | **blocked** — 未完了の blocker がある（数字はその件数）。**隠さず印を付ける**（隠すのは `furrow next` の役目）。 |
| `▶` / `◆` | slice パネルの epic 行の lifecycle 印。`▶` = その repo が今それで作業している box（`furrow brief` と同じ字）、`◆` = pinned。どちらも `epic ls --json` の `active`/`pinned` をそのまま出す。 |
| `▤` | **epic チップ**。epic は lane を持たない別エンティティ（`EpicInfo`）で、カードには所属 epic のタイトルを解決して表示する。epic が stuck なら warn 色。peek には `(done/total)` と STUCK、epic が open な dep を待つ間は resolved な `epic waits on` 行（open は `id (d/t) title`・stuck なら `id (d/t) STUCK title`（warn 色）・furrow が `open_deps` から解決済みの dep は `(satisfied)`。open/満了の判定は furrow 導出値で、ridge は再計算しない）。 |
| `v` | done。 |
| `[0/7]` | チェックリストの進捗。 |
| `v5 e4` | value / effort（各 1..5）。 |
| `◉ focus` | Graph の起点ノード。 |
| `↕ both directions` | Graph で、起点の上流にも下流にも現れるノード。 |
| `↩` | 既出ノード（同じノードに2経路で到達した＝DAG である印）。ツリー表示で重複を避ける。 |

## 内部

| 用語 | 意味 |
|---|---|
| **measurer** | カード高さのキャッシュ。**フレームを跨いで持つ**（フレーム単位だと 658タスクで 36ms/frame）。`recompute()` で破棄。 |
| **ego-graph**（起点グラフ） | あるタスクから N ホップ以内の依存部分グラフ。実データでは最大12ノード・最大5段・1段の最大幅4。 |
| **hop radius** | ego-graph を何ホップまで辿るか。`z` / `1` `2` `3` `0` で切替。 |
| **re-root** | Graph 上のノードを新しい起点にすること（`Enter`）。「読む」ではなく「歩く」ための操作で、静止画にはできない。 |
| **cluster** | 依存グラフの連結成分。実データでは未完了分で9個、中央値2ノード。 |
| **`-dump`** | TTY 無しで1フレームを標準出力に書いて終了するフラグ。headless 検証の土台。 |
| **`-demo`** | 手では捉えにくい一時状態（drag 中・edit の sub-editor・失敗表示など）を1フレームに固定して `-dump` する。名前一覧の正本は `ui.DemoNames`（`ridge -h` もそこから出る。ここに写しを置いたら2度古くなった）。 |
| **`-readonly`** | fixture を schema gate で read-only にした盤面を出す。model の状態ではなく store の性質なので `-demo` ではなくフラグ。書き込みは全部拒否される。 |
| **`-debuglog`** | 操作履歴の JSONL 記録（1 イベント 1 行・追記 open・各セッションの先頭は `session/start`）。層は input / mode / apply / persist / status の 5 つ。hook 点は 3 つ — `Update` の単一経路（input・mode・apply・persist の queue イベント）/ store の perf hook（exec。goroutine 跨ぎ、mutex で直列化）/ `note`/`fail` の funnel（status）。打鍵は全文残る — 記録しないのは body 本文のみ。`-perflog`（latency 計測の TSV）とは役割が別で併用可。`-dump`/`-benchload` とは組めない（記録すべきセッションが無い）。 |
