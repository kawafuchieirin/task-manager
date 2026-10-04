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
    const elapsed = Math.max(0, Number(el.dataset.elapsed) + Math.floor((now - Number(el.dataset.renderedAt)) / 1000));
    el.textContent = formatClock(elapsed);
    if (el.dataset.estimated) updateCountdown(el, elapsed, Number(el.dataset.estimated));
  });
}

// ---- 目標時間のカウントダウンと通知 ----
// 計測中に目標時間を「またいだ」瞬間に1回だけ知らせる。最初から目標を超えていた計測では知らせない。
// 区別のため、計測ごと（タスク ID + 計測開始時刻）に「最初に見たとき目標前だったか」を覚えておく。

const armed = new Map(); // 計測ごとのキー → 最初に見たとき目標前だったか
const NOTIFIED_KEY = "taskboard:notified";

function storageGet(storage, key) {
  try { return storage.getItem(key); } catch { return null; } // プライベートモードなどで使えないことがある
}
function storageSet(storage, key, value) {
  try { storage.setItem(key, value); } catch { /* 保存できなくても動作は続ける */ }
}

function alreadyNotified(key) {
  return (storageGet(sessionStorage, NOTIFIED_KEY) || "").split(",").includes(key);
}
function markNotified(key) {
  const list = (storageGet(sessionStorage, NOTIFIED_KEY) || "").split(",").filter(Boolean);
  list.push(key);
  storageSet(sessionStorage, NOTIFIED_KEY, list.slice(-50).join(","));
}

function updateCountdown(el, elapsed, estimated) {
  const card = el.closest(".card");
  const countdown = el.closest(".card__timer")?.querySelector(".card__countdown");
  const remain = estimated - elapsed;
  if (countdown) {
    countdown.textContent = remain > 0 ? `のこり ${formatClock(remain)}` : `オーバー +${formatClock(-remain)}`;
    countdown.classList.toggle("card__countdown--over", remain <= 0);
  }
  card?.classList.toggle("card--overtime", remain <= 0);

  const key = `${el.dataset.taskId}:${el.dataset.runningSince}`;
  if (!armed.has(key)) armed.set(key, remain > 0);
  if (remain <= 0 && armed.get(key) && !alreadyNotified(key)) {
    markNotified(key);
    notifyGoal(el.dataset.taskTitle || "タスク", Math.round(estimated / 60), key);
  }
}

function notifyEnabled() {
  return storageGet(localStorage, "taskboard:notify") !== "off";
}

function notifyGoal(title, minutes, tag) {
  const text = `「${title}」の もくひょう じかん（${minutes}分）に なった！ ひとやすみ するか、 ていし しよう。`;
  // 画面のメッセージウィンドウ（自動では消さず、クリックで閉じる）
  showFlash(text);
  if (!notifyEnabled()) return;
  playJingle();
  if ("Notification" in window && Notification.permission === "granted") {
    try {
      new Notification("タスクボード", { body: text, tag });
    } catch { /* 通知を出せない環境では、画面のメッセージだけにする */ }
  }
}

// 効果音: 音声ファイルを持たず、Web Audio で短いファンファーレを鳴らす（ブラウザは操作後でないと音を出せない）。
let audioCtx;
function unlockAudio() {
  if (audioCtx || !(window.AudioContext || window.webkitAudioContext)) return;
  try { audioCtx = new (window.AudioContext || window.webkitAudioContext)(); } catch { audioCtx = undefined; }
}
function playJingle() {
  if (!audioCtx) return;
  try {
    if (audioCtx.state === "suspended") audioCtx.resume();
    const notes = [523.25, 659.25, 783.99, 1046.5]; // ド・ミ・ソ・ド
    notes.forEach((freq, i) => {
      const osc = audioCtx.createOscillator();
      const gain = audioCtx.createGain();
      osc.type = "square";
      osc.frequency.value = freq;
      const start = audioCtx.currentTime + i * 0.12;
      gain.gain.setValueAtTime(0.08, start);
      gain.gain.exponentialRampToValueAtTime(0.001, start + 0.11);
      osc.connect(gain).connect(audioCtx.destination);
      osc.start(start);
      osc.stop(start + 0.12);
    });
  } catch { /* 音が出せなくても通知の他の手段は動く */ }
}

// 通知の ON / OFF（ヘッダーのボタン）。ON にしたとき・タイマーを開始したときに、ブラウザの通知の許可を求める。
function requestNotificationPermission() {
  if ("Notification" in window && Notification.permission === "default") {
    try { Notification.requestPermission(); } catch { /* 古いブラウザなど */ }
  }
}
function renderNotifyToggle() {
  const btn = document.getElementById("notify-toggle");
  if (!btn) return;
  const on = notifyEnabled();
  btn.textContent = on ? "つうち: ON" : "つうち: OFF";
  btn.setAttribute("aria-pressed", on ? "true" : "false");
}
document.addEventListener("click", (event) => {
  unlockAudio();
  const target = event.target instanceof Element ? event.target : null;
  if (target?.closest("#notify-toggle")) {
    storageSet(localStorage, "taskboard:notify", notifyEnabled() ? "off" : "on");
    renderNotifyToggle();
    if (notifyEnabled()) requestNotificationPermission();
  } else if (target?.closest(".button--timer") && notifyEnabled()) {
    requestNotificationPermission();
  }
});
document.addEventListener("DOMContentLoaded", renderNotifyToggle);

document.addEventListener("DOMContentLoaded", tickTimers);
document.addEventListener("htmx:afterSettle", tickTimers);
// 1秒ごとだと表示が最大1秒近く遅れるため、短い間隔で確認して秒の切り替わりに追従する。
setInterval(tickTimers, 250);

// メッセージウィンドウはクリックで閉じる（RPG の「▼ で送る」操作に合わせる）。
document.addEventListener("click", (event) => {
  const flash = document.getElementById("flash");
  if (flash && event.target === flash) flash.hidden = true;
});
