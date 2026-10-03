// htmx のエラー応答（404 / 500 など）を画面上部に表示する。
// 422 はボードごと差し替えてフォームにエラーを出すので、ここでは扱わない。
document.addEventListener("htmx:responseError", (event) => {
  const flash = document.getElementById("flash");
  if (!flash) return;
  const xhr = event.detail.xhr;
  flash.textContent = xhr.responseText.trim() || `エラーが発生しました（${xhr.status}）`;
  flash.hidden = false;
});

document.addEventListener("htmx:sendError", () => {
  const flash = document.getElementById("flash");
  if (!flash) return;
  flash.textContent = "画面のサーバーに接続できません。make status で web が起動しているか確認してください。";
  flash.hidden = false;
});

// 操作が成功したら古いエラー表示を消す。
document.addEventListener("htmx:afterSwap", () => {
  const flash = document.getElementById("flash");
  if (flash) flash.hidden = true;
});

// 計測中タイマーの経過時間を進める。
// サーバーが描画した時点の実績（data-elapsed 秒）に、描画からの経過を足す。
// ブラウザとサーバーの時計のずれに影響されないよう、開始時刻ではなく描画時点からの差で数える。
function formatClock(sec) {
  const h = Math.floor(sec / 3600);
  const m = String(Math.floor(sec / 60) % 60).padStart(2, "0");
  const s = String(sec % 60).padStart(2, "0");
  return `${h}:${m}:${s}`;
}

function tickTimers() {
  const now = Date.now();
  document.querySelectorAll("[data-elapsed]").forEach((el) => {
    if (!el.dataset.renderedAt) el.dataset.renderedAt = String(now);
    const elapsed = Number(el.dataset.elapsed) + Math.floor((now - Number(el.dataset.renderedAt)) / 1000);
    el.textContent = formatClock(Math.max(0, elapsed));
  });
}

document.addEventListener("DOMContentLoaded", tickTimers);
document.addEventListener("htmx:afterSettle", tickTimers);
// 1秒ごとだと表示が最大1秒近く遅れるため、短い間隔で確認して秒の切り替わりに追従する。
setInterval(tickTimers, 250);

// メッセージウィンドウはクリックで閉じる（RPG の「▼ で送る」操作に合わせる）。
document.addEventListener("click", (event) => {
  const flash = document.getElementById("flash");
  if (flash && event.target === flash) flash.hidden = true;
});
