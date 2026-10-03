#!/usr/bin/env bash
# taskboard / insight をバックグラウンドで起動・停止する。
# Makefile（make start / stop / restart / status / logs / build）から呼び出す。
#
#   .run/<name>.pid  起動中プロセスの PID
#   .run/<name>.log  標準出力・標準エラー（起動のたびに作り直す）
#
# macOS 標準の bash 3.2 でも動くように、連想配列などの bash 4 以降の機能は使わない。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_DIR="$ROOT/.run"
BIN_DIR="$ROOT/bin"
SERVICES=(taskboard insight)
# 起動待ちの上限（秒）。初回はマイグレーションがあるため余裕を持たせる。
START_TIMEOUT=15
# 停止待ちの上限（秒）。サーバーのグレースフルシャットダウン（10秒）より長くする。
STOP_TIMEOUT=15

# DB パスなどの相対パスをプロジェクトルート基準にする。
cd "$ROOT"

pid_file() { echo "$RUN_DIR/$1.pid"; }
log_file() { echo "$RUN_DIR/$1.log"; }

# running_pid は、サービスが起動中ならその PID を出力して成功を返す。
# PID ファイルが残っていても、プロセスが無い・別のプログラムに PID が再利用されている場合は
# 古いファイルとして削除し、無関係なプロセスを止めないようにする。
running_pid() {
  local name=$1 file pid cmd
  file=$(pid_file "$name")
  [[ -f $file ]] || return 1
  pid=$(<"$file")
  if [[ $pid =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then
    cmd=$(ps -p "$pid" -o command= 2>/dev/null || true)
    if [[ $cmd == "$BIN_DIR/$name"* ]]; then
      echo "$pid"
      return 0
    fi
  fi
  rm -f "$file"
  return 1
}

# listen_addr は、ログからサーバーが実際に待ち受けているアドレスを取り出す。
# 設定の既定値をここに重複させないため、internal/server の起動ログ（addr=...）を唯一の情報源にする。
listen_addr() {
  local file
  file=$(log_file "$1")
  [[ -f $file ]] || return 0
  sed -n 's/.*msg=サーバーを起動しました.* addr=\([^ ]*\).*/\1/p' "$file" | tail -n 1
}

build() {
  echo "ビルドしています..."
  mkdir -p "$BIN_DIR"
  go build -o "$BIN_DIR/" ./cmd/taskboard ./cmd/insight
}

# wait_ready は、起動ログが出る（＝ポートの待ち受けに成功する）まで待つ。
# 途中でプロセスが終了した場合（ポート使用中・設定エラーなど）は失敗を返す。
wait_ready() {
  local name=$1 pid=$2 i
  for ((i = 0; i < START_TIMEOUT * 10; i++)); do
    if ! kill -0 "$pid" 2>/dev/null; then
      return 1
    fi
    if [[ -n $(listen_addr "$name") ]]; then
      return 0
    fi
    sleep 0.1
  done
  return 1
}

stop_one() {
  local name=$1 pid i
  if ! pid=$(running_pid "$name"); then
    echo "$name は停止しています"
    return 0
  fi
  kill -TERM "$pid" 2>/dev/null || true
  for ((i = 0; i < STOP_TIMEOUT * 10; i++)); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
  done
  if kill -0 "$pid" 2>/dev/null; then
    echo "$name が ${STOP_TIMEOUT} 秒以内に終了しないため強制終了します（PID ${pid}）" >&2
    kill -KILL "$pid" 2>/dev/null || true
  fi
  rm -f "$(pid_file "$name")"
  echo "$name を停止しました（PID ${pid}）"
}

cmd_start() {
  mkdir -p "$RUN_DIR"
  build

  local name pid started=()
  for name in "${SERVICES[@]}"; do
    if pid=$(running_pid "$name"); then
      echo "$name は起動済みです（PID ${pid}）。コードを変更した場合は make restart を実行してください"
      continue
    fi
    # nohup と標準入力の切り離しで、ターミナルを閉じても動き続けるようにする。
    nohup "$BIN_DIR/$name" >"$(log_file "$name")" 2>&1 </dev/null &
    echo $! >"$(pid_file "$name")"
    started+=("$name")
  done

  # bash 3.2 では空配列の展開が set -u でエラーになるため ${arr[@]+...} で守る。
  for name in ${started[@]+"${started[@]}"}; do
    pid=$(<"$(pid_file "$name")")
    if ! wait_ready "$name" "$pid"; then
      echo "" >&2
      echo "$name の起動に失敗しました。ログ（$(log_file "$name")）:" >&2
      tail -n 20 "$(log_file "$name")" >&2 || true
      # 一部だけ動いている状態を残さないよう、今回起動したものは止める。
      for n in "${started[@]}"; do
        stop_one "$n" >/dev/null
      done
      exit 1
    fi
  done

  echo ""
  cmd_status
  echo ""
  echo "停止: make stop / ログ: make logs"
}

cmd_stop() {
  local name
  for name in "${SERVICES[@]}"; do
    stop_one "$name"
  done
}

cmd_status() {
  local name pid addr
  for name in "${SERVICES[@]}"; do
    if pid=$(running_pid "$name"); then
      addr=$(listen_addr "$name")
      printf '● %-10s 起動中  http://%s  (PID %s)\n' "$name" "${addr:-?}" "$pid"
    else
      printf '○ %-10s 停止中\n' "$name"
    fi
  done
}

cmd_logs() {
  local files=() name
  for name in "${SERVICES[@]}"; do
    [[ -f $(log_file "$name") ]] && files+=("$(log_file "$name")")
  done
  if [[ ${#files[@]} -eq 0 ]]; then
    echo "ログがありません。make start で起動してください" >&2
    exit 1
  fi
  tail -n 30 -F "${files[@]}"
}

case "${1:-}" in
  build) build ;;
  start) cmd_start ;;
  stop) cmd_stop ;;
  restart) cmd_stop && cmd_start ;;
  status) cmd_status ;;
  logs) cmd_logs ;;
  *)
    echo "使い方: $0 {start|stop|restart|status|logs|build}" >&2
    exit 2
    ;;
esac
