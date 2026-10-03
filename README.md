# task-manager

自分専用の学習・作業タスク管理アプリ。要件は [SPEC.md](SPEC.md) を参照。

| サービス | 役割 | 既定アドレス |
|---|---|---|
| `taskboard` | 画面と Task API（SQLite に保存） | http://127.0.0.1:8080 |
| `insight` | 振り返りから「学んだこと / できなかったこと」を抽出する API | http://127.0.0.1:8081 |

## セットアップ

[mise](https://mise.jdx.dev/) が必要です（Go と golangci-lint のバージョンは `.mise.toml` で固定）。

```sh
mise install
mise run start    # バックグラウンドで起動し、接続先を表示
```

```text
● taskboard  起動中  http://127.0.0.1:8080  (PID 12345)
● insight    起動中  http://127.0.0.1:8081  (PID 12346)

停止: mise run stop / ログ: mise run logs
```

ブラウザで taskboard の URL（既定は http://127.0.0.1:8080 ）を開くとボードが表示されます。

| コマンド | 内容 |
|---|---|
| `mise run start` | ビルドしてバックグラウンドで起動し、接続先を表示（起動済みなら何もしない） |
| `mise run stop` | 停止（正常終了を最大15秒待ち、終わらなければ強制終了） |
| `mise run restart` | 再ビルドして再起動（コードを変更したとき） |
| `mise run status` | 起動状態と接続先を表示 |
| `mise run logs` | ログを表示し続ける（Ctrl-C で終了。サービスは止まらない） |

- PID とログは `.run/` に保存されます。ログは起動のたびに作り直されます
- ポートが使用中などで起動できなかった場合は、ログの末尾を表示し、起動途中のサービスも止めます
- 表示される接続先は、サーバーが実際に待ち受けているアドレスです（`TASKBOARD_ADDR` / `INSIGHT_ADDR` の設定が反映されます）

DB は初回起動時に `data/taskboard.db` に作成され、マイグレーションが自動で適用されます。

## 設定

すべて任意です。変更する場合は `.env.example` を `.env` にコピーして編集すると、mise が自動で読み込みます。

| 環境変数 | 既定値 | 説明 |
|---|---|---|
| `TASKBOARD_ADDR` | `127.0.0.1:8080` | taskboard の待ち受けアドレス（ループバックのみ。`0.0.0.0` などは起動エラー） |
| `TASKBOARD_DB_PATH` | `data/taskboard.db` | SQLite ファイルのパス |
| `TASKBOARD_API_KEY` | （なし） | 設定すると `/api/v1` に `Authorization: Bearer <キー>` を要求する（16文字以上） |
| `TASKBOARD_CORS_ORIGINS` | （なし） | ブラウザから API を呼ぶ他のアプリのオリジン（カンマ区切り。例: `http://localhost:3000`） |
| `INSIGHT_ADDR` | `127.0.0.1:8081` | insight の待ち受けアドレス（ループバックのみ） |

## 他のアプリから API を使う

画面と同じデータを JSON の REST API（`/api/v1`）で操作できます。

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

# 進捗
curl http://127.0.0.1:8080/api/v1/stats/summary   # {"total":1,"done":1,"progress_percent":100}
```

| メソッド | パス | 説明 |
|---|---|---|
| GET | `/api/v1/tasks?status=` | 一覧 |
| POST | `/api/v1/tasks` | 作成 |
| GET / PATCH / DELETE | `/api/v1/tasks/{id}` | 取得 / 部分更新 / 削除 |
| GET | `/api/v1/stats/summary` | 進捗（完了件数・完了率） |

- 日時は UTC の RFC3339、未設定の値は `null` で返します
- PATCH は部分更新です。送らなかった項目は変わりません
  - `"estimated_min": null` を送ると目標時間を未設定に戻せます
  - それ以外の項目（`title` / `description` / `status`）の `null` は「変更しない」として扱います
- 未知のフィールドはエラー（400）、`?status=` に不正な値を渡すと 422 になります
- `TASKBOARD_CORS_ORIGINS` で許可したオリジンは、CSRF 対策の信頼オリジンにもなります（画面のフォーム送信も受け付けます）。信頼できるアプリだけを登録してください
- エラーは `{"error": {"code": "...", "message": "...", "details": [...]}}` の形式です

| code | HTTP | 意味 |
|---|---|---|
| `invalid_json` | 400 | JSON の構文・型の誤り、未知のフィールド |
| `unauthorized` | 401 | API キーが無い・違う |
| `not_found` | 404 | タスクまたは API のパスが存在しない |
| `method_not_allowed` | 405 | パスに対して使えないメソッド（`Allow` ヘッダーに使えるメソッド） |
| `payload_too_large` | 413 | ボディが 1MB を超えている |
| `unsupported_media_type` | 415 | `Content-Type: application/json` でない |
| `validation_failed` | 422 | 入力値の検証エラー（`details` に項目ごとの理由） |
| `internal_error` | 500 | サーバー内部のエラー |

API の手前の防御で拒否された場合は、JSON ではなく平文で返ります。

| HTTP | 原因 |
|---|---|
| 403 | 他サイトのブラウザからの送信（CSRF 対策）。`TASKBOARD_CORS_ORIGINS` に登録すると許可される |
| 421 | `Host` ヘッダーが `localhost` / `127.0.0.1` / `::1` 以外（DNS リバインディング対策） |

## 開発コマンド

| コマンド | 内容 |
|---|---|
| `mise run dev` | 両サービスをフォアグラウンドで起動（ログを直接見ながら開発するとき。Ctrl-C で停止） |
| `mise run test` | 全テスト（`-race` 付き） |
| `mise run lint` | golangci-lint |
| `mise run fmt` | コード整形 |
