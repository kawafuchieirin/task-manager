# よく使うコマンド。`make` または `make help` で一覧を表示する。
#
# ツールのバージョンは .mise.toml で固定し、すべて `mise exec` 経由で実行する。
# シェルで mise を有効化していなくても、CI と同じバージョンが使われる。
# .env があれば mise が環境変数として読み込む（設定項目は .env.example を参照）。
#
# リポジトリは go.work でまとめた複数の Go モジュールで構成する（api / web / insight / shared）。
# macOS 標準の GNU Make 3.81 でも動くように、新しい Make の機能は使わない。

MISE ?= mise
RUN := $(MISE) exec --
SERVICE := $(RUN) bash scripts/service.sh
MODULES := api web insight shared
# go.work のルートでは ./... が使えないため、モジュールごとのパターンを並べる。
PACKAGES := $(addsuffix /...,$(addprefix ./,$(MODULES)))

.DEFAULT_GOAL := help
.PHONY: help start stop restart status logs build test lint fmt tidy

help: ## コマンド一覧を表示する
	@awk 'BEGIN {FS = ":.*## "} /^[a-z]+:.*## / {printf "  make %-8s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

start: ## api / insight / web をバックグラウンドで起動し、接続先を表示する
	@$(SERVICE) start

stop: ## バックグラウンドの api / insight / web を停止する
	@$(SERVICE) stop

restart: ## 再ビルドして再起動する（コードを変更したとき）
	@$(SERVICE) restart

status: ## 起動状態と接続先を表示する
	@$(SERVICE) status

logs: ## ログを表示し続ける（Ctrl-C で終了。サービスは止まらない）
	@$(SERVICE) logs

build: ## bin/ にビルドする
	@$(SERVICE) build

test: ## 全モジュールのテストをレースディテクタ付きで実行する
	$(RUN) go test -race -timeout 120s $(PACKAGES)

lint: ## golangci-lint（モジュールごと）と shellcheck で静的解析する
	@for m in $(MODULES); do \
		echo "golangci-lint: $$m"; \
		(cd $$m && $(RUN) golangci-lint run) || exit 1; \
	done
	$(RUN) shellcheck scripts/*.sh

fmt: ## コードを整形する
	@for m in $(MODULES); do (cd $$m && $(RUN) golangci-lint fmt) || exit 1; done

tidy: ## 各モジュールの go.mod / go.sum を整理する
	@for m in $(MODULES); do (cd $$m && $(RUN) go mod tidy) || exit 1; done
