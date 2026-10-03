# task-manager 要件定義書（SPEC）

- 元 Issue: [#3 機能要件](https://github.com/kawafuchieirin/task-manager/issues/3)
- ステータス: ドラフト（2026-10-03）

## 1. 概要

自分専用の学習・作業タスク管理アプリ。タスクをボードで管理し、目標時間と実績時間を記録、
完了時の振り返りから「学んだこと / できなかったこと」を抽出する。進捗に応じてドット絵キャラクターが成長する。

主要機能は REST API として公開し、他のアプリ（CLI・別の Web アプリ・自動化スクリプト等）からも利用できるようにする。

## 2. 前提・制約

| 項目 | 決定事項 |
|---|---|
| 利用者 | 自分のみ（シングルユーザー、ログイン機能なし） |
| 実行環境 | ローカルのみ（デプロイしない） |
| 言語 | Go 1.27（mise で管理） |
| フロントエンド | Go `html/template` + htmx（サーバーサイドレンダリング） |
| DB | SQLite（ファイル1つ、Docker 不要） |
| 起動方法 | `mise install` → `make start`（バックグラウンド起動・接続先を表示）、停止は `make stop`。コマンドは Makefile に集約し、ツールは `mise exec` 経由で実行する |

### 実行環境に mise + SQLite を選んだ理由

- シングルユーザー・ローカル専用のため、PostgreSQL などの DB サーバーは不要
- Go は単一バイナリになり、mise で Go のバージョンを固定すれば環境の再現性は十分
- Docker を使わないので起動が速く、デバッガ連携も容易
- 将来 Docker が必要になった場合でも、Dockerfile を追加するだけで移行できる構成にする

## 3. システム構成

画面アプリと API を分け、「抽出機能は API を別で切り出す」方針と合わせて、**3つの独立したサービス**として構成する（2026-10-03 変更）。
画面アプリ（web）も「API を使う他のアプリ」の1つとして扱い、DB を持つのは api だけにする。

```
┌──────────────┐  HTML   ┌──────────────────────┐  HTTP/JSON  ┌──────────────────────┐
│ ブラウザ      │────────▶│ web (:3000)          │────────────▶│ api (:8080)          │──▶ SQLite
│ (htmx)       │◀────────│  画面（ボード/進捗/   │（タイムアウト・│  Task API /api/v1    │    (data/taskboard.db)
└──────────────┘         │  キャラ）。DB なし    │  読み取りは   └──────────┬───────────┘
                         └──────────────────────┘  リトライ）              │ HTTP（タイムアウト・リトライ）
┌──────────────┐  HTTP/JSON                                               ▼
│ 他のアプリ    │─────────────────────▶ api ／ insight         ┌──────────────────────┐
└──────────────┘                                              │ insight (:8081)      │
                                                              │  抽出 API（DB なし）   │
                                                              └──────────────────────┘
```

| サービス | ディレクトリ | 役割 | 状態 |
|---|---|---|---|
| `api` | `api/` | タスク・時間・振り返りの管理 API | SQLite に永続化 |
| `web` | `web/` | 画面の配信。データの読み書きはすべて api を呼ぶ | 持たない |
| `insight` | `insight/` | 振り返りテキストを受け取り「学んだこと / できなかったこと」を抽出して返す | 持たない |

- 各ディレクトリは独立した Go モジュールとし、`go.work` でまとめる。共通部品は `shared/` モジュールに置く
- web は api の内部パッケージ（DB・サービス層）を import できない（Go の `internal` の制約）。境界は API の契約（JSON）だけになる
- 入力値の検証は api に一本化し、web は api が返す 422 の内容をそのまま画面に表示する
- web から api への呼び出しはタイムアウト 5 秒。読み取り（GET）だけ通信エラーと 502/503/504 で最大2回リトライする（書き込みは二重登録を避けるためリトライしない）
- api が止まっていても web は落ちず、「API サーバーに接続できません」と表示する
- `insight` は api / web に依存しないので、他のアプリから単体でも使える
- api から insight への呼び出しが失敗しても、タスク管理機能は動き続ける（抽出結果だけ「取得失敗」と表示する）

## 4. 機能要件

### F1. タスク管理ボード

- カンバン形式で「未着手 / 進行中 / 完了」の3列を表示する
- タスクの作成・編集・削除ができる
- ステータスはボタン操作で変更する（ドラッグ&ドロップは F1 の必須範囲外。後から追加できるようにする）
- タスクの項目: タイトル（必須、1〜100文字）、説明（任意、2000文字まで）、目標時間（分）、ステータス、作成日時、完了日時

### F2. 進捗バー

- 全タスクに対する完了タスクの割合（%）をバーで表示する
- タスクが0件のときは 0% と表示する（0除算にしない）
- ステータスを変更したら htmx で即時に更新する

### F3. 完了タスクの取り消し線

- 完了列のタスクはタイトルに取り消し線を引き、文字色を薄くする
- 完了を取り消す（完了 → 進行中に戻す）ことができ、戻すと取り消し線も消える

### F4. 目標時間と実際の時間（API 化）

- タスクごとに目標時間（分）を設定できる
- 実際の時間は次のどちらかで記録できる
  - **タイマー**: 開始/停止ボタンで時間の区間を記録する（同時に動かせるタイマーは1つだけ）
  - **手動入力**: 区間を追加・修正する
- 実際の時間 = 全区間の合計
- 画面には「目標 / 実績 / 差分」を表示し、目標を超えたら強調表示する
- これらの機能はすべて Task API からも操作できる

### F5. 学んだこと・できなかったことの抽出（別 API）

- タスクを完了するときに振り返りメモ（自由記述）を入力できる（任意）
- api は振り返りメモを insight API に送り、抽出結果を保存する。web はそれを表示する
- 振り返り一覧画面で、期間ごとの「学んだこと / できなかったこと」を一覧で表示する
- 抽出の方式は**ルールベース**とする（2026-10-03 決定）。「学んだこと」「できなかったこと」の2つの入力欄、または `+` / `-` で始まる行で分類する
- 将来 LLM による抽出を追加できるよう、抽出処理は `Extractor` インターフェースで差し替えられる設計にする（API の入出力は変えない）

### F6. ドット絵キャラクターの演出

- 画面にドット絵のキャラクターを常に表示する
- **成長**: 完了したタスクの累計数に応じてレベルアップし、見た目が変わる（例: 0 / 5 / 15 / 30 / 50 件でレベルが上がる）
- **表情・モーション**: 当日の進捗率に応じて変化する（例: 0% = 眠る、1〜99% = 歩く、100% = 喜ぶ）
- **イベント演出**: タスク完了時に短いアニメーション（ジャンプ・キラキラなど）を再生する
- 素材はスプライトシートの PNG を CSS アニメーション（`steps()`）で動かす。外部 CDN は使わず、リポジトリに同梱する

## 5. API 仕様（概要）

- JSON / UTF-8、パスは `/api/v1` で始める
- エラーの形式を統一する: `{"error": {"code": "validation_failed", "message": "...", "details": [...]}}`
- OpenAPI 3 の定義ファイル（`api/openapi.yaml`）を API 仕様の唯一の正とし、他のアプリはこれを参照して連携する

### 5.1 Task API（api :8080）

| メソッド | パス | 説明 |
|---|---|---|
| GET | `/api/v1/tasks?status=` | タスク一覧（ステータスで絞り込み可） |
| POST | `/api/v1/tasks` | タスク作成 |
| GET | `/api/v1/tasks/{id}` | タスク詳細（実績時間・振り返りを含む） |
| PATCH | `/api/v1/tasks/{id}` | タスク更新（ステータス変更を含む） |
| DELETE | `/api/v1/tasks/{id}` | タスク削除 |
| POST | `/api/v1/tasks/{id}/timer/start` | タイマー開始（他のタスクのタイマーが動いていれば 409） |
| POST | `/api/v1/tasks/{id}/timer/stop` | タイマー停止 |
| GET | `/api/v1/tasks/{id}/time-entries` | 時間区間の一覧 |
| POST | `/api/v1/tasks/{id}/time-entries` | 時間区間を手動で追加 |
| PATCH / DELETE | `/api/v1/time-entries/{id}` | 時間区間の修正・削除 |
| PUT | `/api/v1/tasks/{id}/reflection` | 振り返りメモの登録（insight での抽出を実行する） |
| GET | `/api/v1/reflections?from=&to=` | 期間内の抽出結果一覧 |
| GET | `/api/v1/stats/summary` | 進捗率、目標/実績時間の合計、キャラクターのレベル |
| GET | `/healthz` | ヘルスチェック |

### 5.2 Insight API（insight :8081）

| メソッド | パス | 説明 |
|---|---|---|
| POST | `/api/v1/extract` | 振り返りテキストを受け取り、抽出結果を返す |
| GET | `/healthz` | ヘルスチェック |

```jsonc
// POST /api/v1/extract
// request
{ "text": "goroutine の使い方を理解した。テストの書き方はまだ曖昧。" }
// response 200
{ "learned": ["goroutine の使い方"], "not_learned": ["テストの書き方"] }
```

## 6. データモデル（SQLite）

```
tasks
  id               INTEGER PK
  title            TEXT NOT NULL
  description      TEXT NOT NULL DEFAULT ''
  status           TEXT NOT NULL CHECK (status IN ('todo','doing','done'))
  estimated_min    INTEGER NULL CHECK (estimated_min >= 0)
  completed_at     TEXT NULL
  created_at / updated_at TEXT NOT NULL

time_entries
  id               INTEGER PK
  task_id          INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE
  started_at       TEXT NOT NULL
  ended_at         TEXT NULL              -- NULL = タイマー計測中
  CHECK (ended_at IS NULL OR ended_at >= started_at)
  -- 計測中（ended_at IS NULL）の行は全体で1行までに制限する（部分ユニークインデックス）

reflections
  task_id          INTEGER PK REFERENCES tasks(id) ON DELETE CASCADE
  body             TEXT NOT NULL          -- 振り返りメモの原文
  learned_json     TEXT NULL              -- 抽出結果（JSON 配列）
  not_learned_json TEXT NULL
  extract_status   TEXT NOT NULL CHECK (extract_status IN ('pending','ok','failed'))
  updated_at       TEXT NOT NULL
```

- 日時は UTC の RFC3339 文字列（TEXT）で保存し、画面では JST で表示する
- マイグレーションは `internal/db/migrations/*.sql` を `embed` でバイナリに組み込み、起動時に未適用のものだけ適用する（`schema_migrations` テーブルで管理）

## 7. 非機能要件

| 区分 | 要件 |
|---|---|
| セキュリティ | 待ち受けアドレスはループバック（`127.0.0.1` / `::1` / `localhost`）に限定し、それ以外を設定すると起動しない（画面は認証なしのため、LAN に公開すると API キーを迂回できてしまう）。他のアプリ向けに任意で API キー認証（`API_KEY`、`/api/v1` のみ対象）を有効化できる。web は `WEB_API_KEY` で同じキーを送る。CORS で許可するオリジンは `API_CORS_ORIGINS` で設定する。ローカルのサーバーをブラウザ経由で操作されないよう、`Host` ヘッダーの検証（DNS リバインディング対策）と `http.CrossOriginProtection`（CSRF 対策）、クリックジャッキング対策のヘッダーを全サービスの全ルートに適用する。入力はすべてサーバー側で検証する |
| 信頼性 | insight の呼び出しはタイムアウト 5 秒、指数バックオフで最大2回リトライ。失敗時は `extract_status=failed` にして、画面から再実行できる |
| 性能 | ローカルでの API 応答は p95 で 100ms 以下（タスク1000件時点） |
| 依存 | できるだけ標準ライブラリを使う（ルーティングは `net/http` の ServeMux）。SQLite ドライバは cgo 不要の `modernc.org/sqlite`。htmx はファイルを同梱する |
| 設定 | ポート・DB パス・insight の URL・API キーは環境変数で指定する（ハードコードしない）。`.env.example` を用意する |
| ログ | `log/slog` で構造化ログを出す |
| テスト | ドメインロジックはテーブル駆動テスト、HTTP は `httptest`、DB はテストごとに一時ファイルの SQLite を使う。エッジケース（0件、目標未設定、タイマーの二重起動、insight の失敗）を必ずテストする |
| 品質 | `gofmt` / `go vet` / `golangci-lint`（モジュールごと）/ `shellcheck`。CI（GitHub Actions + mise）で `make lint` / `make test` を実行する |

## 8. ディレクトリ構成

```
.
├── .mise.toml              # ツールのバージョン（go / golangci-lint / shellcheck）と .env の読み込み
├── Makefile                # make start / stop / test / lint など
├── go.work                 # api / web / insight / shared の各モジュールをまとめる
├── scripts/service.sh      # バックグラウンド起動・停止
├── api/                    # Task API（DB を持つ唯一のサービス）
│   ├── cmd/api/
│   ├── internal/
│   │   ├── config/         # API_* 環境変数
│   │   ├── db/             # SQLite 接続・マイグレーション（migrations/*.sql を embed）
│   │   ├── task/           # ドメインモデル・検証・永続化
│   │   ├── httpapi/        # JSON の REST API（/api/v1）、API キー認証、CORS
│   │   ├── timer/          # （M3）
│   │   ├── reflection/     # （M4）
│   │   └── insightclient/  # （M4）api から insight を呼ぶクライアント
│   └── openapi.yaml        # （M6）
├── web/                    # 画面アプリ（DB を持たない）
│   ├── cmd/web/
│   └── internal/
│       ├── config/         # WEB_* 環境変数
│       ├── taskclient/     # api を呼ぶクライアント（タイムアウト・リトライ）
│       ├── board/          # HTML ハンドラ・テンプレート・静的ファイル（htmx.min.js、CSS、ドット絵スプライトを embed）
│       └── character/      # （M5）レベル・状態の判定ロジック
├── insight/                # 抽出 API（DB を持たない）
│   ├── cmd/insight/
│   └── internal/
│       ├── config/         # INSIGHT_* 環境変数
│       └── extract/        # （M4）抽出ロジック（Extractor インターフェース）
├── shared/                 # 3サービス共通の部品
│   ├── httpserver/         # 起動・グレースフルシャットダウン・/healthz・Host 検証・防御ヘッダー
│   └── envconf/            # 環境変数の読み込みとループバック限定の検証
└── data/                   # SQLite ファイル（.gitignore で除外）
```

## 9. 未決事項

| # | 内容 | 推奨案 |
|---|---|---|
| Q1 | ドット絵素材の入手方法（自作 / フリー素材） | v1 は簡単な自作スプライトで仮置きし、後で差し替える |
| Q2 | レベルアップのしきい値、キャラクターの種類 | 4. F6 の例の値で始め、実際に使いながら調整する |

## 10. 実装の進め方（マイルストーン）

1. **M1 基盤**: `.mise.toml`、2つのサービスの骨組み、SQLite とマイグレーション、`/healthz`、CI
2. **M2 タスク管理**: F1・F2・F3（Task API と htmx の画面）
3. **M3 時間管理**: F4（タイマー・手動入力・目標との差分表示）
4. **M4 抽出**: F5（insight サービス、クライアントのリトライ、振り返り一覧）
5. **M5 演出**: F6（キャラクターの成長・表情・完了時のアニメーション）
6. **M6 仕上げ**: OpenAPI 定義の整備、README（起動手順・他のアプリからの利用例）

各マイルストーンを1つの PR にし、PR タイトルは Conventional Commits の形式（`feat: ...`）にする。
