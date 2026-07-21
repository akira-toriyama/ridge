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
| **Provider** | ridge がタスクを読み書きする唯一の口（interface）。現在は mock 実装のみ。実 furrow 実装がここに入る。 |

## ビュー

| 用語 | 意味 |
|---|---|
| **Board** | カンバン。レーンが列、カードがタスク。既定のビュー。 |
| **Table** | 平坦な表形式のビュー。`v` で Board と切り替え。 |
| **Graph** | **依存グラフ**。1タスクを起点に、上が blocker・下が「閉じると動き出すもの」の階層図。`S` / `Shift+Space`。 |
| **peek**（詳細ペイン） | 選択中タスクの詳細を横に出すオーバーレイ。`Space`。 |
| **依存マップ** | *(未実装)* 全依存クラスタを一画面で俯瞰するビュー。Graph が「1タスク起点」なのに対し、こちらは「全体」。 |

## 操作

| 用語 | 意味 |
|---|---|
| **move mode** | GitHub Projects 由来の並べ替え操作。`Enter` で持ち上げ → 矢印で移動 → `Enter` 確定 / `Esc` 取消。furrow の sparse priority 並べ替えに 1:1 対応。 |
| **drag**（DnD） | マウスでカードを掴んで運ぶ。move mode のマウス版で、確定経路は同一（`commitMove`）。 |
| **ghost** | ドラッグ中にカーソルに追従する半透明のカード。lipgloss の Layer（Z=99）。 |
| **drop indicator** | ドロップ先を示す印。Layer だが **ID を持たない**ので `Compositor.Hit` に拾われない（＝クリックを吸わない）。 |
| **drag threshold** | 「掴んだ」と判定するまでの最小移動距離。これが無いと1セルの震えが移動として確定してしまう（lazygit が実際に踏んだバグ）。 |
| **jump-to-blocker** | `>` で最初の未完了 blocker へカーソルを飛ばし、`<` で戻る。スタックなので何段でも潜れる。 |
| **pin** | フィルタで隠れている blocker へジャンプしたとき、そのカードだけ一時的に盤面へ差し込むこと。飛んだ先が空振りにならないようにする。 |

## 表示要素

| 記号 | 意味 |
|---|---|
| `▸` | **actionable** — next レーンにあり、すべての依存が完了済み（＝今すぐ着手できる）。 |
| `x` / `x1` | **blocked** — 未完了の blocker がある（数字はその件数）。**隠さず印を付ける**（隠すのは `furrow next` の役目）。 |
| `▤` | **epic**（container）。`6/18` のように子タスクの進捗を伴う。 |
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
| **`-demo`** | ジェスチャ途中の状態（`move` / `drag` / `graph` / `help`）を1フレームに固定して `-dump` する。 |
