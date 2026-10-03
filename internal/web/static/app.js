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
  flash.textContent = "サーバーに接続できません。taskboard が起動しているか確認してください。";
  flash.hidden = false;
});

// 操作が成功したら古いエラー表示を消す。
document.addEventListener("htmx:afterSwap", () => {
  const flash = document.getElementById("flash");
  if (flash) flash.hidden = true;
});
