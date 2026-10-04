# task-manager

自分専用の学習・作業タスク管理アプリです。ドラクエ風（レトロ RPG 風）の画面でタスクを片づけ、時間と振り返りを記録します。
すべての機能は REST API でも使えるので、ほかのアプリからも同じデータを操作できます。要件と決めた点は [SPEC.md](SPEC.md) にまとめています。

- **タスクボード**: みちゃくしゅ / しんこうちゅう / クリア の3列。クリアしたタスクは取り消し線で消える
- **しんちょく**: 完了率のゲージと、目標時間・実績時間の合計
- **タイマーと時間記録**: カードごとに目標時間を決め、タイマーや手入力で実績を記録。目標を超えると強調
- **ふりかえり**: クリアしたら振り返りを書き、「まなんだこと / できなかったこと」を自動で抽出。期間ごとに一覧できる
- **ピコ**: クリア数で育つドット絵のキャラクター（Lv1 タマゴ → Lv5 キング）。進捗やタイマーに合わせて様子が変わる

## クイックスタート

[mise](https://mise.jdx.dev/) と make が必要です（Go などのバージョンは `.mise.toml` で固定）。

```sh
mise install   # Go・golangci-lint・shellcheck を入れる
make start     # api / insight / web をバックグラウンドで起動し、接続先を表示
```

```text
● api        起動中  http://127.0.0.1:8080  (PID 12345)
● insight    起動中  http://127.0.0.1:8081  (PID 12346)
● web        起動中  http://127.0.0.1:3000  (PID 12347)

ブラウザで web の URL を開いてください。停止: make stop / ログ: make logs
```

ブラウザで **http://127.0.0.1:3000** を開くとボードが表示されます。止めるときは `make stop` です。
DB（SQLite）は初回起動時に `data/taskboard.db` に作られ、スキーマも自動で用意されます。

## 画面でできること

| 画面 | できること |
|---|---|
| ボード（`/`） | タスクの追加・へんしゅう・すてる、ステータスの変更（とりかかる / クリアする / やりなおす） |
| カード | タイマーの かいし / ていし（計測中は経過時間が進む）、じかんの きろく（区間の一覧・「今までの N 分」の追加・削除）、ふりかえる |
| ふりかえり（`/reflections`） | きょう / この1しゅうかん / この1かげつ / ぜんぶ の期間で、まなんだこと・できなかったことをタスク名つきで一覧 |

- 「クリアする」と、そのカードで振り返りの入力欄が開きます。「＋」で始めた行は まなんだこと、「−」で始めた行は できなかったこと になり、「理解した」「まだ」などの言葉でも分けます
- 操作の結果は画面下のメッセージウィンドウで知らせます（「＊ 「〇〇」が あらわれた！」「〜を やっつけた！」など。文言は `web/internal/board/messages.go`）
- **ピコ**（ボードの左上）: クリア数 0 / 5 / 15 / 30 / 50 でレベルアップ。タスクが無いと ねむり、途中は あるき、全部クリアすると よろこび、タイマーで計測中は たたかう。クリアするとジャンプします
- 動きを減らす設定（OS の視差効果を減らす）では、アニメーションを止めます
- フォントは [DotGothic16](https://github.com/fontworks-fonts/DotGothic16)（SIL Open Font License 1.1）を同梱しているので、オフラインでも同じ見た目です

## 構成

画面アプリと API は別々のプロセスです。DB を持つのは `api` だけで、`web` も「API を使うアプリ」の1つとして HTTP で API を呼びます。

```
ブラウザ ──HTML──▶ web (:3000) ──HTTP/JSON──▶ api (:8080) ──▶ SQLite
                                               │  ▲
                                   HTTP/JSON   ▼  │ HTTP/JSON
                                     insight (:8081)    他のアプリ
                                     抽出 API（DB なし）
```

| サービス | ディレクトリ | 役割 | 既定アドレス |
|---|---|---|---|
| `api` | `api/` | Task API（`/api/v1`）。タスク・時間記録・振り返りを SQLite に保存 | http://127.0.0.1:8080 |
| `web` | `web/` | 画面（Go の html/template + htmx）。DB を持たず api を呼ぶ | http://127.0.0.1:3000 |
| `insight` | `insight/` | 振り返りから「学んだこと / できなかったこと」を抽出する API | http://127.0.0.1:8081 |

各ディレクトリは独立した Go モジュールで、`go.work` でまとめています。3つのサービスで共通の部品は `shared/` にあります。

## コマンド

`make`（または `make help`）で一覧を表示できます。どのコマンドも `mise exec` 経由で動くので、シェルで mise を有効にしていなくても同じバージョンが使われます。

| コマンド | 内容 |
|---|---|
| `make start` | ビルドしてバックグラウンドで起動し、接続先を表示（起動済みなら何もしない） |
| `make stop` | 停止（正常終了を最大15秒待ち、終わらなければ強制終了） |
| `make restart` | 再ビルドして再起動（コードを変更したとき） |
| `make status` | 起動状態と接続先を表示 |
| `make logs` | ログを表示し続ける（Ctrl-C で終了。サービスは止まらない） |
| `make test` | 全モジュールのテスト（`-race`、タイムアウト 120 秒） |
| `make lint` | golangci-lint（モジュールごと）と shellcheck |
| `make fmt` / `make tidy` | コード整形 / 各モジュールの `go.mod` の整理 |
| `make build` | `bin/` にビルド |

起動は api → insight → web の順、停止はその逆順です。PID とログは `.run/` に保存されます（ログは起動のたびに作り直し）。

## 設定

すべて任意です。変えるときは `.env.example` を `.env` にコピーして編集すると、mise が自動で読み込みます。
待ち受けアドレスはループバック（`127.0.0.1` / `::1` / `localhost`）のみで、`0.0.0.0` などを指定すると起動エラーになります（画面にはログインが無いため、LAN に公開しない）。

| 環境変数 | 既定値 | 説明 |
|---|---|---|
| `API_ADDR` | `127.0.0.1:8080` | api の待ち受けアドレス |
| `API_DB_PATH` | `data/taskboard.db` | SQLite ファイルのパス |
| `API_KEY` | （なし） | 設定すると `/api/v1` に `Authorization: Bearer <キー>` を要求する（16文字以上） |
| `API_CORS_ORIGINS` | （なし） | ブラウザから API を直接呼ぶアプリのオリジン（カンマ区切り。例: `http://localhost:5173`）。CSRF 対策の信頼オリジンにもなる |
| `API_INSIGHT_URL` | `http://127.0.0.1:8081` | api が振り返りの抽出を頼む insight の URL |
| `WEB_ADDR` | `127.0.0.1:3000` | web の待ち受けアドレス |
| `WEB_API_URL` | `http://127.0.0.1:8080` | web が呼ぶ api の URL |
| `WEB_API_KEY` | （なし） | `API_KEY` を設定したら同じ値を指定する |
| `INSIGHT_ADDR` | `127.0.0.1:8081` | insight の待ち受けアドレス |

## API

API の仕様は OpenAPI 3.1 で書いた **[`api/openapi.yaml`](api/openapi.yaml)**（insight は [`insight/openapi.yaml`](insight/openapi.yaml)）が正です。
起動中の api からも `GET /api/v1/openapi.yaml` で取得できるので、クライアントの生成やツールでの確認に使えます。
仕様書と実装のパス・メソッドが一致していることは、テスト（`make test`）で毎回確かめています。

```sh
# 作成（201 と Location ヘッダーを返す）
curl -X POST http://127.0.0.1:8080/api/v1/tasks \
  -H 'Content-Type: application/json' -d '{"title": "Go を学ぶ", "estimated_min": 90}'

# 一覧（?status=todo / doing / done で絞り込み）
curl 'http://127.0.0.1:8080/api/v1/tasks?status=todo'

# タイマー（同時に動かせるのは全体で1つ。完了したタスクでは開始できない）・あとから区間を記録
curl -X POST http://127.0.0.1:8080/api/v1/tasks/1/timer/start
curl -X POST http://127.0.0.1:8080/api/v1/tasks/1/timer/stop
curl -X POST http://127.0.0.1:8080/api/v1/tasks/1/time-entries -H 'Content-Type: application/json' \
  -d '{"started_at": "2026-10-03T19:00:00+09:00", "ended_at": "2026-10-03T19:45:00+09:00"}'

# 完了にする（部分更新。completed_at を記録し、計測中のタイマーは止まる）
curl -X PATCH http://127.0.0.1:8080/api/v1/tasks/1 -H 'Content-Type: application/json' -d '{"status": "done"}'

# 振り返り（insight で抽出）
curl -X PUT http://127.0.0.1:8080/api/v1/tasks/1/reflection -H 'Content-Type: application/json' \
  -d '{"body": "goroutine を理解した。\n- テストの書き方"}'

# 進捗と時間の合計・期間内の振り返り
curl http://127.0.0.1:8080/api/v1/stats/summary
curl 'http://127.0.0.1:8080/api/v1/reflections?from=2026-10-01T00:00:00%2B09:00'
```

| パス | メソッド | 内容 |
|---|---|---|
| `/api/v1/tasks` | GET / POST | タスクの一覧 / 作成 |
| `/api/v1/tasks/{id}` | GET / PATCH / DELETE | 取得 / 部分更新 / 削除 |
| `/api/v1/stats/summary` | GET | 進捗と目標・実績時間の合計 |
| `/api/v1/tasks/{id}/timer/start` ・ `/timer/stop` | POST | タイマーの開始・停止 |
| `/api/v1/tasks/{id}/time-entries` | GET / POST | 時間記録の一覧 / 手動追加 |
| `/api/v1/time-entries/{id}` | GET / PATCH / DELETE | 時間記録の取得 / 修正 / 削除 |
| `/api/v1/tasks/{id}/reflection` | GET / PUT / DELETE | 振り返りの取得 / 登録（抽出）/ 削除 |
| `/api/v1/tasks/{id}/reflection/extract` | POST | 抽出のやり直し |
| `/api/v1/reflections` | GET | 期間内の振り返り（`from` / `to` は RFC3339） |
| `/api/v1/openapi.yaml` | GET | 仕様書 |
| `/healthz` | GET | 死活監視（DB に接続できるか） |

おもな約束ごと（詳しくは仕様書）:

- 日時は RFC3339。応答は UTC、リクエストはタイムゾーン付きなら何でも受け付ける。未設定の値は `null`、一覧は 0 件でも空配列
- PATCH は部分更新。`"estimated_min": null` で目標時間を未設定に戻せる（ほかの項目の `null` は「変更しない」）
- タスクを完了にすると、計測中のタイマーは自動で止まる。振り返りは insight が止まっていても保存され、`extract_status` が `failed` になる（`/reflection/extract` でやり直し）
- エラーは `{"error": {"code", "message", "details"}}`。分岐には `code` を使う（`message` は人が読むための文）

| code | HTTP | 意味 |
|---|---|---|
| `invalid_json` | 400 | JSON の構文・型の誤り、未知のフィールド |
| `unauthorized` | 401 | API キーが無い・違う |
| `not_found` | 404 | 対象または API のパスが無い |
| `method_not_allowed` | 405 | 使えないメソッド（`Allow` ヘッダーに使えるメソッド） |
| `timer_already_running` / `timer_not_running` / `task_completed` / `entry_running` | 409 | タイマーや時間記録の状態の衝突 |
| `payload_too_large` | 413 | ボディが 1MB を超えている |
| `unsupported_media_type` | 415 | `Content-Type: application/json` でない |
| `validation_failed` | 422 | 入力値の検証エラー。`details[].code` は `required` / `too_long` / `invalid` / `out_of_range` / `not_after_start` / `in_future` |
| `internal_error` | 500 | サーバー内部のエラー |

API の手前の防御で拒否されたときは平文で返ります: **403** = 他サイトのブラウザからの送信（CSRF 対策）、**421** = `Host` が localhost / 127.0.0.1 / ::1 以外（DNS リバインディング対策）。

### 抽出 API（insight）

DB を持たない抽出だけの API で、ほかのアプリからも単体で使えます。

```sh
curl -X POST http://127.0.0.1:8081/api/v1/extract -H 'Content-Type: application/json' \
  -d '{"text": "goroutine の使い方を理解した。テストの書き方はまだ曖昧。"}'
# {"learned":["goroutine の使い方を理解した"],"not_learned":["テストの書き方はまだ曖昧"]}
```

分類はルールベースで、① 見出し（「学んだこと」「課題」など）の下の行 → ② `+` / `-` で始まる行 → ③ キーワード（否定を先に判定）の順に当てはめます（`insight/internal/extract`）。

## 困ったとき

| 症状 | 原因と対処 |
|---|---|
| `make start` が「ポート 8080 は次のプロセスが使用中です」で止まる | 表示された PID のプロセスがポートを使っています。止めてよいものなら `kill <PID>` してから `make start`。避けるなら `.env` の `*_ADDR` でポートを変える |
| 画面に「API サーバーと つうしん できない！」 | api が止まっています。`make status` で確かめ、`make restart` |
| カードに「ちゅうしゅつに しっぱいした！」 | 振り返りの保存時に insight が止まっていました。`make status` で insight を確かめてから、カードの「もういちど」 |
| 画面に「API キーが あわない！」 | `API_KEY` と `WEB_API_KEY` を同じ値にする |
| 別のアプリから呼ぶと 403 / 421 | ブラウザから呼ぶなら `API_CORS_ORIGINS` にオリジンを登録。`Host` は `127.0.0.1` か `localhost` で呼ぶ |
| 動きがおかしいときのログ | `make logs`（`.run/*.log`） |

## 開発

- テスト: `make test`（ドメインはテーブル駆動、HTTP は `httptest`、DB はテストごとの一時 SQLite、時刻は固定）。仕様書と実装のパス・メソッドの一致もテストで確かめる
- 静的解析: `make lint`。仕様書の書き方の検査は `pnpm --package=@redocly/cli dlx redocly lint api/openapi.yaml insight/openapi.yaml`
- CI（GitHub Actions）は `.mise.toml` と同じバージョンで `make lint` / `make test` を実行する
- コミットと PR のタイトルは Conventional Commits（`feat:` / `fix:` / `docs:` など）。main にマージすると、`feat` は minor、`fix` は patch のリリースが自動で作られる

```
.
├── api/        Task API（cmd/api、internal/{config,db,task,httpapi,insightclient}、openapi.yaml）
├── web/        画面（cmd/web、internal/{config,taskclient,board,character}）
├── insight/    抽出 API（cmd/insight、internal/{config,extract,handler}、openapi.yaml）
├── shared/     共通部品（httpserver: 起動・/healthz・防御、envconf: 設定、jsonapi: JSON 応答とエラー形式）
├── scripts/    バックグラウンド起動・停止（service.sh）
├── SPEC.md     要件定義と決めた点
├── go.work
└── Makefile
```
