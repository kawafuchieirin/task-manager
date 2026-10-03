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
mise run dev      # taskboard と insight を同時に起動（Ctrl-C で停止）
```

起動確認:

```sh
curl http://127.0.0.1:8080/healthz   # {"status":"ok"}
curl http://127.0.0.1:8081/healthz   # {"status":"ok"}
```

DB は初回起動時に `data/taskboard.db` に作成され、マイグレーションが自動で適用されます。

## 設定

すべて任意です。変更する場合は `.env.example` を `.env` にコピーして編集すると、mise が自動で読み込みます。

| 環境変数 | 既定値 | 説明 |
|---|---|---|
| `TASKBOARD_ADDR` | `127.0.0.1:8080` | taskboard の待ち受けアドレス |
| `TASKBOARD_DB_PATH` | `data/taskboard.db` | SQLite ファイルのパス |
| `INSIGHT_ADDR` | `127.0.0.1:8081` | insight の待ち受けアドレス |

## 開発コマンド

| コマンド | 内容 |
|---|---|
| `mise run dev` | 両サービスを起動 |
| `mise run test` | 全テスト（`-race` 付き） |
| `mise run lint` | golangci-lint |
| `mise run fmt` | コード整形 |
