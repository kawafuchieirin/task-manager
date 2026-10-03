// メッセージウィンドウ（#flash）
// - 操作できなかったとき: htmx のエラー応答（404 / 409 / 500 など）の本文を出す
//   （422 はボードごと差し替えてフォームにエラーを出すので、ここでは扱わない）
// - 操作が成功したとき: サーバーが out-of-band で #flash ごと差し替える（flash--info）
function showFlash(text) {
  const flash = document.getElementById("flash");
  if (!flash) return;
  flash.classList.remove("flash--info");
  flash.textContent = text;
  flash.hidden = false;
}

document.addEventListener("htmx:responseError", (event) => {
  const xhr = event.detail.xhr;
  showFlash(xhr.responseText.trim() || `なにか おかしな ことが おきた！（${xhr.status}）`);
});

document.addEventListener("htmx:sendError", () => {
  showFlash("がめんの サーバーと つうしん できない！ make status で web が うごいているか たしかめてください。");
});

// 次の操作を始めたら、前のメッセージを消す。
document.addEventListener("htmx:beforeRequest", () => {
  const flash = document.getElementById("flash");
  if (flash) flash.hidden = true;
});

// 成功のメッセージは、操作のじゃまにならないよう少しして自動で消す（エラーはクリックするまで残す）。
let infoTimer;
document.addEventListener("htmx:oobAfterSwap", (event) => {
  if (event.detail.target?.id !== "flash") return;
  clearTimeout(infoTimer);
  infoTimer = setTimeout(() => {
    const flash = document.getElementById("flash");
    if (flash?.classList.contains("flash--info")) flash.hidden = true;
  }, 4000);
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
