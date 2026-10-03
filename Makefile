# よく使うコマンド。`make` または `make help` で一覧を表示する。
#
# ツールのバージョンは .mise.toml で固定し、すべて `mise exec` 経由で実行する。
# シェルで mise を有効化していなくても、CI と同じバージョンが使われる。
# .env があれば mise が環境変数として読み込む（設定項目は .env.example を参照）。
#
# macOS 標準の GNU Make 3.81 でも動くように、新しい Make の機能は使わない。

MISE ?= mise
RUN := $(MISE) exec --
SERVICE := $(RUN) bash scripts/service.sh

.DEFAULT_GOAL := help
.PHONY: help start stop restart status logs build test lint fmt

help: ## コマンド一覧を表示する
	@awk 'BEGIN {FS = ":.*## "} /^[a-z]+:.*## / {printf "  make %-8s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

start: ## taskboard と insight をバックグラウンドで起動し、接続先を表示する
	@$(SERVICE) start

stop: ## バックグラウンドの taskboard と insight を停止する
	@$(SERVICE) stop

restart: ## 再ビルドして再起動する（コードを変更したとき）
	@$(SERVICE) restart

status: ## 起動状態と接続先を表示する
	@$(SERVICE) status

logs: ## ログを表示し続ける（Ctrl-C で終了。サービスは止まらない）
	@$(SERVICE) logs

build: ## bin/ にビルドする
	@$(SERVICE) build

test: ## 全テストをレースディテクタ付きで実行する
	$(RUN) go test -race ./...

lint: ## golangci-lint と shellcheck で静的解析する
	$(RUN) golangci-lint run
	$(RUN) shellcheck scripts/*.sh

fmt: ## コードを整形する
	$(RUN) golangci-lint fmt
