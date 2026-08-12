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
| **Provider** | ridge がタスクを読み書きする唯一の口（interface）。port は `internal/board` が宣言し、adapter は `internal/store/furrowstore`（`furrow` を exec する実装）と `internal/store/memstore`（fixture）の 2 つ。mutation は Persist 契約 — **Model がローカル適用済みの変更を store に記録するだけ**で、適用そのものはしない。 |
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
| **graph** | ⟨GRAPH⟩ | mode enum 外だがキーボードを専有する full-screen view — 実質 7 つ目。 |
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
| **編集メニュー（edit overlay）** | peek/Table の `Enter`／`m` で開く field 編集 modal（`editmode.go`）。menu → sub-editor（1..5 picker / toggle list / text input / checklist カーソル）の2段。適用は楽観的 + persist キュー、`furrow set` 相当は 1 write に合成。 |
| **slice パネル** | `s` で開く左パネル（`slicemode.go`）。軸は repo / label / epic（epic 行は store の progress/stuck つき）。選択 = -q term の発行で、typed filter と AND 合成（GH の slice 仕様）。radio 動作（再選択で解除・軸切替で解除）。パネルを閉じても選択は残り、filter bar に `slice <term>` として見える。 |
| **sort（Table）** | `o` で canonical → updated → created → value → effort → due を循環（各キーは自然な向きで入り、再押しで昇降反転）。現在地は対応列ヘッダの `▲▼` + フィルタバーの `sort <key> ▲▼` 常時表示（created / effort は列が無いので後者のみ）。ソート可能なヘッダセルのクリックでも同じ（同一セル再クリックで反転・`lane` で canonical へ復帰）。並びは同一スナップショットへのローカル安定ソートで、未設定値（due 無し等）は**両方向とも末尾**。ソート中は `K`/`J` の並べ替えを拒否（GH 同）。task 起票時の指定は `s` だったが slice パネルと衝突するため `o`。 |
| **canonical（順）** | Table の既定並び = 盤面そのもの（lane 順 → lane 内 priority 順）。field ソートの不在であり、方向を持たない。 |
| **quick add** | `a` で開く起票 modal（`addmode.go`）。`furrow add -s <フォーカス列>` に写像し、適用中 filter の単一値 `label:`/`epic:`/`repo:` を継承（チップ表示 — 黙って付けない。GH の filtered-metadata 継承則）。確定後は再読 → 新カードを選択（filter が隠すなら pin）。 |
| **pin** | フィルタで隠れている blocker へジャンプしたとき、そのカードだけ一時的に盤面へ差し込むこと。飛んだ先が空振りにならないようにする。 |

## 表示要素

| 記号 | 意味 |
|---|---|
| `▸` | **actionable** — next レーンにあり、すべての依存が完了済み（＝今すぐ着手できる）。 |
| `x` / `x1` | **blocked** — 未完了の blocker がある（数字はその件数）。**隠さず印を付ける**（隠すのは `furrow next` の役目）。 |
| `▤` | **epic チップ**。epic は lane を持たない別エンティティ（`EpicInfo`）で、カードには所属 epic のタイトルを解決して表示する。epic が stuck なら warn 色。peek には `(done/total)` と STUCK。 |
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
| **`-demo`** | 手では捉えにくい一時状態（`move` / `drag` / `add` / `edit` / `graph` / `help` / `slice` / `sort` / `filter` / `fail`）を1フレームに固定して `-dump` する。正本は `ui.DemoNames`。 |
| **`-readonly`** | fixture を schema gate で read-only にした盤面を出す。model の状態ではなく store の性質なので `-demo` ではなくフラグ。書き込みは全部拒否される。 |
