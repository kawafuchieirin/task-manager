# task-manager

自分専用の学習・作業タスク管理アプリ。要件は [SPEC.md](SPEC.md) を参照。

画面アプリと API は別々のプロセスです。DB を持つのは `api` だけで、`web` も「API を使う他のアプリ」の1つとして HTTP で API を呼び出します。

```
ブラウザ ──HTML──▶ web (:3000) ──HTTP/JSON──▶ api (:8080) ──▶ SQLite
                                                ▲
                       他のアプリ ──HTTP/JSON────┘
                                         insight (:8081)  抽出 API（DB なし）
```

| サービス | ディレクトリ | 役割 | 既定アドレス |
|---|---|---|---|
| `api` | `api/` | Task API（`/api/v1`）。SQLite に保存 | http://127.0.0.1:8080 |
| `web` | `web/` | タスクボードの画面（htmx）。DB を持たず api を呼ぶ | http://127.0.0.1:3000 |
| `insight` | `insight/` | 振り返りから「学んだこと / できなかったこと」を抽出する API | http://127.0.0.1:8081 |

各ディレクトリは独立した Go モジュールで、`go.work` でまとめています。3つのサービスが共通で使う HTTP サーバーの起動処理や防御ミドルウェアは `shared/` にあります。

## セットアップ

[mise](https://mise.jdx.dev/) と make が必要です。Go・golangci-lint・shellcheck のバージョンは `.mise.toml` で固定し、
`make` の各コマンドは `mise exec` 経由で実行されます（シェルで mise を有効化していなくても同じバージョンが使われます）。

```sh
mise install
make start    # バックグラウンドで起動し、接続先を表示
```

```text
● api        起動中  http://127.0.0.1:8080  (PID 12345)
● insight    起動中  http://127.0.0.1:8081  (PID 12346)
● web        起動中  http://127.0.0.1:3000  (PID 12347)

ブラウザで web の URL を開いてください。停止: make stop / ログ: make logs
```

ブラウザで web の URL（既定は http://127.0.0.1:3000 ）を開くとボードが表示されます。画面はレトロ RPG 風（ドット文字・黒地に白枠のウィンドウ・▶ カーソル）で、言葉づかいも合わせています（例: タスクを追加すると「＊ 「〇〇」が あらわれた！」）。画面の文言は `web/internal/board/messages.go` にまとめています。
フォントは [DotGothic16](https://github.com/fontworks-fonts/DotGothic16)（SIL Open Font License 1.1、`web/internal/board/static/fonts/DotGothic16-OFL.txt`）を同梱しているので、オフラインでも同じ見た目になります。api が止まっている場合、画面には「API サーバーに接続できません」と表示されます。

| コマンド | 内容 |
|---|---|
| `make start` | ビルドしてバックグラウンドで起動し、接続先を表示（起動済みなら何もしない） |
| `make stop` | 停止（正常終了を最大15秒待ち、終わらなければ強制終了） |
| `make restart` | 再ビルドして再起動（コードを変更したとき） |
| `make status` | 起動状態と接続先を表示 |
| `make logs` | ログを表示し続ける（Ctrl-C で終了。サービスは止まらない） |

- PID とログは `.run/` に保存されます。ログは起動のたびに作り直されます
- ポートが使用中などで起動できなかった場合は、ログの末尾を表示し、起動途中のサービスも止めます
- ポートが使用中のときは、そのポートを使っているプロセス（PID・コマンド・起動時刻）と止め方も表示します。このリポジトリの `bin/` から起動された管理外のプロセス（以前の構成の残りなど）であれば、その旨も表示します

```text
ポート 8080 は次のプロセスが使用中です:
  PID 18833  /path/to/task-manager/bin/taskboard（起動: Sat Oct  3 22:31:51 2026）
このリポジトリの bin/ から起動された古いプロセスです（make stop の管理外。以前の構成の残りなど）。
止めてよいプロセスなら kill 18833 で停止してから、もう一度 make start を実行してください。
```
- 表示される接続先は、サーバーが実際に待ち受けているアドレスです（`API_ADDR` / `WEB_ADDR` / `INSIGHT_ADDR` の設定が反映されます）
- 起動は api → insight → web の順、停止はその逆順です

DB は初回起動時に `data/taskboard.db` に作成され、マイグレーションが自動で適用されます。

## 設定

すべて任意です。変更する場合は `.env.example` を `.env` にコピーして編集すると、mise が自動で読み込みます。

待ち受けアドレスはすべてループバック（`127.0.0.1` / `::1` / `localhost`）のみです。`0.0.0.0` などを指定すると起動エラーになります。

| 環境変数 | 既定値 | 説明 |
|---|---|---|
| `API_ADDR` | `127.0.0.1:8080` | api の待ち受けアドレス |
| `API_DB_PATH` | `data/taskboard.db` | SQLite ファイルのパス |
| `API_KEY` | （なし） | 設定すると `/api/v1` に `Authorization: Bearer <キー>` を要求する（16文字以上） |
| `API_CORS_ORIGINS` | （なし） | ブラウザから API を直接呼ぶ他のアプリのオリジン（カンマ区切り。例: `http://localhost:5173`）。web はサーバー間で呼ぶので登録不要 |
| `WEB_ADDR` | `127.0.0.1:3000` | web の待ち受けアドレス |
| `WEB_API_URL` | `http://127.0.0.1:8080` | web が呼び出す api の URL |
| `WEB_API_KEY` | （なし） | `API_KEY` を設定した場合に同じ値を指定する |
| `INSIGHT_ADDR` | `127.0.0.1:8081` | insight の待ち受けアドレス |

## 他のアプリから API を使う

画面（web）と同じデータを JSON の REST API（`/api/v1`）で操作できます。web 自身もこの API だけを使って動いています。

```sh
# 作成（201 と Location ヘッダーを返す）
curl -X POST http://127.0.0.1:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"title": "Go を学ぶ", "estimated_min": 90}'

# 一覧（status=todo / doing / done で絞り込み可）
curl 'http://127.0.0.1:8080/api/v1/tasks?status=todo'

# 完了にする（部分更新。completed_at が自動で記録される）
curl -X PATCH http://127.0.0.1:8080/api/v1/tasks/1 \
  -H 'Content-Type: application/json' -d '{"status": "done"}'

# タイマーの開始・停止（同時に動かせるのは全体で1つ）
curl -X POST http://127.0.0.1:8080/api/v1/tasks/1/timer/start
curl -X POST http://127.0.0.1:8080/api/v1/tasks/1/timer/stop

# 作業した区間をあとから記録（タイムゾーン付きの RFC3339）
curl -X POST http://127.0.0.1:8080/api/v1/tasks/1/time-entries \
  -H 'Content-Type: application/json' \
  -d '{"started_at": "2026-10-03T19:00:00+09:00", "ended_at": "2026-10-03T19:45:00+09:00"}'

# 進捗と時間の合計
curl http://127.0.0.1:8080/api/v1/stats/summary
# {"total":1,"done":1,"progress_percent":100,"estimated_min":90,"actual_sec":2700}
```

| メソッド | パス | 説明 |
|---|---|---|
| GET | `/api/v1/tasks?status=` | 一覧 |
| POST | `/api/v1/tasks` | 作成 |
| GET / PATCH / DELETE | `/api/v1/tasks/{id}` | 取得 / 部分更新 / 削除 |
| POST | `/api/v1/tasks/{id}/timer/start` | タイマー開始（201。未着手なら進行中にする） |
| POST | `/api/v1/tasks/{id}/timer/stop` | タイマー停止（確定した区間を返す） |
| GET / POST | `/api/v1/tasks/{id}/time-entries` | 時間記録の一覧 / 手動追加 |
| GET / PATCH / DELETE | `/api/v1/time-entries/{id}` | 時間記録の取得 / 修正 / 削除（計測中の区間を削除するとタイマーの取り消し） |
| GET | `/api/v1/stats/summary` | 進捗（完了件数・完了率）と目標・実績時間の合計 |

- 日時は UTC の RFC3339、未設定の値は `null` で返します。送るときはタイムゾーン付きなら何でも受け付けます
- タスクには `actual_sec`（実績時間の合計秒。計測中の区間は応答時点まで）と `running_since`（計測中タイマーの開始時刻）が付きます
- タスクを完了にすると、計測中のタイマーは自動で止まります。時間記録は「終了 > 開始」「24時間以内」「終了が未来でない」ことが必要です
- PATCH は部分更新です。送らなかった項目は変わりません
  - `"estimated_min": null` を送ると目標時間を未設定に戻せます
  - それ以外の項目（`title` / `description` / `status`）の `null` は「変更しない」として扱います
- 未知のフィールドはエラー（400）、`?status=` に不正な値を渡すと 422 になります
- `API_CORS_ORIGINS` で許可したオリジンは、CSRF 対策の信頼オリジンにもなります。信頼できるアプリだけを登録してください
- エラーは `{"error": {"code": "...", "message": "...", "details": [...]}}` の形式です
- 422 の `details` は `{"field": "title", "code": "required", "message": "タイトルを入力してください"}` の形で、`code` で違反の種類を判別できます（`required` / `too_long` / `invalid` / `out_of_range` / `not_after_start` / `in_future`）。`message` は普通の日本語なので、独自の文言を出したいアプリは `code` を使ってください

| code | HTTP | 意味 |
|---|---|---|
| `invalid_json` | 400 | JSON の構文・型の誤り、未知のフィールド |
| `unauthorized` | 401 | API キーが無い・違う |
| `not_found` | 404 | タスクまたは API のパスが存在しない |
| `method_not_allowed` | 405 | パスに対して使えないメソッド（`Allow` ヘッダーに使えるメソッド） |
| `payload_too_large` | 413 | ボディが 1MB を超えている |
| `unsupported_media_type` | 415 | `Content-Type: application/json` でない |
| `timer_already_running` | 409 | すでにタイマーが動いている（メッセージにどのタスクかを含む） |
| `timer_not_running` | 409 | 止めようとしたタスクのタイマーが動いていない |
| `task_completed` | 409 | 完了したタスクのタイマーは開始できない |
| `entry_running` | 409 | 計測中の区間は修正できない（先に停止する） |
| `validation_failed` | 422 | 入力値の検証エラー（`details` に項目ごとの理由） |
| `internal_error` | 500 | サーバー内部のエラー |

API の手前の防御で拒否された場合は、JSON ではなく平文で返ります。

| HTTP | 原因 |
|---|---|
| 403 | 他サイトのブラウザからの送信（CSRF 対策）。`API_CORS_ORIGINS` に登録すると許可される |
| 421 | `Host` ヘッダーが `localhost` / `127.0.0.1` / `::1` 以外（DNS リバインディング対策） |

## 開発コマンド

`make`（または `make help`）でコマンド一覧を表示できます。

| コマンド | 内容 |
|---|---|
| `make build` | `bin/` にビルド（api / web / insight） |
| `make test` | 全モジュールのテスト（`-race` 付き） |
| `make lint` | golangci-lint（モジュールごと）と shellcheck |
| `make fmt` | コード整形 |
| `make tidy` | 各モジュールの `go.mod` / `go.sum` を整理 |

```
.
├── api/        Task API（cmd/api、internal/{config,db,task,httpapi}）
├── web/        画面アプリ（cmd/web、internal/{config,taskclient,board}）
├── insight/    抽出 API（cmd/insight、internal/config）
├── shared/     共通部品（httpserver: 起動・/healthz・Host 検証・防御ヘッダー、envconf: 設定の共通処理）
├── scripts/    バックグラウンド起動・停止（service.sh）
├── go.work
└── Makefile
```
