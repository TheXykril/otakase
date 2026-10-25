// The Otakase Activity: the watch room (watch.js), a menu of games played alone (the daily
// puzzle, guess the anime, higher or lower, emoji riddles, title scramble,
// the puzzles: zoomed cover, timeline, connections, redacted, anime grid,
// hangman, wordle, quiz, two truths and a lie; watch bingo), games played
// together with everyone in the call (party games, Koi-Koi, races, the
// same puzzle)
// and the points board. Signs in through the Embedded App SDK,
// then talks to the bot under api/ (relative, so it works behind Discord's
// proxy).
"use strict";

const app = document.getElementById("app");
let session = "";
let channel = "";
let sdk = null;
let me = "";
let titles = [];
let daily = null;
let busy = false;
let freshDaily = false;
let where = ""; // "menu", "party" or "" for any other page
let launchMode = ""; // empty for the full Activity, "games" or "watch" for shortcuts
let gamesOff = []; // games staff turned off
let pranks = new Set(); // prank switches an admin put on this screen

const LABELS = { yes: "right", near: "close", no: "no" };
const ARROWS = { up: " ↑", down: " ↓" };

// Line icons, 24×24, drawn with the current colour.
const ICONS = {
  daily: '<rect x="3" y="4" width="18" height="17" rx="2"/><path d="M3 9h18M8 2v4M16 2v4M8 13h2M14 13h2M8 17h2"/>',
  guess: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h6V3M9 15h6v6M15 9h6"/>',
  hl: '<path d="M7 20V5M3 9l4-4 4 4M17 4v15M13 15l4 4 4-4"/>',
  riddle: '<path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12z"/><path d="M10 10a2 2 0 1 1 2.8 1.8c-.5.3-.8.7-.8 1.2M12 16h.01"/>',
  scramble: '<path d="M16 3h5v5M4 20L21 3M21 16v5h-5M15 15l6 6M4 4l5 5"/>',
  bingo: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18M15 3v18M3 9h18M3 15h18"/>',
  board: '<path d="M4 21V12h5v9M9 21V6h6v15M15 21v-6h5v6M3 21h18"/>',
  flame: '<path d="M12 22c4 0 7-2.7 7-6.6 0-3.4-2.4-5.6-4-7.4-.4 2-1.6 3-2.7 3C13 7 11 4 8.5 2 9 6 5 8.6 5 15.4 5 19.3 8 22 12 22z"/>',
  star: '<path d="M12 3l2.8 5.7 6.2.9-4.5 4.4 1 6.2L12 17.3 6.5 20.2l1-6.2L3 9.6l6.2-.9z"/>',
  users: '<circle cx="9" cy="8" r="3.5"/><path d="M2.5 20a6.5 6.5 0 0 1 13 0M16 4.5a3.5 3.5 0 0 1 0 7M18 14a6 6 0 0 1 3.5 6"/>',
  up: '<path d="M12 19V5M5 12l7-7 7 7"/>',
  down: '<path d="M12 5v14M19 12l-7 7-7-7"/>',
  check: '<path d="M4 12l5 5L20 6"/>',
  back: '<path d="M19 12H5M11 18l-6-6 6-6"/>',
  party: '<circle cx="7" cy="8" r="3"/><circle cx="17" cy="8" r="3"/><path d="M2 20a5 5 0 0 1 10 0M12 20a5 5 0 0 1 10 0"/>',
  crown: '<path d="M3 8l4 4 5-7 5 7 4-4-2 11H5z"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  coverle: '<circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3M8 11h6M11 8v6"/>',
  timeline: '<path d="M3 12h18"/><circle cx="6" cy="12" r="2"/><circle cx="12" cy="12" r="2"/><circle cx="18" cy="12" r="2"/><path d="M6 5v3M12 16v3M18 5v3"/>',
  connections: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
  redacted: '<path d="M4 6h16M4 12h4M4 18h9"/><rect x="11" y="10" width="9" height="4" rx="1"/>',
  grid: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 3v18"/><path d="M12.5 15l2 2 4-4"/>',
  hangman: '<path d="M3 20l5-14 5 14M5 15h6M15 20V8M15 13h3.5a2.5 2.5 0 0 1 0 5H15"/>',
  sound: '<path d="M4 9v6h4l5 4V5L8 9zM16.5 8.5a5 5 0 0 1 0 7M19 6a8.5 8.5 0 0 1 0 12"/>',
  mute: '<path d="M4 9v6h4l5 4V5L8 9zM17 9l5 6M22 9l-5 6"/>',
  music: '<path d="M9 18V5l11-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="17" cy="16" r="3"/>',
  settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l-1.62 1.62a1.7 1.7 0 0 0-1.87-.34 1.7 1.7 0 0 0-1.05 1.55V21h-2.4v-1.3a1.7 1.7 0 0 0-1.05-1.55 1.7 1.7 0 0 0-1.87.34l-1.62-1.62A1.7 1.7 0 0 0 8.6 15a1.7 1.7 0 0 0-1.55-1.05H5.7v-2.4h1.35A1.7 1.7 0 0 0 8.6 10.5a1.7 1.7 0 0 0-.34-1.87l1.62-1.62a1.7 1.7 0 0 0 1.87.34 1.7 1.7 0 0 0 1.05-1.55V4.5h2.4v1.3a1.7 1.7 0 0 0 1.05 1.55 1.7 1.7 0 0 0 1.87-.34l1.62 1.62a1.7 1.7 0 0 0-.34 1.87 1.7 1.7 0 0 0 1.55 1.05h1.35v2.4h-1.35A1.7 1.7 0 0 0 19.4 15z"/>',
  shuffle: '<path d="M16 3h5v5M4 20L21 3M21 16v5h-5M15 15l6 6M4 4l5 5"/>',
  quiz: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9.5 9.5a2.5 2.5 0 1 1 3.4 2.3c-.6.3-.9.8-.9 1.4M12 16.5h.01"/>',
  truths: '<path d="M4 6h10M4 12h10M4 18h6"/><path d="M17 15l4 4M21 15l-4 4M16 6l1.5 1.5L21 4M16 10.5l1.5 1.5 3.5-3.5"/>',
  wordle: '<rect x="2" y="8" width="6" height="8" rx="1"/><rect x="9" y="8" width="6" height="8" rx="1"/><rect x="16" y="8" width="6" height="8" rx="1"/>',
  imposter: '<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/><path d="M9.5 8h5"/>',
  draw: '<path d="M4 20l4-1L19 8a2.1 2.1 0 0 0-3-3L5 16z"/><path d="M14 7l3 3"/>',
  bomb: '<circle cx="11" cy="14" r="7"/><path d="M15.5 8.5L18 6M18 6l1-1M20 3v1.5M22 5h-1.5M19.5 7l1 1"/>',
  who: '<rect x="3" y="3" width="8" height="8" rx="1.5"/><rect x="13" y="3" width="8" height="8" rx="1.5"/><rect x="3" y="13" width="8" height="8" rx="1.5"/><path d="M15 15l4 4M19 15l-4 4"/>',
  koikoi: '<rect x="3" y="4" width="10" height="15" rx="1.5" transform="rotate(-8 8 11.5)"/><rect x="11" y="5" width="10" height="15" rx="1.5" transform="rotate(8 16 12.5)"/><circle cx="16" cy="10" r="2.2"/>',
  undo: '<path d="M9 14L4 9l5-5"/><path d="M4 9h10a6 6 0 0 1 0 12h-3"/>',
  rewind: '<path d="M21 5v14l-9-7zM12 5v14l-9-7z"/>',
  trash: '<path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13"/>',
  shield: '<path d="M12 3l8 3v6c0 4.5-3.4 8-8 9-4.6-1-8-4.5-8-9V6z"/>',
  bulb: '<path d="M9 18h6M10 21h4M12 3a6 6 0 0 0-3.5 10.9c.6.5 1 1.2 1 2.1h5c0-.9.4-1.6 1-2.1A6 6 0 0 0 12 3z"/>',
  maximize: '<path d="M8 3H3v5M16 3h5v5M3 16v5h5M21 16v5h-5"/>',
  minimize: '<path d="M3 9h6V3M21 9h-6V3M3 15h6v6M21 15h-6v6"/>',
  play: '<path d="M7 4v16l13-8z"/>',
  pause: '<path d="M8 4v16M16 4v16"/>',
  stop: '<rect x="5" y="5" width="14" height="14" rx="2"/>',
  skip: '<path d="M5 4v16l10-8zM19 4v16"/>',
  x: '<path d="M6 6l12 12M18 6L6 18"/>',
  ff: '<path d="M3 5v14l9-7zM12 5v14l9-7z"/>',
  watch: '<rect x="2" y="4" width="20" height="13" rx="2"/><path d="M8 21h8M12 17v4M10 8.5v5l4-2.5z"/>',
};
const icon = (name) =>
  `<svg class="i" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICONS[name]}</svg>`;

// stamp is the hanko pressed on a result: 正解 (correct) or 残念 (missed).
// With the "fake wrong" prank on you, a right answer stamps 残念 first and
// turns into "jk" a moment later.
const stamp = (ok) =>
  ok && tv && tv.troll && tv.troll.fake_wrong
    ? (setTimeout(prankStamps, 2600), `<div class="stamp miss prank" aria-hidden="true">残念<small>MISSED</small></div>`)
    : `<div class="stamp ${ok ? "ok" : "miss"}" aria-hidden="true">${ok ? "正解" : "残念"}<small>${ok ? "CORRECT" : "MISSED"}</small></div>`;

// preview draws a tiny picture of each game for its menu card.
const PREVIEWS = {
  watch: () => `<div class="pv watch"><i>${icon("play")}</i><b><u></u></b></div>`,
  party: () => `<div class="pv party" id="pvparty">${faces()}</div>`,
  daily: () => `<div class="pv daily">${["yes", "no", "near", "yes", "no", "yes", "near", "yes"].map((h) => `<i class="${h}"></i>`).join("")}</div>`,
  guess: () => `<div class="pv guess">${Array.from({ length: 24 }, (_, k) => `<i style="opacity:${[0.25, 0.45, 0.7, 1][(k * 7) % 4]}"></i>`).join("")}</div>`,
  hl: () => `<div class="pv hl"><i>91</i><b>vs</b><i>?</i></div>`,
  riddle: () => `<div class="pv riddle"><span>🍥</span><span>🦊</span><span>🥷</span></div>`,
  scramble: () => `<div class="pv scramble">${"RUTAN".split("").map((c) => `<i>${c}</i>`).join("")}</div>`,
  bingo: () => `<div class="pv bingo">${[1, 0, 1, 0, 2, 1, 0, 1, 0].map((f) => `<i class="${["", "on", "free"][f]}"></i>`).join("")}</div>`,
  board: () => `<div class="pv board"><i style="height:60%"></i><i style="height:100%"></i><i style="height:42%"></i></div>`,
  coverle: () => `<div class="pv coverle"><i><i><i></i></i></i></div>`,
  timeline: () => `<div class="pv timeline">${["06", "13", "21"].map((y) => `<i>${y}</i>`).join("")}</div>`,
  connections: () => `<div class="pv connections">${Array.from({ length: 16 }, (_, k) => `<i class="${k < 4 ? "on" : ""}"></i>`).join("")}</div>`,
  redacted: () => `<div class="pv redacted"><span><i style="width:22px"></i><b>the</b><i style="width:30px"></i></span><span><i style="width:14px"></i><i style="width:36px"></i></span></div>`,
  grid: () => `<div class="pv grid">${[1, 0, 1, 0, 1, 0, 0, 1, 0].map((f) => `<i class="${f ? "on" : ""}"></i>`).join("")}</div>`,
  hangman: () => `<div class="pv hangman">${"N_R_T_".split("").map((c) => `<i>${c === "_" ? "" : c}</i>`).join("")}</div>`,
  quiz: () => `<div class="pv quiz">${[0, 1, 2, 3].map((k) => `<i class="${k === 2 ? "on" : ""}"></i>`).join("")}</div>`,
  truths: () => `<div class="pv truths"><i></i><i class="on"></i><i></i></div>`,
  koikoi: () => `<div class="pv koikoi"><i></i><i class="b">光</i><i></i></div>`,
  wordle: () =>
    `<div class="pv wordle">${["no", "near", "yes", "no", "yes"].map((h, k) => `<i class="${h}">${"TITAN"[k]}</i>`).join("")}</div>`,
};
const JP = {
  watch: "みる",
  party: "みんなで",
  koikoi: "はなふだ",
  daily: "デイリー",
  guess: "あてる",
  hl: "くらべる",
  riddle: "なぞなぞ",
  scramble: "ならべる",
  coverle: "ズーム",
  timeline: "ねんぴょう",
  connections: "グループ",
  redacted: "くろぬり",
  grid: "マスめ",
  hangman: "もじあて",
  quiz: "クイズ",
  truths: "うそ",
  wordle: "ワードル",
  bingo: "ビンゴ",
  board: "ランキング",
};

const GAMES = [
  { key: "watch", color: "#e8664b", big: true, name: "Watch room", text: "The bot plays an anime for everyone in this call, in step. Opening one takes a watch ticket." },
  { key: "party", color: "#d2492f", big: true, name: "Play together", text: "Everyone in this call, one game: quiz show, imposter, draw it, hot potato and more." },
  { key: "daily", color: "#ef6a4c", big: true, name: "Daily puzzle", text: "One show a day for everyone. 8 tries." },
  { key: "koikoi", color: "#c9a227", name: "Koi-Koi", text: "Hanafuda, the flower card game, with anime covers. Play bots or the whole call." },
  { key: "guess", color: "#9b6bff", name: "Guess the anime", text: "A pixelated cover gets sharper with every clue." },
  { key: "hl", color: "#2fb8a6", name: "Higher or lower", text: "Which show scores higher? Keep the streak going." },
  { key: "riddle", color: "#f2a93b", name: "Emoji riddle", text: "A title told in emojis." },
  { key: "scramble", color: "#4c8dff", name: "Title scramble", text: "Put the letters back in order." },
  { key: "coverle", color: "#5fb3e8", name: "Zoomed cover", text: "A tiny piece of a cover. Each miss zooms out and shows how close you were." },
  { key: "timeline", color: "#8bc34a", name: "Timeline", text: "Put five shows in the order they aired." },
  { key: "connections", color: "#c77dff", name: "Connections", text: "Sixteen shows, four hidden groups." },
  { key: "redacted", color: "#a0a4ab", name: "Redacted", text: "A blacked-out synopsis. Guess words to open it, then name the show." },
  { key: "grid", color: "#ff8a5c", name: "Anime grid", text: "Fill each square with a show that fits its row and column." },
  { key: "hangman", color: "#3fc1c9", name: "Hangman", text: "A title or character name, letter by letter." },
  { key: "wordle", color: "#6aaa64", name: "Wordle", text: "A word from a title or a character's name. Green is right, yellow is elsewhere." },
  { key: "quiz", color: "#ffb547", name: "Quiz", text: "Ten questions about studios, years and characters." },
  { key: "truths", color: "#e05a7a", name: "Two truths, one lie", text: "Three facts about a show. Spot the fake one." },
  { key: "bingo", color: "#e8559a", name: "Watch bingo", text: "This month's card, filled from your AniList." },
  { key: "board", color: "#f2c14e", name: "Leaderboard", text: "Game points this week and all time." },
];

// chars splits text into what a person sees as single characters, so
// emojis made of several code points stay whole.
function chars(text) {
  if (window.Intl && Intl.Segmenter) return Array.from(new Intl.Segmenter().segment(text), (x) => x.segment);
  return Array.from(text);
}

// enter replays the page's slide-in.
function enter() {
  app.classList.remove("enter");
  void app.offsetWidth;
  app.classList.add("enter");
}

function shake(el) {
  if (!el) return;
  sfx("bad");
  el.classList.remove("shake");
  void el.offsetWidth;
  el.classList.add("shake");
}

// confetti drops a short burst of paper over the page.
function confetti(n = 70, prank = false) {
  // Fake wrong: the win shows a moment later, after the 残念 stamp.
  if (!prank && tv && tv.troll && tv.troll.fake_wrong && where === "party") {
    sfx("bad");
    return setTimeout(() => confetti(n, true), 2700);
  }
  sfx("win");
  if (matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  const colors = ["#d2492f", "#ef6a4c", "#f2c14e", "#f4efe6"];
  for (let i = 0; i < n; i++) {
    const p = document.createElement("i");
    p.className = "confetti";
    p.style.left = Math.random() * 100 + "vw";
    p.style.background = colors[i % colors.length];
    p.style.setProperty("--x", (Math.random() - 0.5) * 240 + "px");
    p.style.setProperty("--r", (Math.random() - 0.5) * 1440 + "deg");
    p.style.setProperty("--t", 1.6 + Math.random() * 1.6 + "s");
    p.style.animationDelay = Math.random() * 0.35 + "s";
    document.body.appendChild(p);
    setTimeout(() => p.remove(), 4000);
  }
}

// rain drops n copies of some HTML (faces, tomatoes, claps) over the page.
function rain(html, n = 36) {
  if (matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  for (let i = 0; i < n; i++) {
    const p = document.createElement("i");
    p.className = "confetti rain";
    p.innerHTML = html;
    p.style.left = Math.random() * 96 + "vw";
    p.style.setProperty("--x", (Math.random() - 0.5) * 120 + "px");
    p.style.setProperty("--r", (Math.random() - 0.5) * 720 + "deg");
    p.style.setProperty("--t", 2 + Math.random() * 1.8 + "s");
    p.style.animationDelay = Math.random() * 0.8 + "s";
    document.body.appendChild(p);
    setTimeout(() => p.remove(), 5000);
  }
}

// No scrollbar: when the page is longer than the window, the top line
// dims and a full-strength line fills it as you scroll down.
function scrollLine() {
  const root = document.documentElement;
  const room = root.scrollHeight - innerHeight;
  root.classList.toggle("long", room > 2);
  root.style.setProperty("--scrolled", room > 2 ? Math.min(100, (100 * scrollY) / room) + "%" : "0%");
}
addEventListener("scroll", scrollLine, { passive: true });
addEventListener("resize", scrollLine);
new MutationObserver(scrollLine).observe(app, { childList: true, subtree: true });
new ResizeObserver(scrollLine).observe(app);

// The background: faint diagonal lines of お任せ · OTAKASE, every other
// line drifting the other way. Each line holds its text twice, so sliding
// it by half loops without a jump.
(function bgText() {
  const el = document.getElementById("bgtext");
  if (!el) return;
  const run = "お任せ　OTAKASE　".repeat(14);
  el.innerHTML = Array.from({ length: 46 }, (_, k) => `<div style="--t:${80 + (k % 5) * 12}s">${run}${run}</div>`).join("");
})();

function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function norm(s) {
  return String(s)
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[^a-z0-9]/g, "");
}

async function api(path, body) {
  const opts = { headers: {} };
  if (session) opts.headers.Authorization = "Bearer " + session;
  if (body) {
    opts.method = "POST";
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const r = await fetch("api/" + path, opts);
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || "Something went wrong (" + r.status + ")");
  return data;
}

function message(text) {
  app.innerHTML = `<p class="msg">${esc(text)}</p>`;
  enter();
}

const loading = (text) => `<p class="msg loading">${esc(text)}</p>`;

// showPic loads the guess picture with the session header (an <img> can't
// send one) and shows it once it's in.
let picURL = "";
async function showPic(path) {
  const el = document.getElementById("pic");
  const r = await fetch(path, { headers: { Authorization: "Bearer " + session } }).catch(() => null);
  if (!r || !r.ok || !el.isConnected) return;
  if (picURL) URL.revokeObjectURL(picURL);
  picURL = URL.createObjectURL(await r.blob());
  el.src = picURL;
}

function cover(id) {
  return `api/cover/${encodeURIComponent(id)}`;
}

function openLink(url) {
  if (sdk) sdk.commands.openExternalLink({ url }).catch(() => {});
}

function page(title, body) {
  clearTimeout(pending);
  if (where === "watch") watchLeave();
  where = "";
  app.innerHTML = `${launchMode === "watch" ? "" : `<button class="back" id="back">${icon("back")} Games</button>`}<header><h1>${title}</h1><span class="pill" id="side" hidden></span></header>${body}`;
  const back = document.getElementById("back");
  if (back) back.onclick = menu;
  enter();
}

// side sets the pill next to the page title.
function side(html) {
  const el = document.getElementById("side");
  el.innerHTML = html;
  el.hidden = !html;
}

function showError(e) {
  const err = document.getElementById("err");
  if (err) err.textContent = e.message;
  else message(e.message);
}

async function start() {
  const cfg = await api("config");
  gamesOff = cfg.off || [];
  if (!cfg.ready) return message("The Otakase Activity isn't set up yet. Play in chat: /leaderboard lists every game.");
  if (!new URLSearchParams(location.search).get("frame_id")) return message("Open this inside Discord: /activity on the Otakase server.");
  sdk = new Discord.DiscordSDK(cfg.client_id);
  await sdk.ready();
  const { code } = await sdk.commands.authorize({
    client_id: cfg.client_id,
    response_type: "code",
    state: "",
    prompt: "none",
    scope: ["identify"],
  });
  const tok = await api("token", { code });
  session = tok.session;
  launchMode = ["games", "watch"].includes(tok.mode) ? tok.mode : "";
  me = tok.name || "";
  try {
    await sdk.commands.authenticate({ access_token: tok.access_token });
  } catch (_) {
    // Only needed for SDK features this page doesn't use.
  }
  channel = sdk.channelId || "";
  room = String(sdk.instanceId || channel || "solo").replace(/[^A-Za-z0-9_.:-]/g, "").slice(0, 100) || "solo";
  [titles, daily] = await Promise.all([api("titles"), api("daily").catch(() => null)]);
  joinRoom();
  if (launchMode === "watch") watchView().catch(showError);
  else menu();
}

// ── Menu ──

function menu() {
  if (launchMode === "watch") {
    if (where !== "watch") watchView().catch(showError);
    return;
  }
  clearTimeout(pending);
  if (where === "watch") watchLeave();
  where = "menu";
  const tag = (g) => {
    if (g.key === "party") return `<span class="tag live" id="ptag">${partyTag()}</span>`;
    if (g.key !== "daily" || !daily) return "";
    if (daily.play.done) return `<span class="tag done">#${daily.num} · ${esc(daily.score)}</span>`;
    return `<span class="tag live">#${daily.num} · ${daily.play.guesses.length}/${daily.tries} · today's puzzle</span>`;
  };
  app.innerHTML = `
    <header class="hero"><div><h1>Otakase <span>Activity</span></h1><p class="sub">${launchMode === "games" ? "Pick a game to play." : "Watch anime together or pick a game to play."}</p></div>${
      me ? `<span class="pill">${esc(me)}</span>` : ""
    }</header>
    <div class="menu">${GAMES.filter((g) => launchMode !== "games" || g.key !== "watch").map(
      (g, i) =>
        `<button class="card${g.big ? " big" : ""}${gamesOff.includes(g.key) ? " off" : ""}" data-game="${g.key}" style="--c:${g.color};--i:${i}"><span class="jp">${JP[g.key]}</span>${PREVIEWS[
          g.key
        ]()}<span class="no">${String(i + 1).padStart(2, "0")}</span><h3>${esc(g.name)}</h3><p>${esc(g.text)}</p>${tag(g)}</button>`
    ).join("")}</div>`;
  app.querySelectorAll("[data-game]").forEach((el) => (el.onclick = () => (el.classList.contains("off") ? message("Staff turned this game off for now.") : open(el.dataset.game))));
}

function open(game) {
  if ((launchMode === "games" && game === "watch") || (launchMode === "watch" && game !== "watch")) return;
  const views = { watch: watchView, party: partyView, koikoi: kkOpen, daily: dailyView, guess: soloStart, riddle: soloStart, scramble: soloStart, hl: hlStart, bingo: bingoView, board: boardView };
  Promise.resolve((views[game] || pzOpen)(game)).catch(showError);
}

// ── Search box ──

// search wires a title box: pick(t) gets the picked title, enter(text)
// typed text when nothing is picked (or null to ignore).
function search(skip, pick, enter) {
  const q = document.getElementById("q");
  const list = document.getElementById("list");
  let shown = [];
  let k = 0;
  const draw = () => {
    list.hidden = shown.length === 0;
    list.innerHTML = shown
      .map((t, i) => `<button data-k="${i}" class="${i === k ? "on" : ""}">${esc(t.name)}${t.year ? ` <small>${t.year}</small>` : ""}</button>`)
      .join("");
  };
  q.oninput = () => {
    const n = norm(q.value);
    k = 0;
    shown = n.length < 2 ? [] : titles.filter((t) => !skip.has(t.id) && [t.name, ...(t.other || [])].some((x) => norm(x).includes(n))).slice(0, 8);
    draw();
  };
  q.onkeydown = (e) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (shown.length) k = (k + (e.key === "ArrowDown" ? 1 : shown.length - 1)) % shown.length;
      draw();
    } else if (e.key === "Enter") {
      if (shown[k]) pick(shown[k]);
      else if (enter && q.value.trim()) enter(q.value.trim());
    }
  };
  list.onclick = (e) => {
    const b = e.target.closest("button");
    if (b) pick(shown[+b.dataset.k]);
  };
  q.focus();
}

const searchBox = (placeholder) =>
  `<div class="search"><input id="q" placeholder="${esc(placeholder)}" autocomplete="off" spellcheck="false"><div class="list" id="list" hidden></div></div><div class="err" id="err"></div>`;

// ── Daily puzzle ──

async function dailyView() {
  daily = await api("daily");
  renderDaily();
}

function tryRow(g, fresh) {
  const cells = g.cells
    .map(
      (c, i) =>
        `<div class="cell ${esc(c.hit)}" style="--i:${i}" title="${esc(c.label)}: ${LABELS[c.hit]}"><b>${esc(c.label)}</b><span>${esc(c.text)}${
          ARROWS[c.arrow] || ""
        }</span></div>`
    )
    .join("");
  return `<div class="try${fresh ? " fresh" : ""}"><img src="${cover(g.id)}" alt="" loading="lazy"><div class="body"><div class="name">${esc(
    g.name
  )}</div><div class="cells">${cells}</div></div></div>`;
}

function dailyResult() {
  const s = daily.stats || {};
  const pct = s.played ? Math.round((100 * s.won) / s.played) : 0;
  const head = daily.play.solved ? `Solved in ${daily.score}` : "Not this time";
  return `<section class="result">${stamp(daily.play.solved)}
    <img src="${cover(daily.answer.id)}" alt="">
    <div>
      <p class="muted" style="margin:0">${esc(head)} · the answer was</p>
      <h2>${esc(daily.answer.name)}</h2>
      <a id="open">Open on AniList</a>
      <div class="stats">
        <div><strong>${s.played || 0}</strong><small>Played</small></div>
        <div><strong>${pct}%</strong><small>Won</small></div>
        <div><strong>${s.streak || 0}</strong><small>Streak</small></div>
        <div><strong>${s.best || 0}</strong><small>Best</small></div>
      </div>
      <pre class="grid">${esc(daily.grid)}</pre>
      <p class="note">Your grid is posted in the channel. Next puzzle <span id="next"></span>.</p>
    </div>
  </section>`;
}

function renderDaily() {
  const p = daily.play;
  const left = daily.tries - p.guesses.length;
  const tries = p.guesses
    .slice()
    .reverse()
    .map((g, i) => tryRow(g, freshDaily && i === 0))
    .join("");
  const solvedNow = freshDaily && p.solved;
  freshDaily = false;
  page(
    `Daily <span>#${daily.num}</span>`,
    `${p.done ? dailyResult() : searchBox("Type an anime title…")}
    <div class="legend"><span><i style="background:var(--yes)"></i>Right</span><span><i style="background:var(--near)"></i>Close</span><span>↑ ↓ the answer's is higher or lower</span></div>
    ${tries || `<p class="note">One popular show a day, the same for everyone. Each try shows how close its year, format, episodes, score, studio, source, genres and popularity are.</p>`}
    <section class="friends" id="friends" hidden></section>`
  );
  dailyFriends();
  side(p.done ? esc(daily.score) : `${left} ${left === 1 ? "try" : "tries"} left`);
  if (solvedNow) setTimeout(() => confetti(), 700);
  if (p.done) {
    document.getElementById("open").onclick = () => openLink(daily.answer.url);
    tick();
  } else {
    search(new Set(p.guesses.map((g) => g.id)), (t) => dailyGuess(t.id), null);
  }
}

function tick() {
  const el = document.getElementById("next");
  if (!el) return;
  const s = Math.max(0, daily.next - Math.floor(Date.now() / 1000));
  if (s === 0) return dailyView().catch(showError);
  el.textContent = `in ${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
  setTimeout(tick, 30000);
}

async function dailyGuess(id) {
  if (busy) return;
  busy = true;
  try {
    daily = await api("guess", { id, channel });
    freshDaily = true;
    renderDaily();
    if (!daily.play.done) shake(document.querySelector(".try.fresh img"));
  } catch (e) {
    showError(e);
  } finally {
    busy = false;
  }
}

// ── Guess, riddle, scramble ──

const SOLO = {
  guess: { title: "Guess the <span>anime</span>", ask: "Which anime is this?" },
  riddle: { title: "Emoji <span>riddle</span>", ask: "Which anime do these emojis tell?" },
  scramble: { title: "Title <span>scramble</span>", ask: "Unscramble the title." },
};

async function soloStart(game) {
  page(SOLO[game].title, loading("Picking a show…"));
  renderSolo(await api(`play/${game}/start`, {}));
}

// stageHTML is what a round puts in front of you: the cover, the emojis
// or the scrambled letters.
function stageHTML(game, puzzle) {
  if (game === "guess") return `<img class="pic" id="pic" alt="">`;
  if (game === "riddle")
    return `<div class="emojis">${chars(puzzle)
      .map((c, i) => (c.trim() ? `<span style="--i:${i}">${esc(c)}</span>` : " "))
      .join("")}</div>`;
  // Letters come spaced, words three spaces apart: "T C N A K A   N O".
  let i = 0;
  const words = puzzle.trim().split(/\s{2,}/);
  return `<p class="wcount">${words.length} ${words.length === 1 ? "word" : "words"}</p><div class="letters">${words
    .map(
      (w) =>
        `<span class="word">${w
          .split(/\s+/)
          .map((c) => `<span class="l" style="--i:${i++}">${esc(c)}</span>`)
          .join("")}</span>`
    )
    .join("")}</div>`;
}

function renderSolo(v) {
  const g = SOLO[v.game];
  const stage = stageHTML(v.game, v.puzzle);
  const clues = v.clues.length
    ? `<ul class="clues">${v.clues.map((c, i) => `<li style="animation-delay:${i * 60}ms"><b>${i + 1}.</b> ${clueHTML(c)}</li>`).join("")}</ul>`
    : "";
  let foot;
  if (v.done) {
    const line = v.right
      ? `<p class="won">Right! ${v.points ? "+1 game point" : esc(v.note || "No point: today's 10 Activity points are reached.")}</p>`
      : `<p class="lost">The answer was:</p>`;
    foot = `<section class="result" style="margin-top:12px">${stamp(v.right)}
      ${v.cover ? `<img src="${cover(v.cover)}" alt="">` : ""}
      <div>${line}<h2>${esc(v.answer)}</h2>${v.url ? `<a id="open">Open on AniList</a>` : ""}
      <div class="row" style="margin-top:14px"><button class="btn" id="again">Next round</button></div></div></section>`;
  } else {
    foot = `${searchBox("Type the title…")}
      <div class="row"><button class="btn alt" id="hint" ${v.more ? "" : "disabled"}>Hint</button><button class="btn alt" id="give">Give up</button>
      <span class="muted">${v.left} wrong ${v.left === 1 ? "try" : "tries"} left · a wrong try shows the next clue</span></div>`;
  }
  page(g.title, `<div class="stage"><p class="muted" style="margin-top:0">${esc(g.ask)}</p>${stage}${clues}</div>${foot}`);
  if (v.pic) showPic(v.pic);
  if (v.done) {
    if (v.right) confetti();
    document.getElementById("again").onclick = () => soloStart(v.game).catch(showError);
    const o = document.getElementById("open");
    if (o) o.onclick = () => openLink(v.url);
    return;
  }
  const send = (body) => {
    if (busy) return;
    busy = true;
    api(`play/${v.game}/answer`, body)
      .then((nv) => {
        renderSolo(nv);
        if (!nv.done && body.text) {
          const err = document.getElementById("err");
          if (err) err.textContent = `Not ${body.text}.`;
          shake(document.getElementById("q"));
        }
      })
      .catch(showError)
      .finally(() => (busy = false));
  };
  search(new Set(), (t) => send({ text: t.name }), (text) => send({ text }));
  document.getElementById("hint").onclick = () => api(`play/${v.game}/hint`, {}).then(renderSolo).catch(showError);
  document.getElementById("give").onclick = () => send({ give_up: true });
}

// clueHTML draws title-blank clues ("Starts with: S _ _   C _ _") as a
// row of boxes per word, so where one word ends is plain to see. Other
// clues stay text.
function clueHTML(c) {
  const m = /^([^:]+:\s*)(.+)$/.exec(c);
  if (!m || !m[2].includes("_") || !/^(\S( |$))+/.test(m[2].replace(/ {2,}/g, " "))) return esc(c);
  const words = m[2].trim().split(/\s{2,}/);
  if (words.some((w) => w.split(" ").some((ch) => [...ch].length > 1))) return esc(c);
  return `${esc(m[1])}<span class="blanks">${words
    .map((w) => `<span class="bw">${w.split(" ").map((ch) => `<i>${ch === "_" ? "" : esc(ch)}</i>`).join("")}</span>`)
    .join("")}</span>`;
}

// ── Higher or lower ──

let hlStat = "score";

async function hlStart() {
  page(
    "Higher or <span>lower</span>",
    `<div class="hlstart">
      <div class="demo"><div class="mini a">91%</div><div class="vs">VS</div><div class="mini b">?</div></div>
      <p class="lead">Two shows, one number. Is the second higher or lower?</p>
      <p class="muted">Pick what to compare</p>
      <div class="modes">
        <button class="mode" data-stat="score"><span class="ic">${icon("star")}</span><b>AniList score</b><small>Mean score from AniList members, like 85%</small></button>
        <button class="mode" data-stat="popularity"><span class="ic">${icon("users")}</span><b>Popularity</b><small>How many people have it on their AniList</small></button>
      </div>
      <p class="rules">Keep the streak going. A game point for every 3 in a row, up to 5 a day.</p>
    </div>`
  );
  app.querySelectorAll("[data-stat]").forEach(
    (b) =>
      (b.onclick = () => {
        hlStat = b.dataset.stat;
        api("hl/start", { stat: hlStat }).then(renderHL).catch(showError);
      })
  );
}

// hlNum reads the number out of a value like "91%" or "123,456 members".
const hlNum = (text) => Number(String(text).replace(/[^\d.]/g, "")) || 0;

// later runs one pending step (next round, back to the menu); leaving the
// page cancels it.
let pending = 0;
function later(fn, ms) {
  clearTimeout(pending);
  pending = setTimeout(fn, ms);
}

function hlSide(s, value, extra = "") {
  return `<div class="side ${extra}"><img src="${cover(s.id)}" alt=""><div class="n">${esc(s.name)}</div><div class="v${
    value === "?" ? " q" : ""
  }">${esc(value)}</div><div class="meter"><i></i></div></div>`;
}

function renderHL(v) {
  const what = v.stat === "popularity" ? "popularity" : "AniList score";
  page(
    "Higher or <span>lower</span>",
    `<div class="hl">${hlSide(v.a, v.a.value)}<div class="vs">VS</div>${hlSide(v.b, "?")}</div>
     <p class="muted" style="text-align:center">Is the ${what} of <b>${esc(v.b.name)}</b> higher or lower?</p>
     <div class="row" style="justify-content:center" id="pick"><button class="btn" id="up">${icon("up")} Higher</button><button class="btn alt" id="down">${icon("down")} Lower</button></div>`
  );
  side(`${icon("flame")} ${v.streak} · best ${v.best}`);
  const answer = (higher) => {
    if (busy) return;
    busy = true;
    api("hl/answer", { higher })
      .then((nv) => hlReveal(v, nv))
      .catch(showError)
      .finally(() => (busy = false));
  };
  document.getElementById("up").onclick = () => answer(true);
  document.getElementById("down").onclick = () => answer(false);
}

// hlReveal shows both values: the higher show's card grows, the other
// shrinks, bars show how far apart they are. A right answer moves on by
// itself; a wrong one asks to play again before going back to the menu.
function hlReveal(v, nv) {
  const a = hlNum(v.a.value);
  const b = hlNum(nv.shown.value);
  const top = Math.max(a, b) || 1;
  const bWins = b > a;
  const mood = nv.right ? "good" : "bad";
  const diff = Math.abs(b - a);
  const diffText = (v.stat === "popularity" ? diff.toLocaleString("en-US") : diff) + " apart";
  page(
    "Higher or <span>lower</span>",
    `<div class="hl reveal ${mood}">${stamp(nv.right)}${hlSide(v.a, v.a.value, bWins ? "lose" : "win")}<div class="vs diff">${
      bWins ? "&lt;" : "&gt;"
    }<small>${esc(diffText)}</small></div>${hlSide(nv.shown, "0", bWins ? "win" : "lose")}</div>
     <p class="${nv.right ? "won" : "lost"} verdict">${nv.right ? "Right!" : "Wrong!"} ${esc(nv.shown.name)} is ${bWins ? "higher" : "lower"}.</p>
     <div id="after"></div>`
  );
  side(`${icon("flame")} ${nv.streak} · best ${nv.best}`);
  const sides = app.querySelectorAll(".hl .side");
  [a, b].forEach((n, i) => sides[i].querySelector(".meter i").style.setProperty("--w", (100 * n) / top + "%"));
  countUp(sides[1].querySelector(".v"), b, nv.shown.value);
  const after = document.getElementById("after");
  if (!nv.over) {
    after.innerHTML = `<div class="next"><div class="bar"><i></i></div><button class="btn alt" id="go">Next round</button></div>`;
    const go = () => renderHL(nv);
    document.getElementById("go").onclick = go;
    later(go, 3500);
    return;
  }
  if (!nv.right) shake(sides[bWins ? 0 : 1]);
  if (nv.right || (nv.streak > 0 && nv.streak >= nv.best)) confetti(40);
  after.innerHTML = `<div class="stage again"><h2 style="margin:0">${nv.streak} in a row</h2><p class="muted">Your best: ${nv.best}</p>
    <p>Play again?</p><div class="row" style="justify-content:center"><button class="btn" id="again">Play again</button><button class="btn alt" id="leave">Menu</button></div>
    <div class="bar slow"><i></i></div><p class="note" id="left"></p></div>`;
  const restart = () => api("hl/start", { stat: hlStat }).then(renderHL).catch(showError);
  document.getElementById("again").onclick = restart;
  document.getElementById("leave").onclick = menu;
  let t = 10;
  const tick = () => {
    const el = document.getElementById("left");
    if (!el) return;
    if (t <= 0) return menu();
    el.textContent = `Back to the menu in ${t}s`;
    t--;
    later(tick, 1000);
  };
  tick();
}

// countUp rolls a number up to its value, then shows the real text.
function countUp(el, to, text) {
  const start = performance.now();
  const step = (now) => {
    if (!el.isConnected) return;
    const k = Math.min(1, (now - start) / 900);
    el.textContent = Math.round(to * (1 - Math.pow(1 - k, 3))).toLocaleString("en-US") + (text.endsWith("%") ? "%" : "");
    if (k < 1) requestAnimationFrame(step);
    else el.textContent = text;
  };
  requestAnimationFrame(step);
}

// ── Bingo ──

async function bingoView() {
  page("Watch <span>bingo</span>", loading("Checking your AniList…"));
  const c = await api("bingo");
  const squares = c.squares
    .map((s, i) =>
      s.free
        ? `<div class="sq free" style="--i:${i}">Free</div>`
        : `<div class="sq ${s.show ? "on" : ""}" style="--i:${i}"><span>${esc(s.label)}</span>${s.show ? `<small>${icon("check")} ${esc(s.show)}</small>` : ""}</div>`
    )
    .join("");
  let note = "Finish shows this month and they fill the squares. 2 points a line, 5 for a full card.";
  if (!c.linked) note = "Link your AniList in chat with /link-anilist and your card fills itself.";
  if (c.private) note = "Your AniList list is private, so the card can't be filled.";
  page(
    `Watch <span>bingo</span>`,
    `<div class="bingo">${squares}</div>
     <p class="note">${c.got ? `<b class="won">+${c.got} game points!</b> ` : ""}${c.full ? "Full card! " : ""}${esc(note)}</p>`
  );
  side(`${esc(c.month)} · ${c.lines} of 8 lines`);
  if (c.got) confetti();
}

// ── Board ──

async function boardView() {
  page("Leader<span>board</span>", loading("Counting points…"));
  const b = await api("board");
  const list = (rows) => {
    if (!rows.length) return `<p class="msg">Nobody has points yet. Win a game to be first!</p>`;
    const top = rows[0].points || 1;
    const step = (r, i) =>
      r ? `<div class="p${i + 1}"><span class="m">${i + 1}</span><div class="name">${esc(r.name)}</div><div class="step">${r.points}</div></div>` : "<div></div>";
    const podium = `<div class="podium">${step(rows[1], 1)}${step(rows[0], 0)}${step(rows[2], 2)}</div>`;
    const rest = rows
      .map(
        (r, i) =>
          `<li class="${r.me ? "me" : ""}" style="--i:${i};--w:${Math.round((100 * r.points) / top)}%"><span class="bar"></span><span class="rank">${
            i + 1
          }</span><span class="who">${esc(r.name)}</span><b>${r.points}</b></li>`
      )
      .join("");
    return `${podium}<ol class="board">${rest}</ol>`;
  };
  const draw = (tab) => {
    page(
      "Leader<span>board</span>",
      `<div class="tabs"><button class="btn ${tab === "week" ? "" : "alt"}" data-tab="week">This week</button><button class="btn ${
        tab === "all" ? "" : "alt"
      }" data-tab="all">All time</button></div>${list(b[tab])}
      ${b.champions && b.champions.length ? `<p class="note">Game Champion: ${b.champions.map(esc).join(", ")}</p>` : ""}`
    );
    app.querySelectorAll("[data-tab]").forEach((el) => (el.onclick = () => draw(el.dataset.tab)));
  };
  draw("week");
}

// ── Play together ──
//
// Everyone who opens the Activity from the same call shares a room. The
// page joins it at start, then keeps one request waiting on the room's
// next change, so every screen moves at the same moment.

let room = "";
let tv = null; // the room as the server last showed it
let tvKey = ""; // what the party page has drawn, to know when to redraw
let tvPic = "";
let tvSkip = new Set();

const RACES = [
  { game: "guess", name: "Guess the anime", text: "One pixelated cover, sharper every few seconds." },
  { game: "riddle", name: "Emoji riddle", text: "A title in emojis, more clues as time runs." },
  { game: "scramble", name: "Title scramble", text: "Same letters for everyone. Fastest fingers win." },
];

const hue = (id) => Math.round(([...String(id)].reduce((h, c) => (h * 31 + c.charCodeAt(0)) % 9973, 7) * 137.5) % 360);
function initials(name) {
  const w = String(name).split(/\s+/).filter((x) => /^[\p{L}\p{N}]/u.test(x));
  return ((w[0] || "?")[0] + ((w[1] || "")[0] || "")).toUpperCase();
}
// av is a member's picture, their initials until it loads (or if it can't).
// An admin's prank can swap everyone's pictures around (tv.swap).
const av = (p) =>
  `<i class="av" style="--h:${hue(p.id || p.name)}">${esc(initials(p.name))}${
    p.id
      ? `<img src="api/avatar/${encodeURIComponent((tv && tv.swap && tv.swap[p.id]) || p.id)}" alt="" loading="lazy" onerror="this.remove()">`
      : ""
  }</i>`;

// faces are the menu card's picture: who is in the room right now.
function faces() {
  const ms = tv ? tv.members : [];
  if (!ms.length) return `<i class="av ghost"></i><i class="av ghost"></i><i class="av ghost"></i>`;
  return ms.slice(0, 5).map(av).join("") + (ms.length > 5 ? `<i class="av more">+${ms.length - 5}</i>` : "");
}

function partyTag() {
  if (!tv) return "joining…";
  const n = tv.members.length;
  if (tv.phase !== "lobby" && tv.phase !== "over") return `playing now · ${n} in`;
  return n === 1 ? "just you so far" : `${n} here`;
}

const meName = () => ((tv && tv.members.find((m) => m.id === tv.me)) || {}).name || me;
// Staff can do whatever the host can.
const amHost = () => tv && (tv.host === tv.me || !!tv.admin);

function setRoom(v) {
  if (!v || !v.members) return;
  tv = v;
  // Countdowns become wall-clock ends, so later redraws can't drift.
  const at = Date.now();
  if (v.race) v.race.end = at + v.race.left * 1000;
  if (v.pz) v.pz.end = at + v.pz.left * 1000;
  if (v.hl) v.hl.end = at + v.hl.left * 1000;
  for (const k of ["quiz", "imp", "draw", "bomb", "kk"]) if (v[k]) v[k].end = at + v[k].left * 1000;
  adminRoom(v);
  if (where === "party") drawParty();
  else if (where === "menu") {
    const t = document.getElementById("ptag");
    if (t) t.textContent = partyTag();
    const f = document.getElementById("pvparty");
    if (f) f.innerHTML = faces();
  }
}

async function joinRoom() {
  try {
    setRoom(await api("party/join", { room }));
  } catch (_) {
    return setTimeout(joinRoom, 5000);
  }
  for (;;) {
    const r = await fetch(`api/party/state?room=${encodeURIComponent(room)}&v=${tv.ver}`, { headers: { Authorization: "Bearer " + session } }).catch(
      () => null
    );
    const v = r && r.ok ? await r.json().catch(() => null) : null;
    if (v) setRoom(v);
    else await new Promise((ok) => setTimeout(ok, 3000));
  }
}

addEventListener("pagehide", () => {
  if (!room || !session) return;
  fetch("api/party/leave", {
    method: "POST",
    keepalive: true,
    headers: { Authorization: "Bearer " + session, "Content-Type": "application/json" },
    body: JSON.stringify({ room }),
  }).catch(() => {});
});

// One clock for every countdown on the page: [data-end] is a wall-clock
// end; with data-total it drains a bar, else it shows seconds.
setInterval(() => {
  const paused = tv && tv.paused && where === "party";
  document.querySelectorAll("[data-end]").forEach((el) => {
    if (paused) {
      if (!el.dataset.total) el.textContent = "❚❚";
      return;
    }
    let s = Math.max(0, (+el.dataset.end - Date.now()) / 1000);
    // The fast-timer prank: the clock drains twice as fast as it should.
    if (pranks.has("fast")) {
      if (el.dataset.fe !== el.dataset.end) (el.dataset.fe = el.dataset.end), (el.dataset.fs = s), (el.dataset.fa = Date.now());
      s = Math.max(0, +el.dataset.fs - (2 * (Date.now() - +el.dataset.fa)) / 1000);
    }
    if (el.dataset.total) el.style.width = Math.min(100, (100 * s) / +el.dataset.total) + "%";
    else {
      const left = Math.ceil(s);
      // The last seconds of a party round tick.
      if (where === "party" && left > 0 && left <= 5 && el.textContent !== left + "s" && el.closest(".rhead")) sfx("tick");
      el.textContent = left + "s";
    }
  });
}, 200);

function partyView() {
  page("Play <span>together</span>", `<div id="pty">${loading("Joining the room…")}</div>`);
  where = "party";
  tvKey = "";
  drawParty();
}

function partyAct(path, body) {
  return api("party/" + path, { room, ...body })
    .then((v) => setRoom(v.view || v))
    .catch(showError);
}

function drawParty() {
  const box = document.getElementById("pty");
  if (!box || !tv) return;
  const v = tv;
  let key = v.phase + ":" + (v.game || "");
  if (v.phase === "race" && v.race) key += `:${v.race.round}:${v.race.loading ? "l" : ""}${v.race.over ? "o" : ""}`;
  if (v.phase === "hl" && v.hl) key += `:${v.hl.round}:${v.hl.revealed ? "r" : ""}`;
  if (v.phase === "pz" && v.pz) key += `:${v.pz.round}:${v.pz.loading ? "l" : ""}${v.pz.over ? "o" : ""}`;
  if (v.phase === "quiz" && v.quiz) key += `:${v.quiz.index}:${v.quiz.open ? "" : "r"}`;
  if (v.phase === "imposter" && v.imp) key += `:${v.imp.round}:${v.imp.stage}`;
  if (v.phase === "draw" && v.draw) key += `:${v.draw.round}:${v.draw.stage}:${v.draw.got ? "g" : ""}`;
  if (v.phase === "bomb" && v.bomb) key += `:${v.bomb.stage}:${v.bomb.category}`;
  if (v.phase === "who" && v.who) key += `:${v.who.in ? "p" : "w"}`;
  if (v.phase === "koikoi" && v.kk) key += `:${v.kk.table}:${v.kk.hand}:${v.kk.stage === "done" ? "d" : ""}`;
  const fresh = key !== tvKey;
  tvKey = key;
  side(`${icon("party")} ${v.members.length}`);
  const draw = { lobby: drawLobby, race: drawRace, hl: drawHLParty, pz: drawPzParty, quiz: drawQuizParty, imposter: drawImp, draw: drawDraw, bomb: drawBomb, who: drawWho, koikoi: drawKK }[v.phase] || drawOver;
  draw(box, v, fresh);
  adminAfterDraw(box, v);
}

// fill swaps the party page's content, sliding it in when it's new.
function fill(box, html, fresh) {
  box.innerHTML = html;
  if (fresh) {
    box.classList.remove("in");
    void box.offsetWidth;
    box.classList.add("in");
  }
}

// ── Lobby ──

function drawLobby(box, v, fresh) {
  const host = v.members.find((m) => m.host) || {};
  const people = v.members
    .map(
      (m, i) =>
        `<div class="mem" style="--i:${i}">${av(m)}<span>${esc(m.name)}</span>${m.host ? `<em title="Host">${icon("crown")}</em>` : ""}${
          m.id === v.me ? "<small>you</small>" : ""
        }</div>`
    )
    .join("");
  const pick = `
    <h3 class="lh" id="kksec">Koi-Koi</h3>
    <p class="note">Hanafuda with anime covers. Bigger calls split into tables that play at once; bots fill the empty seats. <a class="lnk" id="kkhow">How to play</a></p>
    <div class="picks">${[2, 3, 4]
      .map(
        (n) =>
          `<button class="pick" data-game="koikoi" data-stat="${n}" style="--c:#c9a227">${icon("koikoi")}<b>${n === 2 ? "1v1" : n + " per table"}</b><small>${esc(
            kkSeats(v.members.length, n)
          )}</small></button>`
      )
      .join("")}</div>
    <h3 class="lh">Party games</h3>
    <p class="note">Made for a call: talk, draw and shout answers.</p>
    <div class="picks">${PARTY.map(
      (g) =>
        `<button class="pick" data-game="${g.game}" style="--c:${g.color}" ${v.members.length < g.min ? "disabled" : ""}>${icon(g.game)}<b>${esc(g.name)}</b><small>${esc(
          v.members.length < g.min ? `Needs ${g.min} players` : g.text
        )}</small></button>`
    ).join("")}</div>
    <h3 class="lh">Race rounds</h3>
    <p class="note">5 rounds, the same puzzle for everyone. The first right answer takes the round.</p>
    <div class="picks">${RACES.map(
      (g) =>
        `<button class="pick" data-game="${g.game}" style="--c:${GAMES.find((x) => x.key === g.game).color}">${icon(g.game)}<b>${esc(g.name)}</b><small>${esc(
          g.text
        )}</small></button>`
    ).join("")}</div>
    <h3 class="lh">Higher or lower party</h3>
    <p class="note">Everyone votes at once. Wrong votes are out, the last one standing wins.</p>
    <div class="picks two">
      <button class="pick" data-game="hl" data-stat="score" style="--c:#2fb8a6">${icon("star")}<b>AniList score</b><small>Mean score, like 85%</small></button>
      <button class="pick" data-game="hl" data-stat="popularity" style="--c:#2fb8a6">${icon("users")}<b>Popularity</b><small>People with it on their list</small></button>
    </div>
    <h3 class="lh">Same puzzle</h3>
    <p class="note">3 rounds. Everyone plays their own copy of the same puzzle; the first to solve takes the round.</p>
    <div class="picks">${PZ_PARTY.map(
      (g) =>
        `<button class="pick" data-game="${g.game}" data-stat="${g.mode || ""}" style="--c:${GAMES.find((x) => x.key === g.game).color}">${icon(g.game)}<b>${esc(
          g.name
        )}</b><small>${esc(g.text)}</small></button>`
    ).join("")}</div>`;
  fill(
    box,
    `<div class="lobby">
      <p class="muted lead">${v.members.length === 1 ? "Just you so far. Everyone who opens Otakase Activity in this call lands here." : `${v.members.length} people in this call.`}</p>
      <div class="people">${people}</div>
      ${amHost() ? pick : `<p class="wait">${icon("clock")} Waiting for <b>${esc(host.name || "the host")}</b> to pick a game</p>`}
      <div class="err" id="err"></div>
    </div>`,
    fresh
  );
  box.querySelectorAll("[data-game]").forEach((b) => {
    if (gamesOff.includes(b.dataset.game)) {
      b.disabled = true;
      b.classList.add("off");
      b.querySelector("small").textContent = "Turned off by staff";
    }
    b.onclick = () => partyAct("start", { game: b.dataset.game, stat: b.dataset.stat || "" });
  });
  const how = document.getElementById("kkhow");
  if (how) how.onclick = kkGuide;
}

// ── Race rounds ──

function drawRace(box, v, fresh) {
  const r = v.race;
  if (fresh) {
    tvPic = "";
    if (!r.over) tvSkip = new Set();
    const ask = { guess: "Which anime is this?", riddle: "Which anime do these emojis tell?", scramble: "Unscramble the title." }[v.game];
    let body;
    if (r.loading) body = loading("Picking a show…");
    else {
      const won = r.winner && r.winner === meName();
      const end = r.over
        ? `<section class="result">${stamp(won)}${r.cover ? `<img src="${cover(r.cover)}" alt="">` : ""}<div>
            <p class="${r.winner ? (won ? "won" : "muted") : "lost"}" style="margin:0">${
              r.winner ? (won ? "You got it! +1" : `${esc(r.winner)} got it`) : "Nobody got it"
            } · the answer was</p>
            <h2>${esc(r.answer)}</h2>${r.url ? `<a id="open">Open on AniList</a>` : ""}
            <p class="note">${r.round >= r.rounds ? "Final results" : "Next round"} in <b class="cd"></b></p></div></section>`
        : `${searchBox("Type the title…")}`;
      body = `<div class="stage"><p class="muted" style="margin-top:0">${esc(ask)}</p>${stageHTML(v.game, r.puzzle)}<ul class="clues" id="clues"></ul></div>${end}`;
    }
    fill(
      box,
      `<div class="rhead"><span class="pill">Round <b>${r.round}</b> of ${r.rounds}</span>${
        r.loading || r.over ? "" : `<span class="pill">${icon("clock")} <b class="cd"></b></span>`
      }</div>${body}
      <div class="racelow"><div><h3 class="lh">Answers</h3><ul class="feed" id="feed"></ul></div><div><h3 class="lh">Points</h3><ol class="board mini" id="stand"></ol></div></div>`,
      fresh
    );
    if (r.over && r.winner === meName()) confetti(40);
    const o = document.getElementById("open");
    if (o) o.onclick = () => openLink(r.url);
    if (!r.over && !r.loading) {
      const send = (text, id) => {
        if (busy) return;
        busy = true;
        if (id) tvSkip.add(id);
        api("party/answer", { room, text })
          .then((res) => {
            if (!res.right) {
              const q = document.getElementById("q");
              document.getElementById("err").textContent = `Not ${text}.`;
              if (q) (q.value = ""), shake(q);
              document.getElementById("list").hidden = true;
            }
            setRoom(res.view);
          })
          .catch(showError)
          .finally(() => (busy = false));
      };
      search(tvSkip, (t) => send(t.name, t.id), (text) => send(text));
    }
  }
  box.querySelectorAll(".cd").forEach((el) => (el.dataset.end = r.end));
  const clues = document.getElementById("clues");
  if (clues && clues.children.length !== r.clues.length) {
    clues.innerHTML = r.clues.map((c, i) => `<li${i < clues.children.length ? ' style="animation:none"' : ""}><b>${i + 1}.</b> ${clueHTML(c)}</li>`).join("");
  }
  if (r.pic && r.pic !== tvPic && document.getElementById("pic")) {
    tvPic = r.pic;
    showPic(r.pic);
  }
  document.getElementById("feed").innerHTML =
    r.feed.map((f, i) => `<li data-i="${i}" class="${f.ok ? "ok" : ""}"><b>${esc(f.name)}</b> ${f.ok ? `${icon("check")} got it` : esc(f.text)}</li>`).join("") ||
    `<li class="none">Wrong answers show up here.</li>`;
  document.getElementById("stand").innerHTML = standings(r.standings);
}

function standings(rows) {
  if (!rows || !rows.length) return `<li class="none">No points yet.</li>`;
  // The fake-leaderboard prank: they see themselves first with 9999.
  if (pranks.has("fake_top") && tv) rows = [{ ...(rows.find((p) => p.id === tv.me) || { id: tv.me, name: meName() }), points: 9999 }, ...rows.filter((p) => p.id !== tv.me)];
  const top = rows[0].points || 1;
  return rows
    .map(
      (p, i) =>
        `<li class="${p.id === tv.me ? "me" : ""}" style="--i:${i};--w:${Math.round((100 * (p.points || 0)) / top)}%"><span class="bar"></span><span class="rank">${
          i + 1
        }</span>${av(p)}<span class="who">${esc(p.name)}</span><b>${p.points || 0}</b></li>`
    )
    .join("");
}

// ── Higher or lower party ──

function drawHLParty(box, v, fresh) {
  const h = v.hl;
  const what = h.stat === "popularity" ? "popularity" : "AniList score";
  const inCount = h.players.filter((p) => !p.out).length;
  if (fresh) {
    let mid;
    if (!h.revealed) {
      mid = h.in
        ? `<p class="muted" style="text-align:center">Is the ${what} of <b>${esc(h.b.name)}</b> higher or lower?</p>
           <div class="row" style="justify-content:center"><button class="btn" id="up">${icon("up")} Higher</button><button class="btn alt" id="down">${icon(
             "down"
           )} Lower</button></div>`
        : `<p class="note" style="text-align:center">You're out this game. Watching the rest.</p>`;
    } else {
      const right = h.mine !== undefined && h.mine === h.higher;
      const outNow = h.players.filter((p) => p.right === false).length;
      mid = `<p class="${h.in || right ? "won" : "lost"} verdict">${esc(h.b.name)} is ${h.higher ? "higher" : "lower"}. ${
        outNow ? `${outNow} out.` : "Nobody out."
      }</p>`;
    }
    const a = hlNum(h.a.value);
    const b = hlNum(h.b.value);
    const mineRight = h.revealed && h.mine !== undefined && h.mine === h.higher;
    fill(
      box,
      `<div class="rhead"><span class="pill">Pair <b>${h.round}</b></span><span class="pill">${icon("party")} ${inCount} in</span></div>
      <div class="hl${h.revealed ? ` reveal ${mineRight ? "good" : "bad"}` : ""}">${h.revealed && h.mine !== undefined ? stamp(mineRight) : ""}${hlSide(
        h.a,
        h.a.value,
        h.revealed ? (h.higher ? "lose" : "win") : ""
      )}<div class="vs${h.revealed ? " diff" : ""}">${h.revealed ? `${h.higher ? "&lt;" : "&gt;"}<small>${(h.stat === "popularity" ? Math.abs(b - a).toLocaleString("en-US") : Math.abs(b - a)) + " apart"}</small>` : "VS"}</div>${hlSide(
        h.b,
        h.revealed ? "0" : "?",
        h.revealed ? (h.higher ? "win" : "lose") : ""
      )}</div>
      ${mid}
      <div class="bar tbar"><i data-total="${h.revealed ? 5 : 12}"></i></div>
      <div class="players" id="players"></div>`,
      fresh
    );
    if (h.revealed) {
      const top = Math.max(a, b) || 1;
      const sides = box.querySelectorAll(".hl .side");
      [a, b].forEach((n, i) => sides[i].querySelector(".meter i").style.setProperty("--w", (100 * n) / top + "%"));
      countUp(sides[1].querySelector(".v"), b, h.b.value);
    } else if (h.in) {
      document.getElementById("up").onclick = () => partyAct("vote", { higher: true });
      document.getElementById("down").onclick = () => partyAct("vote", { higher: false });
    }
  }
  box.querySelector(".tbar i").dataset.end = h.end;
  ["up", "down"].forEach((id) => {
    const el = document.getElementById(id);
    if (!el) return;
    el.disabled = h.mine !== undefined;
    el.classList.toggle("chosen", h.mine === (id === "up"));
  });
  document.getElementById("players").innerHTML = h.players
    .map((p) => {
      const mark = h.revealed && p.right !== undefined ? (p.right ? "yes" : "no") : "";
      const tick = !h.revealed && p.voted ? `<em>${icon("check")}</em>` : "";
      return `<span class="mem ${p.out ? "out" : ""} ${mark}">${av(p)}<span>${esc(p.name)}</span>${tick}</span>`;
    })
    .join("");
}

// ── Game over ──

function drawOver(box, v, fresh) {
  if (!fresh) return;
  const won = v.winners && v.winners.includes(meName());
  let head = "Nobody won";
  if (v.winners && v.winners.length) head = `${v.winners.map(esc).join(" & ")} ${v.winners.length > 1 ? "win" : "wins"}`;
  let detail = "";
  if (v.race) detail = `<ol class="board">${standings(v.race.standings)}</ol>`;
  if (v.pz) detail = `<ol class="board">${standings(v.pz.standings)}</ol>`;
  for (const k of ["quiz", "imp", "draw"]) if (v[k]) detail = `<ol class="board">${standings(v[k].standings)}</ol>`;
  if (v.bomb)
    detail = `<div class="players">${v.bomb.players
      .map((p) => `<span class="mem ${p.out ? "out" : ""}">${av(p)}<span>${esc(p.name)}</span></span>`)
      .join("")}</div>`;
  if (v.who) detail = whoEnd(v.who);
  if (v.kk) detail = `<ol class="board">${standings(v.kk.standings)}</ol>`;
  if (v.hl)
    detail = `<p class="muted">${v.hl.round} ${v.hl.round === 1 ? "pair" : "pairs"} played</p><div class="players">${v.hl.players
      .map((p) => `<span class="mem ${p.out ? "out" : ""}">${av(p)}<span>${esc(p.name)}</span></span>`)
      .join("")}</div>`;
  const again = { game: v.game, stat: v.hl ? v.hl.stat : v.pz ? v.pz.mode || "" : v.kk ? String(v.kk.size) : "" };
  fill(
    box,
    `<section class="stage over">${stamp(won)}<p class="muted" style="margin:0">Game over</p><h2>${head}</h2>${detail}
      ${
        amHost()
          ? `<div class="row" style="justify-content:center;margin-top:16px"><button class="btn" id="again">Play again</button><button class="btn alt" id="lobby">Back to lobby</button></div>`
          : `<p class="wait">${icon("clock")} Waiting for the host</p>`
      }<div class="err" id="err"></div></section>`,
    fresh
  );
  if (won) confetti();
  if (amHost()) {
    document.getElementById("again").onclick = () => partyAct("start", again);
    document.getElementById("lobby").onclick = () => partyAct("lobby", {});
  }
}

// ── Daily puzzle with the call ──

// dailyFriends shows how everyone in the call is doing at today's puzzle:
// colours only, never titles.
async function dailyFriends() {
  const el = document.getElementById("friends");
  if (!el || !room) return;
  const rows = await api("party/daily?room=" + encodeURIComponent(room)).catch(() => []);
  if (!el.isConnected) return;
  el.hidden = !rows.some((r) => !r.me);
  if (!el.hidden)
    el.innerHTML = `<h3 class="lh">${icon("party")} In this call</h3><div class="fgrid">${rows
      .map(
        (r) =>
          `<div class="friend ${r.me ? "me" : ""}"><div class="fn">${av(r)}<span>${esc(r.name)}</span></div><div class="mini">${
            r.rows.map((row) => `<div>${row.map((h) => `<i class="${esc(h)}"></i>`).join("")}</div>`).join("") || `<p class="note">No tries yet</p>`
          }</div><div class="fs ${r.solved ? "won" : r.done ? "lost" : ""}">${
            r.solved ? `Solved in ${r.rows.length}` : r.done ? "Missed" : `${r.rows.length}/${daily ? daily.tries : 8}`
          }</div></div>`
      )
      .join("")}</div>`;
  setTimeout(() => el.isConnected && dailyFriends(), 15000);
}

// ── Puzzles: zoomed cover, timeline, connections, redacted, grid, hangman ──
//
// The bot keeps each board; a move goes up and the board comes back as the
// page may see it. The same drawing serves a solo board (pz/{game}/move)
// and your own board in a party round (party/move).

const PZ = {
  coverle: { title: "Zoomed <span>cover</span>", side: (b) => `${b.max - b.tries.length} ${b.max - b.tries.length === 1 ? "try" : "tries"} left` },
  timeline: { title: "Time<span>line</span>", side: (b) => `${b.left} ${b.left === 1 ? "try" : "tries"} left` },
  connections: { title: "Connec<span>tions</span>", side: (b) => `${b.left} ${b.left === 1 ? "mistake" : "mistakes"} left` },
  redacted: { title: "Re<span>dacted</span>", side: (b) => `${b.guesses.length} ${b.guesses.length === 1 ? "guess" : "guesses"}` },
  grid: { title: "Anime <span>grid</span>", side: (b) => `${b.left} ${b.left === 1 ? "guess" : "guesses"} left` },
  hangman: { title: "Hang<span>man</span>", side: (b) => `${b.left} ${b.left === 1 ? "miss" : "misses"} left` },
  wordle: { title: "Word<span>le</span>", side: (b) => `${b.max - b.rows.length} ${b.max - b.rows.length === 1 ? "try" : "tries"} left` },
  quiz: { title: "Qu<span>iz</span>", side: (b) => `${b.right} right · ${Math.min(b.index + 1, b.total)} of ${b.total}` },
  truths: { title: "Two truths, <span>one lie</span>", side: (b) => `${b.right} right · ${Math.min(b.index + 1, b.total)} of ${b.total}` },
};

// The party games, with how many players each needs.
const PARTY = [
  { game: "quiz", color: "#ffb547", min: 1, name: "Quiz show", text: "Same question for all at once. Faster right answers score more." },
  { game: "truths", color: "#e05a7a", min: 1, name: "Two truths, one lie", text: "Three facts about a show. Spot the lie before the others." },
  { game: "imposter", color: "#8e7dff", min: 3, name: "Imposter", text: "Everyone gets the same show but one. Talk, then vote out the imposter." },
  { game: "draw", color: "#4fc3a1", min: 2, name: "Draw it", text: "One draws an anime title, the rest guess." },
  { game: "bomb", color: "#ff6b4a", min: 2, name: "Hot potato", text: "Name a show that fits before the bomb goes off." },
  { game: "who", color: "#5fa8ff", min: 2, name: "Guess who", text: "1v1. Ask yes or no questions in the call and find the other's character." },
];

const PZ_PARTY = [
  { game: "coverle", name: "Zoomed cover", text: "Same tiny crop for all. Fewest tries wins." },
  { game: "timeline", name: "Timeline", text: "Same five shows to put in airing order." },
  { game: "connections", name: "Connections", text: "Same sixteen shows, find the four groups." },
  { game: "redacted", name: "Redacted", text: "Same blacked-out synopsis." },
  { game: "grid", name: "Anime grid", text: "Same rules, fill all nine squares." },
  { game: "hangman", mode: "title", name: "Hangman: titles", text: "Same title, letter by letter." },
  { game: "hangman", mode: "character", name: "Hangman: names", text: "Same character name." },
  { game: "wordle", mode: "title", name: "Wordle: titles", text: "Same word from a title." },
  { game: "wordle", mode: "character", name: "Wordle: names", text: "Same character name." },
];

// The four connections colours, easiest first.
const CONN = ["#b58a2a", "#3f8f5a", "#4c8dff", "#9b6bff"];

let pzSolo = { game: "", mode: "" };

function pzOpen(game) {
  if (game === "hangman" || game === "wordle") return modeStart(game);
  return pzStart(game, "");
}

// modeStart asks hangman and wordle players what kind of word to guess.
function modeStart(game) {
  const wd = game === "wordle";
  page(
    PZ[game].title,
    `<div class="hlstart">
      <div class="demo">${
        wd
          ? `<div class="wd-demo">${["no", "near", "yes", "no", "yes"].map((h, k) => `<i class="${h}">${"TITAN"[k]}</i>`).join("")}</div>`
          : `<div class="hm-demo">${"N_R_T_".split("").map((c) => `<i>${c === "_" ? "" : c}</i>`).join("")}</div>`
      }</div>
      <p class="lead">${
        wd
          ? "Six tries to find the word. Green letters are in the right place, yellow ones are in the word somewhere else."
          : "Guess letters to open the word. Six misses and it's over."
      }</p>
      <p class="muted">Pick what to guess</p>
      <div class="modes">
        <button class="mode" data-mode="title"><span class="ic">${icon(game)}</span><b>${wd ? "Title words" : "Anime titles"}</b><small>${
          wd ? "One word from a title; after 3 tries, a hint" : "With the format, year and genres as a hint"
        }</small></button>
        <button class="mode" data-mode="character"><span class="ic">${icon("users")}</span><b>Character names</b><small>${
          wd ? "A main character's name; after 3 tries, the show" : "A main character; the show is the hint"
        }</small></button>
      </div>
      <p class="rules">A win is a game point, up to 10 a day from Activity games.</p>
    </div>`
  );
  app.querySelectorAll("[data-mode]").forEach((b) => (b.onclick = () => pzStart(game, b.dataset.mode).catch(showError)));
}

async function pzStart(game, mode) {
  pzSolo = { game, mode };
  page(PZ[game].title, loading("Making a puzzle…"));
  renderPzSolo(await api(`pz/${game}/start`, { mode }));
}

// pzResult is the end of a board: the stamp, the answer when there is one
// show to name, and extra (points, buttons).
function pzResult(game, o, extra) {
  const b = o.board;
  const ans = b.answer;
  let head = o.won ? `Solved in ${o.moves} ${o.moves === 1 ? "move" : "moves"}` : "Not this time";
  if (game === "coverle" && o.won) head = `Solved in ${b.tries.length} ${b.tries.length === 1 ? "try" : "tries"}`;
  if (game === "quiz" || game === "truths") head = `${b.right} of ${b.total} right${o.won ? "" : ` · ${b.need} needed`}`;
  if ((game === "hangman" || game === "wordle") && b.mode === "character" && ans) head += ` · ${b.word} from`;
  else if (game === "wordle" && ans) head += ` · "${b.word}" from`;
  else if (ans) head += " · the answer was";
  return `<section class="result">${stamp(o.won)}${ans ? `<img src="${cover(ans.id)}" alt="">` : ""}<div>
    <p class="${o.won ? "won" : "lost"}" style="margin:0">${esc(head)}</p>
    ${ans ? `<h2>${esc(ans.name)}</h2><a class="openans">Open on AniList</a>` : ""}${extra}</div></section>`;
}

function renderPzSolo(o) {
  const game = o.game;
  let top = "";
  if (o.done) {
    const pts = o.won ? `<p class="won">${o.points ? "+1 game point" : "No point: today's 10 Activity points are reached."}</p>` : "";
    top = pzResult(game, o, `${pts}<div class="row" style="margin-top:14px"><button class="btn" id="again">Next puzzle</button></div>`);
  }
  page(PZ[game].title, `${top}<div id="pzb"></div>`);
  side(esc(PZ[game].side(o.board)));
  if (o.done) {
    if (o.won) confetti();
    document.getElementById("again").onclick = () => pzStart(game, pzSolo.mode).catch(showError);
    const a = app.querySelector(".openans");
    if (a) a.onclick = () => openLink(o.board.answer.url);
  }
  pzDraw(document.getElementById("pzb"), game, o.board, "", (move) => api(`pz/${game}/move`, move).then(renderPzSolo));
}

// pzDraw draws a board; send(move) returns a promise that redraws.
function pzDraw(el, game, b, roomID, send) {
  let busy2 = false;
  const go = (move) => {
    if (busy2) return;
    busy2 = true;
    send(move)
      .catch(showError)
      .finally(() => (busy2 = false));
  };
  DRAW[game](el, b, go, roomID);
}

const DRAW = {
  // ── Zoomed cover ──
  coverle(el, b, go, roomID) {
    const steps = b.max + 1;
    const dots = Array.from({ length: steps }, (_, k) => `<i class="${k <= b.step ? "on" : ""}"></i>`).join("");
    const rows = b.tries
      .slice()
      .reverse()
      .map((t, i) => {
        const cell = (label, text, h, arrow) =>
          `<div class="cell ${esc(h)}" style="--i:${i === 0 ? 0 : 0}"><b>${label}</b><span>${esc(text)}${arrow ? ARROWS[arrow] : ""}</span></div>`;
        return `<div class="try${i === 0 ? " fresh" : ""}"><img src="${cover(t.id)}" alt="" loading="lazy"><div class="body">
          <div class="name">${esc(t.name)}</div>
          <div class="cells c4">${cell("Year", t.year, t.year_hit, t.arrow)}${cell("Studio", t.studio, t.studio_hit)}${cell("Genres", t.genres, t.genres_hit)}${cell(
            "Format",
            t.format,
            t.format_hit
          )}</div>
          <div class="close"><i style="width:${t.close}%"></i><span>${t.close}% close</span></div></div></div>`;
      })
      .join("");
    el.innerHTML = `<div class="stage"><p class="muted" style="margin-top:0">${b.done ? "The full cover" : "Which anime's cover is this?"}</p>
      <img class="pic zoomed" id="pic" alt=""><div class="zoomdots" title="Zoom">${dots}</div></div>
      ${b.done ? "" : `${searchBox("Type an anime title…")}<div class="row"><button class="btn alt" id="give">Give up</button><span class="muted">Each miss zooms out. Arrows point to the answer's year.</span></div>`}
      <div class="legend" style="margin-top:12px"><span><i style="background:var(--yes)"></i>Same</span><span><i style="background:var(--near)"></i>Close</span><span>% how close overall</span></div>
      ${rows}`;
    showPic(`api/pz/pic?room=${encodeURIComponent(roomID)}&step=${b.step}&t=${Date.now()}`);
    if (!b.done) {
      search(new Set(b.tries.map((t) => t.id)), (t) => go({ id: t.id }), null);
      document.getElementById("give").onclick = () => go({ give_up: true });
    }
  },

  // ── Timeline ──
  timeline(el, b, go) {
    const order = b.items.map((it) => it.id);
    const byID = Object.fromEntries(b.items.map((it) => [it.id, it]));
    const draw = () => {
      const list = order
        .map((id, k) => {
          const it = byID[id];
          const lock = b.done;
          return `<li class="tl ${it.hit || ""}" style="--i:${k}"><span class="pos">${k + 1}</span><img src="${cover(id)}" alt="" loading="lazy">
            <span class="tn">${esc(it.name)}${it.year ? `<small>${it.year}</small>` : ""}</span>${
              lock
                ? ""
                : `<span class="mv"><button class="btn alt" data-up="${k}" ${k === 0 ? "disabled" : ""} aria-label="Earlier">${icon("up")}</button><button class="btn alt" data-down="${k}" ${
                    k === order.length - 1 ? "disabled" : ""
                  } aria-label="Later">${icon("down")}</button></span>`
            }</li>`;
        })
        .join("");
      const answer = b.answer
        ? `<h3 class="lh">Right order</h3><ol class="tlans">${b.answer.map((a) => `<li><b>${a.year}</b> ${esc(a.name)}</li>`).join("")}</ol>`
        : "";
      el.innerHTML = `<p class="muted lead">Oldest at the top, newest at the bottom. Green ones are in the right place.</p>
        <ol class="tlist">${list}</ol>
        ${b.done ? "" : `<div class="err" id="err"></div><div class="row"><button class="btn" id="check">${icon("check")} Check order</button><button class="btn alt" id="give">Give up</button></div>`}
        ${answer}`;
      if (b.done) return;
      el.querySelectorAll("[data-up]").forEach((x) => (x.onclick = () => swap(+x.dataset.up, -1)));
      el.querySelectorAll("[data-down]").forEach((x) => (x.onclick = () => swap(+x.dataset.down, 1)));
      document.getElementById("check").onclick = () => go({ ids: order });
      document.getElementById("give").onclick = () => go({ give_up: true });
    };
    const swap = (k, d) => {
      [order[k], order[k + d]] = [order[k + d], order[k]];
      byID[order[k]].hit = byID[order[k + d]].hit = "";
      draw();
    };
    draw();
  },

  // ── Connections ──
  connections(el, b, go) {
    let tiles = b.tiles.slice();
    const picked = new Set();
    const draw = () => {
      const found = b.found
        .map(
          (f) =>
            `<div class="cg${f.found ? "" : " late"}" style="--g:${CONN[f.color]}"><b>${esc(f.label)}</b><span>${f.tiles.map((t) => esc(t.name)).join(" · ")}</span></div>`
        )
        .join("");
      const left = Array.from({ length: 4 }, (_, k) => `<i class="${k < b.left ? "on" : ""}"></i>`).join("");
      el.innerHTML = `${found}
        ${
          b.done
            ? ""
            : `<div class="ctiles">${tiles
                .map((t, i) => `<button class="ct${picked.has(t.id) ? " on" : ""}" data-id="${t.id}" style="--i:${i}"><span>${esc(t.name)}</span></button>`)
                .join("")}</div>
          <div class="err" id="err">${esc(b.note || "")}</div>
          <div class="row"><span class="muted">Mistakes left</span><span class="lives">${left}</span></div>
          <div class="row" style="margin-top:12px"><button class="btn" id="send" ${picked.size === 4 ? "" : "disabled"}>Submit</button><button class="btn alt" id="mix">${icon(
            "shuffle"
          )} Shuffle</button><button class="btn alt" id="clear" ${picked.size ? "" : "disabled"}>Deselect</button><button class="btn alt" id="give">Give up</button></div>`
        }`;
      if (b.done) return;
      el.querySelectorAll("[data-id]").forEach(
        (x) =>
          (x.onclick = () => {
            const id = +x.dataset.id;
            if (picked.has(id)) picked.delete(id);
            else if (picked.size < 4) picked.add(id);
            draw();
          })
      );
      document.getElementById("send").onclick = () => go({ ids: [...picked] });
      document.getElementById("mix").onclick = () => {
        tiles = tiles
          .map((t) => [Math.random(), t])
          .sort((x, y) => x[0] - y[0])
          .map((x) => x[1]);
        draw();
      };
      document.getElementById("clear").onclick = () => {
        picked.clear();
        draw();
      };
      document.getElementById("give").onclick = () => go({ give_up: true });
    };
    draw();
  },

  // ── Redacted ──
  redacted(el, b, go) {
    const toks = (list) =>
      list
        .map((t) => {
          if (!t.w) return esc(t.t);
          if (t.n) return `<span class="rx" style="--n:${t.n}" title="${t.n} letters"></span>`;
          return t.h ? `<mark>${esc(t.t)}</mark>` : esc(t.t);
        })
        .join("");
    const guesses = b.guesses
      .map((g) => `<span class="gw${g.hits > 0 ? " ok" : ""}">${esc(g.word)}${g.hits >= 0 ? ` <b>${g.hits}</b>` : ""}</span>`)
      .join("");
    el.innerHTML = `<div class="stage rd"><h2 class="rtitle">${toks(b.title)}</h2><p class="rbody">${toks(b.body)}</p></div>
      ${
        b.done
          ? ""
          : `<div class="search"><input id="word" placeholder="A word, or the whole title…" autocomplete="off" spellcheck="false"></div>
        <div class="err" id="err">${esc(b.note || "")}</div>
        <div class="row"><button class="btn" id="try">Guess</button><button class="btn alt" id="give">Give up</button><span class="muted">Open every word of the title to win.</span></div>`
      }
      ${guesses ? `<h3 class="lh">Your guesses</h3><div class="gws">${guesses}</div>` : ""}`;
    if (b.done) return;
    const box = document.getElementById("word");
    const send = () => box.value.trim() && go({ text: box.value.trim() });
    box.onkeydown = (e) => {
      if (e.key === "Enter") send();
    };
    document.getElementById("try").onclick = send;
    document.getElementById("give").onclick = () => go({ give_up: true });
    box.focus();
  },

  // ── Anime grid ──
  grid(el, b, go) {
    let cell = b.cells.findIndex((c) => !c.id);
    const used = new Set(b.cells.filter((c) => c.id).map((c) => c.id));
    const draw = () => {
      let html = `<div class="ag"><div class="agc"></div>${b.cols.map((c) => `<div class="agh">${esc(c)}</div>`).join("")}`;
      for (let r = 0; r < 3; r++) {
        html += `<div class="agh row">${esc(b.rows[r])}</div>`;
        for (let c = 0; c < 3; c++) {
          const k = r * 3 + c;
          const x = b.cells[k];
          if (x.id) html += `<div class="sq on agq"><img src="${cover(x.id)}" alt="" loading="lazy"><small>${esc(x.name)}</small></div>`;
          else if (b.done) html += `<div class="sq agq miss"><small>${x.eg ? `e.g. ${esc(x.eg)}` : "—"}</small></div>`;
          else html += `<button class="sq agq${k === cell ? " sel" : ""}" data-k="${k}">+</button>`;
        }
      }
      html += "</div>";
      const ask = cell >= 0 ? `<p class="muted">${esc(b.rows[Math.floor(cell / 3)])} <b>×</b> ${esc(b.cols[cell % 3])}</p>` : "";
      el.innerHTML = `${html}${
        b.done
          ? ""
          : `${ask}${searchBox("Type an anime title…")}<div class="row"><button class="btn alt" id="give">Give up</button><span class="muted">Each show once. Wrong picks use up guesses.</span></div>`
      }`;
      if (b.note && !b.done) document.getElementById("err").textContent = b.note;
      if (b.done) return;
      el.querySelectorAll("[data-k]").forEach(
        (x) =>
          (x.onclick = () => {
            cell = +x.dataset.k;
            draw();
          })
      );
      search(used, (t) => cell >= 0 && go({ cell, id: t.id }), null);
      document.getElementById("give").onclick = () => go({ give_up: true });
    };
    draw();
  },

  // ── Hangman ──
  hangman(el, b, go) {
    const words = b.words.map((w) => `<span class="bw">${w.map((c) => `<i class="${c ? "" : "e"}">${esc(c)}</i>`).join("")}</span>`).join("");
    const lives = Array.from({ length: 6 }, (_, k) => `<i class="${k < 6 - b.left ? "x" : ""}"></i>`).join("");
    const keys = "abcdefghijklmnopqrstuvwxyz"
      .split("")
      .map((c) => {
        const cls = b.hits.includes(c) ? "yes" : b.misses.includes(c) ? "no" : "";
        return `<button class="key ${cls}" data-l="${c}" ${cls || b.done ? "disabled" : ""}>${c.toUpperCase()}</button>`;
      })
      .join("");
    el.innerHTML = `<div class="stage"><p class="muted" style="margin-top:0">${esc(b.hint)}</p><div class="hm">${words}</div>
      <div class="hlives" title="Misses">${lives}</div></div>
      ${
        b.done
          ? ""
          : `<div class="keys">${keys}</div><div class="err" id="err"></div>
        <div class="row"><div class="search" style="flex:1;margin:0"><input id="solve" placeholder="Know it? Type the whole ${
          b.mode === "character" ? "name" : "title"
        }…" autocomplete="off" spellcheck="false"></div><button class="btn alt" id="give">Give up</button></div>
        <p class="note">A wrong full answer counts as a miss.</p>`
      }`;
    if (b.done) return;
    el.querySelectorAll("[data-l]").forEach((x) => (x.onclick = () => go({ letter: x.dataset.l })));
    const box = document.getElementById("solve");
    box.onkeydown = (e) => {
      e.stopPropagation();
      if (e.key === "Enter" && box.value.trim()) go({ text: box.value.trim() });
    };
    document.getElementById("give").onclick = () => go({ give_up: true });
    hangKeys = (l) => {
      const k = el.querySelector(`[data-l="${l}"]`);
      if (k && !k.disabled && el.isConnected) go({ letter: l });
    };
  },

  // ── Quiz and two truths, one lie ──
  quiz(el, b, go) {
    DRAW.qset(el, b, go);
  },
  truths(el, b, go) {
    DRAW.qset(el, b, go);
  },
  qset(el, b, go) {
    const last = b.last && !b.done ? qLast(b.last) : "";
    if (b.done) {
      el.innerHTML = `<h3 class="lh">Answers</h3>${b.review
        .map(
          (q, i) =>
            `<div class="qrev ${q.pick === q.answer ? "ok" : "bad"}"><b>${i + 1}.</b> ${esc(q.prompt)}<span>${esc(q.options[q.answer])}${
              q.pick !== q.answer && q.pick >= 0 ? ` <s>${esc(q.options[q.pick])}</s>` : ""
            }</span>${q.explain ? `<small>${esc(q.explain)}</small>` : ""}</div>`
        )
        .join("")}`;
      return;
    }
    const q = b.q;
    el.innerHTML = `${last}<div class="stage qstage">${q.cover ? `<img class="qcover" src="${cover(q.cover)}" alt="">` : ""}
      <p class="qnum">Question ${b.index + 1} of ${b.total}</p><h2 class="qprompt">${esc(q.prompt)}</h2></div>
      ${qOpts(q.options, {})}<div class="err" id="err"></div>
      <div class="row"><button class="btn alt" id="give">Give up</button><span class="muted">${b.need} of ${b.total} right wins.</span></div>`;
    el.querySelectorAll("[data-o]").forEach((x) => (x.onclick = () => go({ cell: +x.dataset.o })));
    document.getElementById("give").onclick = () => go({ give_up: true });
  },

  // ── Wordle ──
  wordle(el, b, go) {
    let typed = "";
    const draw = () => {
      const rows = [];
      for (let r = 0; r < b.max; r++) {
        const row = b.rows[r];
        let cells = "";
        for (let k = 0; k < b.len; k++) {
          if (row) cells += `<i class="${row.marks[k]}${r === b.rows.length - 1 ? " fresh" : ""}" style="--k:${k}">${esc(row.word[k])}</i>`;
          else if (r === b.rows.length && !b.done) cells += `<i class="${typed[k] ? "typed" : ""}">${esc(typed[k] || "")}</i>`;
          else cells += "<i></i>";
        }
        rows.push(`<div class="wr">${cells}</div>`);
      }
      const keys = ["qwertyuiop", "asdfghjkl", "zxcvbnm"]
        .map(
          (line, i) =>
            `<div class="wkrow">${i === 2 ? `<button class="key wide" data-w="enter">Enter</button>` : ""}${line
              .split("")
              .map((c) => `<button class="key ${b.keys[c] || ""}" data-w="${c}">${c.toUpperCase()}</button>`)
              .join("")}${i === 2 ? `<button class="key wide" data-w="back" aria-label="Delete">${icon("back")}</button>` : ""}</div>`
        )
        .join("");
      el.innerHTML = `<p class="muted lead" style="text-align:center">${esc(b.hint)} · ${b.len} letters</p>
        <div class="wd" style="--n:${b.len}">${rows.join("")}</div>
        ${
          b.done
            ? ""
            : `<div class="err" id="err" style="text-align:center"></div><div class="wkeys">${keys}</div>
          <div class="row" style="justify-content:center"><button class="btn alt" id="give">Give up</button></div>`
        }`;
      if (b.done) return;
      el.querySelectorAll("[data-w]").forEach((x) => (x.onclick = () => press(x.dataset.w)));
      document.getElementById("give").onclick = () => go({ give_up: true });
    };
    const press = (k) => {
      if (k === "enter") {
        if (typed.length !== b.len) return shake(el.querySelector(".wr:nth-child(" + (b.rows.length + 1) + ")"));
        return go({ text: typed });
      }
      if (k === "back") typed = typed.slice(0, -1);
      else if (/^[a-z]$/.test(k) && typed.length < b.len) typed += k;
      draw();
    };
    wordKeys = (k) => el.isConnected && press(k);
    draw();
  },
};

// qOpts draws the four answers. With an answer it marks right and wrong,
// and with counts it shows how many picked each.
const QSHAPES = ["#e8664b", "#4c8dff", "#d9a441", "#3f8f5a"];
function qOpts(opts, { answer, pick, counts, disabled }) {
  const shown = answer !== undefined && answer >= 0;
  // The lying-votes prank: the counts are passed round one place.
  if (counts && pranks.has("lie_votes")) counts = counts.map((_, i) => counts[(i + 1) % counts.length]);
  return `<div class="qopts">${opts
    .map((o, i) => {
      const cls = shown ? (i === answer ? "right" : i === pick ? "wrong" : "dim") : i === pick ? "chosen" : "";
      return `<button class="qo ${cls}" data-o="${i}" style="--q:${QSHAPES[i % 4]};--i:${i}" ${shown || disabled ? "disabled" : ""}><span class="ql">${"ABCD"[i]}</span><span>${esc(
        o
      )}</span>${counts ? `<em>${counts[i]}</em>` : ""}</button>`;
    })
    .join("")}</div>`;
}

// qLast is the answer to the question just played.
function qLast(q) {
  const ok = q.pick === q.answer;
  if (ok) sfx("ok");
  else sfx("bad");
  return `<div class="qlast ${ok ? "ok" : "bad"}">${icon(ok ? "check" : "back")}<div><b>${ok ? "Right!" : "Not quite."}</b> ${
    ok ? "" : `The answer: <b>${esc(q.options[q.answer])}</b>`
  }${q.explain ? `<small>${esc(q.explain)}</small>` : ""}</div></div>`;
}

// Typing on a wordle page fills the row.
let wordKeys = null;
addEventListener("keydown", (e) => {
  if (!wordKeys || e.ctrlKey || e.metaKey || e.altKey || /^(INPUT|TEXTAREA)$/.test((e.target || {}).tagName || "")) return;
  if (!document.querySelector(".wkeys")) return;
  if (e.key === "Enter") wordKeys("enter");
  else if (e.key === "Backspace") wordKeys("back");
  else if (/^[a-z]$/i.test(e.key)) wordKeys(e.key.toLowerCase());
});

// Typing a letter anywhere on a hangman page guesses it.
let hangKeys = null;
addEventListener("keydown", (e) => {
  if (!hangKeys || e.ctrlKey || e.metaKey || e.altKey || /^(INPUT|TEXTAREA)$/.test((e.target || {}).tagName || "")) return;
  if (/^[a-z]$/i.test(e.key) && document.querySelector(".keys")) hangKeys(e.key.toLowerCase());
});

// ── Same puzzle in a party ──

let tvBoard = "";

function drawPzParty(box, v, fresh) {
  const p = v.pz;
  if (fresh) {
    tvBoard = "";
    let body;
    if (p.loading) body = loading("Making a puzzle…");
    else if (p.over) {
      const won = p.board && p.board.won;
      const who = p.winner ? (p.winner === meName() ? "You solved it first! +1" : `${esc(p.winner)} solved it first`) : "Nobody solved it";
      body = p.board
        ? pzResult(v.game, p.board, `<p class="note">${who} · ${p.round >= p.rounds ? "Final results" : "Next round"} in <b class="cd"></b></p>`)
        : "";
      if (won && p.winner === meName()) confetti(40);
      body += `<div id="pzb"></div>`;
    } else body = `<div id="pzb"></div>`;
    fill(
      box,
      `<div class="rhead"><span class="pill">Round <b>${p.round}</b> of ${p.rounds}</span><span class="pill" id="pzside" hidden></span>${
        p.loading || p.over ? "" : `<span class="pill">${icon("clock")} <b class="cd"></b></span>`
      }</div>${body}
      <div class="racelow"><div><h3 class="lh">Players</h3><ul class="feed" id="pzp"></ul></div><div><h3 class="lh">Points</h3><ol class="board mini" id="stand"></ol></div></div>`,
      fresh
    );
    const a = box.querySelector(".openans");
    if (a && p.board && p.board.board.answer) a.onclick = () => openLink(p.board.board.answer.url);
  }
  box.querySelectorAll(".cd").forEach((el) => (el.dataset.end = p.end));
  const el = document.getElementById("pzb");
  if (el && p.board) {
    const key = JSON.stringify(p.board.board);
    if (key !== tvBoard) {
      tvBoard = key;
      pzDraw(el, v.game, p.board.board, room, (move) => api("party/move", { room, ...move }).then((r) => setRoom(r.view)));
    }
    const s = document.getElementById("pzside");
    if (s) {
      s.hidden = false;
      s.textContent = PZ[v.game].side(p.board.board);
    }
  }
  document.getElementById("pzp").innerHTML =
    p.players
      .map((x) => {
        const what = x.won ? `${icon(x.first ? "crown" : "check")} solved in ${x.moves}` : x.done ? "out" : `${x.moves} ${x.moves === 1 ? "move" : "moves"}`;
        return `<li class="pzpl${x.won ? " ok" : ""}">${av(x)}<b>${esc(x.name)}</b> ${what}</li>`;
      })
      .join("") || `<li class="none">Nobody has moved yet.</li>`;
  document.getElementById("stand").innerHTML = standings(p.standings);
}

// ── Quiz show ──

function drawQuizParty(box, v, fresh) {
  const q = v.quiz;
  const opts = () => qOpts(q.q.options, { answer: q.open ? undefined : q.answer, pick: q.mine, counts: q.open ? null : q.counts, disabled: q.mine >= 0 });
  if (fresh) {
    let after = "";
    if (!q.open) {
      const ok = q.mine === q.answer;
      if (q.mine >= 0) sfx(ok ? "ok" : "bad");
      after = `<p class="${ok ? "won" : "lost"} verdict">${q.mine < 0 ? "No answer" : ok ? `Right! +${q.gain}` : "Wrong"}</p>${
        q.explain ? `<p class="note" style="text-align:center">${esc(q.explain)}</p>` : ""
      }`;
    }
    fill(
      box,
      `<div class="rhead"><span class="pill">${v.game === "truths" ? "Show" : "Question"} <b>${q.index}</b> of ${q.total}</span>${
        q.open ? `<span class="pill">${icon("clock")} <b class="cd"></b></span>` : ""
      }</div>
      <div class="stage qstage">${q.q.cover ? `<img class="qcover" src="${cover(q.q.cover)}" alt="">` : ""}<h2 class="qprompt">${esc(q.q.prompt)}</h2></div>
      <div id="qo">${opts()}</div>${after}<div class="err" id="err"></div>
      <div class="bar tbar"><i data-total="${q.time}"></i></div>
      <div class="racelow"><div><h3 class="lh">Players</h3><div class="players" id="qp"></div></div><div><h3 class="lh">Points</h3><ol class="board mini" id="stand"></ol></div></div>`,
      fresh
    );
  } else document.getElementById("qo").innerHTML = opts();
  box.querySelectorAll(".cd").forEach((el) => (el.dataset.end = q.end));
  box.querySelector(".tbar i").dataset.end = q.end;
  if (q.open && q.mine < 0) box.querySelectorAll("[data-o]").forEach((x) => (x.onclick = () => partyAct("act", { kind: "answer", cell: +x.dataset.o })));
  document.getElementById("qp").innerHTML = q.players
    .map((p) => {
      const mark = !q.open && p.right !== undefined ? (p.right ? "yes" : "no") : "";
      const tick = q.open && p.voted ? `<em>${icon("check")}</em>` : "";
      return `<span class="mem ${mark}">${av(p)}<span>${esc(p.name)}</span>${tick}${!q.open && p.points ? `<small>+${p.points}</small>` : ""}</span>`;
    })
    .join("");
  document.getElementById("stand").innerHTML = standings(q.standings);
}

// ── Imposter ──

function drawImp(box, v, fresh) {
  const g = v.imp;
  const showCard = (s, label) =>
    `<div class="impcard"><img src="${cover(s.id)}" alt=""><div><small>${esc(label)}</small><h2>${esc(s.name)}</h2></div></div>`;
  let body = "";
  if (g.stage === "talk") {
    body = g.in
      ? `${showCard(g.show, "Your show")}
        <p class="lead">Take turns describing your show in the call without saying its name. Everyone has the same show except one person, and they don't know it's them.</p>
        ${amHost() ? `<div class="row"><button class="btn" id="votenow">Vote now</button></div>` : ""}`
      : `<p class="note">You're watching this round.</p>`;
  } else if (g.stage === "vote") {
    body = g.in
      ? `<p class="lead">Who had a different show? Tap them.</p><div class="players vote">${g.players
          .filter((p) => p.id !== v.me)
          .map(
            (p) =>
              `<button class="mem ${g.mine === p.id ? "yes" : ""}" data-v="${esc(p.id)}">${av(p)}<span>${esc(p.name)}</span>${p.voted ? `<em>${icon("check")}</em>` : ""}</button>`
          )
          .join("")}</div>${showCard(g.show, "Your show")}`
      : `<p class="note">Votes are coming in…</p>`;
  } else {
    const imp = g.players.find((p) => p.id === g.imposter) || { name: "Someone who left" };
    const me = g.imposter === v.me;
    const good = g.caught ? !me : me;
    if (fresh) sfx(good ? "ok" : "bad");
    body = `<section class="result impres">${stamp(good)}<div>
        <p class="${good ? "won" : "lost"}" style="margin:0">${g.caught ? "Caught!" : "The imposter got away!"} ${g.caught ? "+1 for everyone else" : "+2 for the imposter"}</p>
        <h2>${me ? "You were" : esc(imp.name) + " was"} the imposter</h2></div></section>
      <div class="impvs">${showCard(g.main, "Everyone had")}${showCard(g.odd, "The imposter had")}</div>
      <h3 class="lh">Votes</h3><div class="players">${g.players
        .map((p) => `<span class="mem ${p.out ? "no" : ""}">${av(p)}<span>${esc(p.name)}</span><small>${p.points || 0} ${p.points === 1 ? "vote" : "votes"}</small></span>`)
        .join("")}</div>`;
  }
  const label = { talk: "Talk", vote: "Vote", reveal: "Reveal" }[g.stage];
  fill(
    box,
    `<div class="rhead"><span class="pill">Round <b>${g.round}</b> of ${g.rounds}</span><span class="pill">${icon("clock")} ${label} <b class="cd"></b></span></div>
    <div class="bar tbar"><i data-total="${g.time}"></i></div>${body}<div class="err" id="err"></div>
    ${g.stage === "reveal" ? "" : `<h3 class="lh">Players</h3><div class="players">${g.players.map((p) => `<span class="mem">${av(p)}<span>${esc(p.name)}</span>${g.stage === "vote" && p.voted ? `<em>${icon("check")}</em>` : ""}</span>`).join("")}</div>`}
    <h3 class="lh">Points</h3><ol class="board mini">${standings(g.standings)}</ol>`,
    fresh
  );
  box.querySelectorAll(".cd").forEach((el) => (el.dataset.end = g.end));
  box.querySelector(".tbar i").dataset.end = g.end;
  const vn = document.getElementById("votenow");
  if (vn) vn.onclick = () => partyAct("act", { kind: "vote-now" });
  box.querySelectorAll("[data-v]").forEach((x) => (x.onclick = () => partyAct("act", { kind: "vote", text: x.dataset.v })));
}

// ── Draw it ──

const INK = ["#1b1b1f", "#d2492f", "#e8a33b", "#3f8f5a", "#4c8dff", "#9b6bff", "#8b5a3c", "#f4efe6"];
const PEN = [5, 12, 28];
let ink = { gen: -1, strokes: [], n: 0, pts: 0 };
let pen = { c: 0, w: 0 };
let inkBusy = false;

function inkPaint() {
  const cv = document.getElementById("cv");
  if (!cv) return;
  const x = cv.getContext("2d");
  x.fillStyle = "#f4efe6";
  x.fillRect(0, 0, cv.width, cv.height);
  x.lineCap = x.lineJoin = "round";
  for (const s of ink.strokes) inkLine(x, cv, s, 0);
}

function inkLine(x, cv, s, from) {
  const p = s.p;
  if (p.length < 2) return;
  const sx = cv.width / 1000;
  const sy = cv.height / 1000;
  x.strokeStyle = INK[s.c] || INK[0];
  x.lineWidth = s.w * sx;
  x.beginPath();
  const k = Math.max(0, from - 2);
  x.moveTo(p[k] * sx, p[k + 1] * sy);
  if (p.length === 2) x.lineTo(p[0] * sx + 0.1, p[1] * sy);
  for (let i = k + 2; i < p.length; i += 2) x.lineTo(p[i] * sx, p[i + 1] * sy);
  x.stroke();
}

// inkSync fetches the strokes the page doesn't have yet.
async function inkSync(d) {
  if (inkBusy) return;
  if (d.ink_gen === ink.gen && d.ink_n === ink.n && d.ink_pts === ink.pts) return;
  inkBusy = true;
  try {
    const fresh = d.ink_gen !== ink.gen;
    const from = fresh ? 0 : Math.max(0, ink.strokes.length - 1);
    const r = await api(`party/ink?room=${encodeURIComponent(room)}&from=${from}`);
    if (r.gen !== d.ink_gen && !fresh) return;
    ink.strokes = (fresh ? [] : ink.strokes.slice(0, r.from)).concat(r.strokes);
    ink.gen = r.gen;
    ink.n = ink.strokes.length;
    ink.pts = ink.strokes.reduce((n, s) => n + s.p.length / 2, 0);
    inkPaint();
  } catch (_) {
  } finally {
    inkBusy = false;
  }
  if (tv && tv.draw && (tv.draw.ink_gen !== ink.gen || tv.draw.ink_n !== ink.n || tv.draw.ink_pts !== ink.pts)) inkSync(tv.draw);
}

// inkPen lets the drawer draw: points go up in small batches.
function inkPen(cv) {
  let cur = null;
  let sent = 0;
  let id = Date.now() % 1e6;
  let timer = 0;
  const flush = () => {
    timer = 0;
    if (!cur || cur.p.length <= sent) return;
    const piece = { id: cur.id, c: cur.c, w: cur.w, p: cur.p.slice(sent) };
    sent = cur.p.length;
    api("party/ink", { room, strokes: [piece] })
      .then((r) => {
        ink.gen = r.ink_gen;
        ink.n = r.ink_n;
      })
      .catch(() => {});
  };
  const at = (e) => {
    const r = cv.getBoundingClientRect();
    return [Math.round(((e.clientX - r.left) / r.width) * 1000), Math.round(((e.clientY - r.top) / r.height) * 1000)];
  };
  cv.onpointerdown = (e) => {
    cv.setPointerCapture(e.pointerId);
    // The pink-pen prank: their pen is pink or a random colour, whatever they pick.
    const c = pranks.has("pink") ? (Math.random() < 0.5 ? "#ff5fb2" : `hsl(${Math.floor(Math.random() * 360)} 90% 60%)`) : pen.c;
    cur = { id: ++id, c, w: PEN[pen.w], p: at(e) };
    sent = 0;
    ink.strokes.push(cur);
    inkLine(cv.getContext("2d"), cv, cur, 0);
    if (!timer) timer = setTimeout(flush, 90);
  };
  cv.onpointermove = (e) => {
    if (!cur) return;
    const n = cur.p.length;
    cur.p.push(...at(e));
    inkLine(cv.getContext("2d"), cv, cur, n);
    if (!timer) timer = setTimeout(flush, 90);
  };
  cv.onpointerup = cv.onpointercancel = () => {
    flush();
    cur = null;
  };
}

function drawDraw(box, v, fresh) {
  const d = v.draw;
  if (fresh) {
    let top = "";
    let below = "";
    if (d.stage === "pick") {
      top = d.mine
        ? `<p class="lead">Pick what to draw</p><div class="picks">${d.choices
            .map((c, i) => `<button class="pick" data-pick="${i}" style="--c:#4fc3a1">${icon("draw")}<b>${esc(c)}</b></button>`)
            .join("")}</div>`
        : `<p class="wait">${av(d.drawer)} <b>${esc(d.drawer.name)}</b> is picking a title…</p>`;
    } else {
      if (d.stage === "draw" && d.mine) {
        top = `<p class="lead">Draw <b>${esc(d.word.name)}</b>. No letters or numbers!</p>`;
        below = `<div class="inkbar">${INK.map((c, i) => `<button class="sw ${pen.c === i ? "on" : ""}" data-c="${i}" style="--sw:${c}" aria-label="Colour"></button>`).join(
          ""
        )}<span class="gap"></span>${PEN.map((w, i) => `<button class="pw ${pen.w === i ? "on" : ""}" data-w="${i}" aria-label="Size"><i style="--pw:${4 + i * 5}px"></i></button>`).join(
          ""
        )}<button class="btn alt" id="undo" aria-label="Undo">${icon("undo")}</button><button class="btn alt" id="clear" aria-label="Clear">${icon("trash")}</button></div>`;
      } else if (d.stage === "draw") {
        top = `<p class="lead">${av(d.drawer)} <b>${esc(d.drawer.name)}</b> is drawing</p><div class="mask">${d.got ? esc(d.word.name) : maskHTML(d.mask)}</div>`;
        below = d.got
          ? `<p class="won verdict">You got it!</p>`
          : `<div class="search"><input id="dg" placeholder="Type your guess…" autocomplete="off" spellcheck="false"></div><div class="err" id="err"></div>`;
      } else {
        top = `<section class="result">${d.word ? `<img src="${cover(d.word.id)}" alt="">` : ""}<div><p class="muted" style="margin:0">${esc(d.drawer.name)} drew</p><h2>${esc(
          d.word ? d.word.name : "?"
        )}</h2><p class="note">${d.round >= d.rounds ? "Final results" : "Next round"} in <b class="cd"></b></p></div></section>`;
      }
      top += `<div class="cvwrap"><canvas id="cv" width="800" height="600"></canvas></div>`;
    }
    fill(
      box,
      `<div class="rhead"><span class="pill">Round <b>${d.round}</b> of ${d.rounds}</span>${
        d.stage === "reveal" ? "" : `<span class="pill">${icon("clock")} <b class="cd"></b></span>`
      }</div>${top}${below}
      <div class="racelow"><div><h3 class="lh">Guesses</h3><ul class="feed" id="feed"></ul></div><div><h3 class="lh">Points</h3><ol class="board mini" id="stand"></ol></div></div>`,
      fresh
    );
    if (d.stage === "pick" && fresh) ink = { gen: -1, strokes: [], n: 0, pts: 0 };
    box.querySelectorAll("[data-pick]").forEach((x) => (x.onclick = () => partyAct("act", { kind: "pick", cell: +x.dataset.pick })));
    const cv = document.getElementById("cv");
    if (cv) {
      inkPaint();
      if (d.stage === "draw" && d.mine) {
        inkPen(cv);
        box.querySelectorAll("[data-c]").forEach(
          (x) =>
            (x.onclick = () => {
              pen.c = +x.dataset.c;
              box.querySelectorAll("[data-c]").forEach((y) => y.classList.toggle("on", y === x));
            })
        );
        box.querySelectorAll("[data-w]").forEach(
          (x) =>
            (x.onclick = () => {
              pen.w = +x.dataset.w;
              box.querySelectorAll("[data-w]").forEach((y) => y.classList.toggle("on", y === x));
            })
        );
        document.getElementById("undo").onclick = () => api("party/ink", { room, undo: true }).catch(() => {});
        document.getElementById("clear").onclick = () => api("party/ink", { room, clear: true }).catch(() => {});
      }
    }
    const dg = document.getElementById("dg");
    if (dg) {
      dg.onkeydown = (e) => {
        if (e.key !== "Enter" || !dg.value.trim()) return;
        const text = dg.value.trim();
        dg.value = "";
        partyAct("act", { kind: "guess", text });
      };
      dg.focus();
    }
    if (d.got && fresh) sfx("ok");
  }
  if (!d.mine || d.ink_gen !== ink.gen) inkSync(d);
  const m = box.querySelector(".mask");
  if (m && !d.got && d.mask) m.innerHTML = maskHTML(d.mask);
  box.querySelectorAll(".cd").forEach((el) => (el.dataset.end = d.end));
  document.getElementById("feed").innerHTML =
    d.feed.map((f, i) => `<li data-i="${i}" class="${f.ok ? "ok" : ""}"><b>${esc(f.name)}</b> ${f.ok ? `${icon("check")} got it` : esc(f.text)}</li>`).join("") ||
    `<li class="none">Guesses show up here.</li>`;
  document.getElementById("stand").innerHTML = standings(d.standings);
}

const maskHTML = (mask) =>
  mask
    .split(" ")
    .map((w) => `<span>${[...w].map((c) => `<i class="${c === "_" ? "e" : ""}">${c === "_" ? "" : esc(c)}</i>`).join("")}</span>`)
    .join("");

// ── Hot potato ──

let bombTick = 0;

function drawBomb(box, v, fresh) {
  const g = v.bomb;
  if (fresh) {
    let mid;
    if (g.stage === "boom") {
      sfx("stamp");
      sfx("bad");
      mid = `<div class="boom">BOOM</div><p class="lost verdict">${esc(g.boomed)} is out!</p>`;
    } else {
      mid = `<div class="bombrow"><div class="bomb${g.mine ? " mine" : ""}">${icon("bomb")}<i class="spark"></i></div><div><small class="muted">Name a show that fits</small><h2 class="cat">${esc(
        g.category
      )}</h2><p class="holder">${av(g.holder)} ${g.mine ? "<b>You</b> have the bomb!" : `<b>${esc(g.holder.name)}</b> has the bomb`}</p></div></div>
      ${g.mine ? `${searchBox("Type a show that fits…")}` : g.in ? `<p class="note">Get ready, it comes to you next…</p>` : `<p class="note">You're out. Watch the rest.</p>`}`;
    }
    fill(
      box,
      `<div class="rhead"><span class="pill">${icon("bomb")} Hot potato</span><span class="pill">${icon("party")} ${g.players.filter((p) => !p.out).length} in</span></div>
      ${mid}<div class="err" id="err"></div>
      <h3 class="lh">Said</h3><div class="gws" id="said"></div>
      <div class="racelow"><div><h3 class="lh">Tries</h3><ul class="feed" id="feed"></ul></div><div><h3 class="lh">Players</h3><div class="players" id="bp"></div></div></div>`,
      fresh
    );
    if (g.mine) {
      const send = (body) =>
        partyAct("act", { kind: "answer", ...body }).then(() => {
          const q = document.getElementById("q");
          if (q) q.value = "";
        });
      search(new Set(), (t) => send({ cell: t.id, text: t.name }), (text) => send({ text }));
    }
    clearInterval(bombTick);
    if (g.mine) bombTick = setInterval(() => (where === "party" && tv && tv.bomb && tv.bomb.mine ? sfx("tick") : clearInterval(bombTick)), 700);
  }
  document.getElementById("said").innerHTML = g.said.map((x) => `<span class="gw ok">${esc(x)}</span>`).join("") || `<span class="note">Nothing yet.</span>`;
  document.getElementById("feed").innerHTML =
    g.feed
      .map((f, i) => `<li data-i="${i}" class="${f.ok ? "ok" : ""}"><b>${esc(f.name)}</b> ${f.ok ? `${icon("check")} ` : ""}${esc(f.text)}</li>`)
      .reverse()
      .join("") || `<li class="none">Answers show up here.</li>`;
  document.getElementById("bp").innerHTML = g.players
    .map((p) => `<span class="mem ${p.out ? "out" : ""} ${p.host ? "hot" : ""}">${av(p)}<span>${esc(p.name)}</span>${p.host ? `<em>${icon("bomb")}</em>` : ""}</span>`)
    .join("");
}

// ── Guess who ──

let whoGuessing = false;

const whoFace = (c) =>
  c.face ? `<img src="api/face/${c.show_id}/${c.k}" alt="" loading="lazy" onerror="this.remove()">` : "";

function whoCardHTML(c, i, cls) {
  return `<button class="wc ${cls}" data-k="${i}" style="--h:${hue(c.name)}"><span class="wf">${esc(initials(c.name))}${whoFace(c)}</span><b>${esc(c.name)}</b><small>${esc(
    c.show
  )}</small></button>`;
}

function drawWho(box, v, fresh) {
  const g = v.who;
  const opp = g.players.find((p) => p.id !== v.me) || g.players[1];
  const turnP = g.players.find((p) => p.id === g.turn) || {};
  const mine = g.in && g.turn === v.me;
  const down = new Set(g.down);
  let head;
  if (!g.in) head = `<p class="note">Watching ${esc(g.players[0].name)} vs ${esc(g.players[1].name)}. ${esc(turnP.name || "")}'s turn.</p>`;
  else {
    const sc = g.cards[g.secret];
    head = `<div class="whohead"><div class="wsecret">${whoCardHTML(sc, g.secret, "me")}<small>Your character</small></div><div>${
      mine
        ? `<p class="lead"><b>Your turn.</b> Ask ${esc(opp.name)} a yes or no question in the call, then flip down everyone it rules out.</p>
          <div class="row"><button class="btn alt" id="end">End turn</button><button class="btn ${whoGuessing ? "" : "alt"}" id="guess">${whoGuessing ? "Tap your guess…" : "Make a guess"}</button></div>`
        : `<p class="lead"><b>${esc(turnP.name)}'s turn.</b> Answer their question in the call. You can still flip your cards.</p>`
    }<p class="note">A wrong guess loses the game. ${esc(opp.name)} has ${opp.points || 0} cards left up.</p></div></div>`;
  }
  fill(
    box,
    `<div class="rhead"><span class="pill">${icon("who")} Guess who</span><span class="pill">${g.players.map((p) => `${av(p)} ${p.points || 0}`).join(" · ")}</span></div>
    ${head}<div class="err" id="err"></div><div class="wgrid${whoGuessing && mine ? " guessing" : ""}">${g.cards
      .map((c, i) => whoCardHTML(c, i, down.has(i) ? "down" : ""))
      .join("")}</div>`,
    fresh
  );
  if (!g.in) return;
  if (!mine) whoGuessing = false;
  box.querySelectorAll(".wgrid [data-k]").forEach(
    (x) =>
      (x.onclick = () => {
        const k = +x.dataset.k;
        if (whoGuessing && mine) {
          whoGuessing = false;
          return partyAct("act", { kind: "guess", cell: k });
        }
        partyAct("act", { kind: "flip", cell: k });
      })
  );
  const end = document.getElementById("end");
  if (end) end.onclick = () => ((whoGuessing = false), partyAct("act", { kind: "end", cell: 0 }));
  const gs = document.getElementById("guess");
  if (gs)
    gs.onclick = () => {
      whoGuessing = !whoGuessing;
      drawWho(box, tv, false);
    };
}

// whoEnd is guess who's result: both characters.
function whoEnd(g) {
  return `${g.missed ? `<p class="note">Lost on a wrong guess.</p>` : ""}<div class="impvs">${g.players
    .map((p) => {
      const c = g.cards[g.secrets[p.id]];
      return `<div class="wsecret">${whoCardHTML(c, 0, "")}<small>${esc(p.name)}'s character</small></div>`;
    })
    .join("")}</div>`;
}

// ── Koi-Koi ──
//
// Hanafuda with anime covers: each month is one show, so matching means the
// same cover. Tap a card in your hand to see what it takes, then tap it
// again to play it, or tap the field card to take when two match.

const KK_KINDS = ["BRPP", "ARPP", "BRPP", "ARPP", "ARPP", "ARPP", "ARPP", "BAPP", "ARPP", "ARPP", "BARP", "BPPP"];
const KK_MONTH = "松梅桜藤菖牡萩芒菊紅柳桐";
const KK_FLOWER = ["Pine", "Plum", "Cherry", "Wisteria", "Iris", "Peony", "Clover", "Pampas", "Chrysanthemum", "Maple", "Willow", "Paulownia"];
const KK_POEM = [1, 5, 9];
const KK_BLUE = [21, 33, 37];
const KK_NAMES = { B: "Brights", A: "Animals", R: "Ribbons", P: "Plains" };
const kkKind = (c) => KK_KINDS[c >> 2][c & 3];
let kkSel = -1;

// kkSeats says how a call would sit at tables of n.
function kkSeats(people, n) {
  const tables = Math.ceil(people / n);
  const bots = tables * n - people;
  const t = tables === 1 ? "One table" : `${tables} tables`;
  return bots ? `${t}, ${bots} ${bots === 1 ? "bot" : "bots"}` : people === n ? "You all at one table" : `${t}, no bots`;
}

// kkOpen is the menu card: the room's lobby, at Koi-Koi.
function kkOpen() {
  partyView();
  const go = () => {
    const el = document.getElementById("kksec");
    if (el) el.scrollIntoView({ block: "start" });
    else if (where === "party" && !tv) setTimeout(go, 300);
  };
  setTimeout(go, 50);
}

function kkLabel(c) {
  if (c === 32) return "盃"; // the sake cup
  if (c === 40) return "雨"; // the rain man
  const k = kkKind(c);
  if (k === "B") return "光";
  if (k === "A") return "種";
  if (k === "R") return KK_POEM.includes(c) ? "赤" : KK_BLUE.includes(c) ? "青" : "短";
  return "";
}

function kkCard(g, c, cls = "", attrs = "") {
  const s = (g && g.shows[c >> 2]) || {};
  const k = kkKind(c);
  const rib = k === "R" ? (KK_BLUE.includes(c) ? " blue" : " red") : "";
  const lab = kkLabel(c);
  return `<button class="hf k${k}${rib} ${cls}" ${attrs} style="--h:${(c >> 2) * 30}" title="${esc(`${KK_FLOWER[c >> 2]} · ${s.name || ""}`)}">${
    s.id ? `<img src="${cover(s.id)}" alt="" loading="lazy" onerror="this.remove()">` : ""
  }${rib ? `<i class="rib"></i>` : ""}<span class="mo">${KK_MONTH[c >> 2]}</span>${
    lab ? `<span class="kd">${lab}</span>` : ""
  }</button>`;
}

// kkSeen is whether this device has seen the guide; the first game opens it.
function kkSeen() {
  try {
    if (localStorage.getItem("otakase-kk-guide")) return true;
    localStorage.setItem("otakase-kk-guide", "1");
  } catch (_) {}
  return false;
}

// kkGuide explains the game in a sheet over the page.
function kkGuide() {
  if (document.querySelector(".modal")) return;
  const g = tv && tv.kk;
  const c = (cs) => `<div class="kex">${cs.map((x) => kkCard(g, x)).join("")}</div>`;
  const row = (name, pts, how) => `<tr><td><b>${name}</b><small>${how}</small></td><td>${pts}</td></tr>`;
  const m = document.createElement("div");
  m.className = "modal";
  m.innerHTML = `<div class="sheet" role="dialog" aria-label="How to play Koi-Koi">
    <button class="pill close" id="kkclose">Close</button>
    <h2>How to play <span>Koi-Koi</span></h2>
    <p>Koi-Koi is played with hanafuda, Japanese flower cards. Collect cards to make combos, then decide: score now, or push your luck for more.</p>
    <h3 class="lh">The cards</h3>
    <p>48 cards, 12 months, 4 cards each. Here every month is one anime, so <b>the same cover means the same month</b>. The sign in the corner is the month.</p>
    <div class="kmonths">${[...KK_MONTH].map((k, i) => `<span><b>${k}</b>${KK_FLOWER[i]}</span>`).join("")}</div>
    <p>Some cards are worth more. The badge says which kind:</p>
    <div class="kkinds">
      <div>${c([0, 8])}<p><b class="gold">光 Brights</b> (5): the best cards, gold frame. 雨 the rain man is the weak one.</p></div>
      <div>${c([24, 32])}<p><b class="tealt">種 Animals</b> (9). 盃 the sake cup also counts as a plain.</p></div>
      <div>${c([1, 21, 13])}<p><b class="redt">短 Ribbons</b> (10): 赤 red poem, 青 blue, or plain red.</p></div>
      <div>${c([2, 3])}<p><b>Plains</b> (24): dimmed, worth little alone.</p></div>
    </div>
    <h3 class="lh">Your turn</h3>
    <ol class="steps">
      <li><b>Play a card</b> from your hand onto a field card with the same cover. You take both. If two match, tap the one you want. If three match, you take all four. No match: your card stays on the field.</li>
      <li><b>The deck flips</b> its top card for you, and it does the same: it takes a match, or stays on the field.</li>
      <li>Your taken cards build <b>combos</b>.</li>
    </ol>
    <h3 class="lh">Combos</h3>
    <table class="kyaku">
      ${row("Five brights", 10, "All 5 光")}
      ${row("Four brights", 8, "4 光 without 雨")}
      ${row("Rainy four brights", 7, "4 光 with 雨")}
      ${row("Three brights", 5, "3 光 without 雨")}
      ${row("Boar, deer, butterfly", 5, "The 種 of 萩, 紅 and 牡")}
      ${row("Red poem ribbons", 5, "All three 赤")}
      ${row("Blue ribbons", 5, "All three 青")}
      ${row("Red and blue ribbons", 10, "All six")}
      ${row("Flower viewing", 5, "桜 光 and 盃")}
      ${row("Moon viewing", 5, "芒 光 and 盃")}
      ${row("Animals", "1+", "5 種, +1 each more")}
      ${row("Ribbons", "1+", "5 短, +1 each more")}
      ${row("Plains", "1+", "10 plains, +1 each more")}
    </table>
    <h3 class="lh">Koi-koi or stop</h3>
    <p>When you make a new combo, or a better one, you choose:</p>
    <ul class="steps">
      <li><b>Stop</b>: score your combos now. The hand ends.</li>
      <li><b>Koi-koi!</b>: play on to grow your score. But if anyone else stops first, you get nothing.</li>
    </ul>
    <p>7 points or more is doubled. Stopping while someone else at your table called koi-koi doubles it again. If every hand runs out and nobody stopped, nobody scores.</p>
    <h3 class="lh">The game</h3>
    <p>3 hands, points add up. A big call plays at several tables at once and the best total across all tables wins. Bots fill empty seats. You have 45 seconds a turn, then a bot plays it for you.</p>
    <button class="btn" id="kkok">Got it</button>
  </div>`;
  document.body.appendChild(m);
  const close = () => m.remove();
  m.onclick = (e) => e.target === m && close();
  document.getElementById("kkclose").onclick = close;
  document.getElementById("kkok").onclick = close;
}

// kkPile is what a seat has taken, by kind.
function kkPile(g, got, big) {
  return `<div class="kpile${big ? " big" : ""}">${["B", "A", "R", "P"]
    .map((k) => {
      const cs = got.filter((c) => kkKind(c) === k);
      if (!cs.length) return "";
      return `<div class="kgrp" title="${KK_NAMES[k]}">${big ? cs.map((c) => kkCard(g, c, "mini")).join("") : kkCard(g, cs[0], "mini")}<b>${cs.length}</b></div>`;
    })
    .join("")}</div>`;
}

const kkYaku = (ys) => ys.map((y) => `<span class="ky">${esc(y.name)} <b>${y.points}</b></span>`).join("");

// kkPoints is what stopping now would score.
function kkPoints(g, seat) {
  let pts = g.seats[seat].yaku.reduce((n, y) => n + y.points, 0);
  if (pts >= 7) pts *= 2;
  if (g.seats.some((s, i) => i !== seat && s.koi)) pts *= 2;
  return pts;
}

function drawKK(box, v, fresh) {
  const g = v.kk;
  if (fresh) kkSel = -1;
  const meSeat = g.me >= 0 ? g.seats[g.me] : null;
  const mine = g.me >= 0 && g.turn === g.me && !meSeat.bot;
  const turnS = g.seats[g.turn] || {};
  const playing = mine && g.stage === "play";
  if (!playing || !g.mine.includes(kkSel)) kkSel = -1;
  const match = kkSel < 0 ? [] : g.field.map((f, i) => ((f >> 2) === (kkSel >> 2) ? i : -1)).filter((i) => i >= 0);
  const last = new Set(g.last);

  let status;
  if (g.stage === "done") {
    const r = g.result;
    const who = r.seat >= 0 ? g.seats[r.seat] : null;
    const notes = [r.big ? "doubled for 7 or more" : "", r.koi ? "doubled by a koi-koi" : ""].filter(Boolean).join(", ");
    status = `<section class="kres">${
      who
        ? `<p class="lead">${r.seat === g.me ? "<b>You</b> stopped" : `<b>${esc(who.name)}</b> stopped`} and scored <b class="gold">${r.points}</b>${
            notes ? ` <span class="muted">(${notes})</span>` : ""
          }</p><div class="kys">${kkYaku(r.yaku)}</div>`
        : `<p class="lead">${esc(r.through || "Nobody scored this hand")}</p>`
    }<p class="note">${g.hand >= g.hands ? "Final scores" : "Next hand"} in <b class="cd"></b></p></section>`;
  } else if (g.stage === "finished") {
    status = `<p class="lead">This table is done. Waiting for the other tables…</p>`;
  } else if (mine && g.stage === "decide") {
    const pts = kkPoints(g, g.me);
    status = `<section class="kdecide"><p class="lead"><b>You made a combo!</b> Stop and score it, or call koi-koi and play on for more. If someone else stops first, you get nothing.</p>
      <div class="kys">${kkYaku(meSeat.yaku)}</div>
      <div class="row"><button class="btn" id="kstop">Stop, score ${pts}</button><button class="btn alt" id="kkoi">Koi-Koi!</button></div></section>`;
  } else if (playing) {
    const tip =
      kkSel < 0
        ? "Your turn. Tap a card in your hand."
        : match.length === 0
          ? "Nothing matches it. Tap it again to lay it on the field."
          : match.length === 2
            ? "Two match. Tap the one you want to take."
            : "Tap it again to play it.";
    status = `<p class="lead"><b>${tip}</b></p>`;
  } else {
    status = `<p class="lead">${turnS.bot ? "" : av(turnS)} <b>${esc(turnS.name || "")}</b> ${g.stage === "decide" ? "is deciding: stop or koi-koi?" : "is playing…"}</p>`;
  }

  const seat = (s, i, pile = true) =>
    `<div class="kseat${i === g.turn && g.stage !== "done" ? " turn" : ""}${s.koi ? " koi" : ""}">${s.bot ? `<i class="av bot">${icon("koikoi")}</i>` : av(s)}<div class="kn"><b>${esc(
      s.name
    )}${i === g.me ? " (you)" : ""}</b><small>${s.total} pts · ${s.hand} in hand${s.koi ? ` · <em>koi-koi!</em>` : ""}</small><div class="kys">${kkYaku(s.yaku)}</div></div>${pile ? kkPile(g, s.got, false) : ""}</div>`;
  const others = g.seats.map((s, i) => (i === g.me ? "" : seat(s, i))).join("");
  const tables =
    g.tables.length > 1
      ? `<div class="ktables">${g.tables
          .map(
            (t, i) =>
              `<button class="pill${i === g.table ? " on" : ""}" data-t="${i}" ${g.me >= 0 ? "disabled" : ""}>Table ${i + 1} · ${
                t.stage === "finished" ? "done" : `hand ${t.hand}`
              }</button>`
          )
          .join("")}</div>`
      : "";
  fill(
    box,
    `<div class="rhead"><span class="pill">${icon("koikoi")} Koi-Koi · hand <b>${g.hand}</b> of ${g.hands}</span><button class="pill kq" id="kkhelp" title="How to play">?</button>${
      mine && (g.stage === "play" || g.stage === "decide") ? `<span class="pill">${icon("clock")} <b class="cd"></b></span>` : ""
    }</div>
    ${tables}${g.me < 0 ? `<p class="note">Watching table ${g.table + 1}.</p>` : ""}
    <div class="kseats">${others}</div>
    <div class="kfield"><div class="kdeck"><i></i><b>${g.deck}</b></div><div class="kcards">${g.field
      .map((c, i) => kkCard(g, c, `${match.includes(i) ? "match" : ""} ${last.has(c) ? "last" : ""}`, `data-f="${i}"`))
      .join("")}</div></div>
    <div class="klog">${status}${g.log && g.stage !== "done" ? `<p class="note">${esc(g.log)}</p>` : ""}</div><div class="err" id="err"></div>
    ${
      meSeat
        ? `<div class="kme">${seat(meSeat, g.me, false)}${kkPile(g, meSeat.got, true)}</div>
      <div class="khand${playing ? " live" : ""}">${g.mine.map((c) => kkCard(g, c, `${c === kkSel ? "sel" : ""}`, `data-h="${c}"`)).join("")}</div>`
        : ""
    }
    <h3 class="lh">Scores</h3><ol class="board mini">${standings(g.standings)}</ol>`,
    fresh
  );
  box.querySelectorAll(".cd").forEach((el) => (el.dataset.end = g.end));
  document.getElementById("kkhelp").onclick = kkGuide;
  if (fresh && g.me >= 0 && g.hand === 1 && !kkSeen()) kkGuide();
  if (fresh && g.stage === "done" && g.result && g.result.seat === g.me && g.me >= 0) sfx("ok");
  const play = (card, pick) => {
    kkSel = -1;
    sfx("tap");
    partyAct("act", { kind: "play", cell: card, pick });
  };
  if (playing) {
    box.querySelectorAll("[data-h]").forEach(
      (el) =>
        (el.onclick = () => {
          const c = +el.dataset.h;
          if (c !== kkSel) {
            kkSel = c;
            sfx("tap");
            return drawKK(box, tv, false);
          }
          if (match.length !== 2) play(c, match.length ? match[0] : -1);
        })
    );
    box.querySelectorAll("[data-f]").forEach((el) => {
      const i = +el.dataset.f;
      if (match.includes(i)) el.onclick = () => play(kkSel, i);
    });
  }
  const st = document.getElementById("kstop");
  if (st) st.onclick = () => partyAct("act", { kind: "stop", cell: 0 });
  const ko = document.getElementById("kkoi");
  if (ko) ko.onclick = () => (sfx("stamp"), partyAct("act", { kind: "koi", cell: 0 }));
  box.querySelectorAll("[data-t]").forEach((el) => (el.onclick = () => partyAct("act", { kind: "watch", cell: +el.dataset.t })));
}

// ── Sound ──
//
// Effects and music are made here with Web Audio: no sound files. The
// speaker button cycles off, effects, effects + music; effects are on by
// default and music is off, since most people are talking in a call. The
// choice is kept per device.

const SOUND_MODES = ["off", "fx", "music"];
const SOUND_TEXT = { off: "Sound off", fx: "Sound effects", music: "Effects + music" };
let soundMode = "fx";
try {
  soundMode = SOUND_MODES.includes(localStorage.getItem("otakase-sound")) ? localStorage.getItem("otakase-sound") : "fx";
} catch (_) {}
let actx = null;
let fxBus = null;
let musicBus = null;

// audio starts the audio context; browsers only allow it after a tap.
function audio() {
  if (actx) return actx;
  const AC = window.AudioContext || window.webkitAudioContext;
  if (!AC) return null;
  actx = new AC();
  fxBus = actx.createGain();
  fxBus.gain.value = 0.35;
  fxBus.connect(actx.destination);
  musicBus = actx.createGain();
  musicBus.gain.value = 0;
  musicBus.connect(actx.destination);
  return actx;
}

// tone plays one note: type, frequency (or [from, to]), start offset,
// length and loudness.
function tone(type, freq, at, len, vol, bus = fxBus) {
  const t = actx.currentTime + at;
  const o = actx.createOscillator();
  const g = actx.createGain();
  o.type = type;
  if (Array.isArray(freq)) {
    o.frequency.setValueAtTime(freq[0], t);
    o.frequency.exponentialRampToValueAtTime(freq[1], t + len);
  } else o.frequency.value = freq;
  g.gain.setValueAtTime(0.0001, t);
  g.gain.exponentialRampToValueAtTime(vol, t + 0.008);
  g.gain.exponentialRampToValueAtTime(0.0001, t + len);
  o.connect(g).connect(bus);
  o.start(t);
  o.stop(t + len + 0.05);
}

// noise is a short filtered hiss, for the stamp's thud.
function noise(at, len, vol, cutoff) {
  const t = actx.currentTime + at;
  const buf = actx.createBuffer(1, Math.ceil(actx.sampleRate * len), actx.sampleRate);
  const d = buf.getChannelData(0);
  for (let i = 0; i < d.length; i++) d[i] = (Math.random() * 2 - 1) * (1 - i / d.length);
  const src = actx.createBufferSource();
  src.buffer = buf;
  const f = actx.createBiquadFilter();
  f.type = "lowpass";
  f.frequency.value = cutoff;
  const g = actx.createGain();
  g.gain.value = vol;
  src.connect(f).connect(g).connect(fxBus);
  src.start(t);
}

const SFX = {
  tap: () => tone("triangle", 660, 0, 0.05, 0.12),
  ok: () => (tone("sine", 880, 0, 0.18, 0.4), tone("sine", 1318.5, 0.09, 0.3, 0.35)),
  bad: () => (tone("triangle", [220, 150], 0, 0.22, 0.35), tone("sine", 110, 0, 0.2, 0.25)),
  stamp: () => (noise(0, 0.12, 0.9, 900), tone("sine", [120, 60], 0, 0.18, 0.6)),
  tick: () => tone("square", 1760, 0, 0.03, 0.06),
  win: () => [587.3, 698.5, 784, 880, 1174.7].forEach((f, k) => tone("triangle", f, k * 0.07, 0.35, 0.25)),
  // An air horn: three blasts of a buzzy chord.
  horn: () => [0, 0.32, 0.64].forEach((at, k) => [466, 587, 698].forEach((f) => tone("sawtooth", f, at, k === 2 ? 0.7 : 0.26, 0.16))),
  boom: () => (noise(0, 0.9, 1.4, 300), tone("sine", [90, 30], 0, 0.8, 0.9)),
  // A rubber duck: a nasal falling squawk, twice.
  quack: () => [0, 0.16].forEach((at) => (tone("sawtooth", [620, 380], at, 0.13, 0.22), tone("square", [1240, 760], at, 0.1, 0.06))),
  // A crowd: low wobbly "boo"s, or many short claps.
  boo: () => [130, 146, 110, 164].forEach((f, k) => tone("sawtooth", [f * 1.2, f], k * 0.05, 1.1, 0.07)),
  clap: () => Array.from({ length: 22 }, (_, k) => noise(k * 0.07 + Math.random() * 0.04, 0.05, 0.5, 1800)),
  ghost: () => tone("sine", [520, 260], 0, 0.6, 0.18),
  // Dial-up-ish beeps for the fake disconnect.
  drop: () => [0, 0.18, 0.36].forEach((at, k) => tone("square", 440 - k * 110, at, 0.12, 0.12)),
};

function sfx(name) {
  if (soundMode === "off" || !audio() || actx.state !== "running") return;
  try {
    SFX[name]();
  } catch (_) {}
}

// sfxForce plays an admin's prank sound even with effects off, as long as
// the browser allows sound at all.
function sfxForce(name) {
  if (!audio() || actx.state !== "running") return;
  try {
    SFX[name]();
  } catch (_) {}
}

// ── Music: a dark lo-fi loop ──
//
// 16 bars at 58 BPM in D Phrygian (Dm, Ebmaj7, Dm(add9), C#dim7): warbly
// detuned electric piano chords over a low drone, a deep round bass, soft
// swung drums with brushes and a rim click, and worn vinyl crackle, all
// muffled and in a large room. A koto melody comes in after 4 bars and sparse
// plucks close the loop. Notes are scheduled a little ahead.

const BEAT = 60 / 58;
const EIGHTH = BEAT / 2;
const SWING = 0.13; // late off-beats, of a beat
const CHORDS = [
  { bass: 38, keys: [50, 53, 57, 62] }, // Dm
  { bass: 39, keys: [51, 55, 58, 62] }, // Ebmaj7
  { bass: 38, keys: [50, 53, 57, 64] }, // Dm(add9)
  { bass: 37, keys: [49, 52, 55, 58] }, // C#dim7
];
// The melody: per bar, [eighth, note, length in eighths].
const MELODY = [
  [[0, 69, 2], [3, 70, 1], [4, 69, 3]],
  [[1, 63, 1], [2, 62, 2], [4, 63, 4]],
  [[0, 65, 2], [2, 63, 2], [4, 62, 3], [7, 61, 1]],
  [[0, 62, 6]],
];
const KICK = [1, 0, 0, 0, 0, 1, 0, 0];
const SNARE = [0, 0, 1, 0, 0, 0, 1, 0];
const hz = (m) => 440 * Math.pow(2, (m - 69) / 12);

let musicTimer = 0;
let musicAt = 0;
let musicStep = 0;
let echo = null;
let mBus = null;
let crackle = null;
let drone = null;

function pluck(freq, at, vol) {
  const o = actx.createOscillator();
  const o2 = actx.createOscillator();
  const f = actx.createBiquadFilter();
  const g = actx.createGain();
  const g2 = actx.createGain();
  o.type = "triangle";
  o2.type = "sawtooth";
  o.frequency.value = freq;
  o2.frequency.value = freq;
  o2.detune.value = 4;
  f.type = "lowpass";
  f.frequency.setValueAtTime(freq * 8, at);
  f.frequency.exponentialRampToValueAtTime(freq * 1.4, at + 0.7);
  g.gain.setValueAtTime(0.0001, at);
  g.gain.exponentialRampToValueAtTime(vol, at + 0.006);
  g.gain.exponentialRampToValueAtTime(0.0001, at + 1.6);
  g2.gain.value = 0.25;
  o.connect(f);
  o2.connect(g2).connect(f);
  f.connect(g);
  g.connect(mBus);
  g.connect(echo);
  o.start(at);
  o2.start(at);
  o.stop(at + 1.7);
  o2.stop(at + 1.7);
}

// keys is one electric piano note: two soft sines and a bell on top.
function keys(freq, at, len, vol) {
  const g = actx.createGain();
  g.gain.setValueAtTime(0.0001, at);
  g.gain.exponentialRampToValueAtTime(vol, at + 0.02);
  g.gain.exponentialRampToValueAtTime(vol * 0.35, at + 0.5);
  g.gain.exponentialRampToValueAtTime(0.0001, at + len);
  g.connect(mBus);
  [
    [1, 0, 1],
    [1, 18, 0.6],
    [4, 0, 0.08],
  ].forEach(([m, det, v]) => {
    const o = actx.createOscillator();
    const ov = actx.createGain();
    o.type = "sine";
    o.frequency.value = freq * m;
    o.detune.value = det;
    ov.gain.value = v;
    o.connect(ov).connect(g);
    o.start(at);
    o.stop(at + len + 0.05);
  });
}

function bass(freq, at, len) {
  const o = actx.createOscillator();
  const f = actx.createBiquadFilter();
  const g = actx.createGain();
  o.type = "triangle";
  o.frequency.value = freq;
  f.type = "lowpass";
  f.frequency.value = 300;
  g.gain.setValueAtTime(0.0001, at);
  g.gain.exponentialRampToValueAtTime(0.4, at + 0.02);
  g.gain.exponentialRampToValueAtTime(0.0001, at + len);
  o.connect(f).connect(g).connect(mBus);
  o.start(at);
  o.stop(at + len + 0.05);
}

// hiss is filtered noise: snare, hats.
function hiss(at, len, vol, type, cutoff) {
  const buf = actx.createBuffer(1, Math.ceil(actx.sampleRate * len), actx.sampleRate);
  const d = buf.getChannelData(0);
  for (let i = 0; i < d.length; i++) d[i] = (Math.random() * 2 - 1) * Math.pow(1 - i / d.length, 3);
  const src = actx.createBufferSource();
  const f = actx.createBiquadFilter();
  const g = actx.createGain();
  src.buffer = buf;
  f.type = type;
  f.frequency.value = cutoff;
  g.gain.value = vol;
  src.connect(f).connect(g).connect(mBus);
  src.start(at);
}

function kick(at) {
  const o = actx.createOscillator();
  const g = actx.createGain();
  o.frequency.setValueAtTime(110, at);
  o.frequency.exponentialRampToValueAtTime(42, at + 0.12);
  g.gain.setValueAtTime(0.55, at);
  g.gain.exponentialRampToValueAtTime(0.0001, at + 0.35);
  o.connect(g).connect(mBus);
  o.start(at);
  o.stop(at + 0.4);
}

// rim is a soft dry click on the back beat, in place of a noisy snare.
function rim(at) {
  [
    [1700, 0.08],
    [520, 0.13],
  ].forEach(([freq, vol]) => {
    const o = actx.createOscillator();
    const g = actx.createGain();
    o.type = "triangle";
    o.frequency.value = freq;
    g.gain.setValueAtTime(0.0001, at);
    g.gain.exponentialRampToValueAtTime(vol, at + 0.001);
    g.gain.exponentialRampToValueAtTime(0.0001, at + 0.05);
    o.connect(g).connect(mBus);
    o.start(at);
    o.stop(at + 0.06);
  });
}

// hum is a low drone on D under the loop: detuned saws, filtered dark.
function hum() {
  const f = actx.createBiquadFilter();
  const g = actx.createGain();
  f.type = "lowpass";
  f.frequency.value = 220;
  g.gain.value = 0.05;
  f.connect(g).connect(mBus);
  const oscs = [];
  [26, 38, 45].forEach((m) =>
    [-8, 8].forEach((det) => {
      const o = actx.createOscillator();
      o.type = "sawtooth";
      o.frequency.value = hz(m);
      o.detune.value = det;
      o.connect(f);
      o.start();
      oscs.push(o);
    }),
  );
  return oscs;
}

// vinyl is a quiet bed of crackle under the loop.
function vinyl() {
  const len = 4;
  const buf = actx.createBuffer(1, actx.sampleRate * len, actx.sampleRate);
  const d = buf.getChannelData(0);
  for (let i = 0; i < d.length; i++) d[i] = (Math.random() * 2 - 1) * 0.02 + (Math.random() < 0.0004 ? (Math.random() - 0.5) * 0.9 : 0);
  const src = actx.createBufferSource();
  const f = actx.createBiquadFilter();
  const g = actx.createGain();
  src.buffer = buf;
  src.loop = true;
  f.type = "bandpass";
  f.frequency.value = 2500;
  f.Q.value = 0.6;
  g.gain.value = 0.18;
  src.connect(f).connect(g).connect(mBus);
  src.start();
  return src;
}

// musicPlan schedules the eighths that start in the next 0.6 seconds.
function musicPlan() {
  while (musicAt < actx.currentTime + 0.6) {
    const bar = Math.floor(musicStep / 8) % 16;
    const e = musicStep % 8;
    const at = musicAt + (e % 2 ? SWING * BEAT : 0);
    const c = CHORDS[bar % 4];
    if (e === 0) {
      c.keys.forEach((m, k) => keys(hz(m), at + k * 0.03, EIGHTH * 8, 0.07));
      bass(hz(c.bass - 12), at, EIGHTH * 3);
    }
    if (e === 5) bass(hz(c.bass - 12), at, EIGHTH * 2);
    if (KICK[e] && bar !== 15) kick(at);
    if (SNARE[e]) rim(at);
    hiss(at, 0.12, e % 2 ? 0.05 : 0.09, "bandpass", 3500); // brush
    if (bar >= 4 && bar < 12) {
      for (const [when, note] of MELODY[bar % 4]) if (when === e) pluck(hz(note), at + (Math.random() - 0.5) * 0.015, 0.11);
    } else if (bar >= 12 && e % 3 === 0 && Math.random() < 0.3) {
      pluck(hz(c.keys[Math.floor(Math.random() * 4)] + 12), at, 0.06);
    }
    musicAt += EIGHTH;
    musicStep++;
  }
}

function musicStart() {
  if (musicTimer || !audio() || actx.state !== "running") return;
  if (!mBus) {
    // A warm low-pass and a gentle compressor glue the loop together.
    const lp = actx.createBiquadFilter();
    lp.type = "lowpass";
    lp.frequency.value = 1900;
    const comp = actx.createDynamicsCompressor();
    comp.threshold.value = -20;
    comp.ratio.value = 3;
    mBus = actx.createGain();
    mBus.connect(lp).connect(comp).connect(musicBus);
    echo = actx.createDelay(1);
    echo.delayTime.value = BEAT * 0.68;
    const fb = actx.createGain();
    fb.gain.value = 0.3;
    const wet = actx.createGain();
    wet.gain.value = 0.3;
    echo.connect(fb).connect(echo);
    echo.connect(wet).connect(mBus);
    // A long, dark room: decaying noise as the impulse response.
    const ir = actx.createBuffer(2, actx.sampleRate * 3.5, actx.sampleRate);
    for (let c = 0; c < 2; c++) {
      const d = ir.getChannelData(c);
      for (let i = 0; i < d.length; i++) d[i] = (Math.random() * 2 - 1) * Math.pow(1 - i / d.length, 2.2);
    }
    const room = actx.createConvolver();
    room.buffer = ir;
    const roomWet = actx.createGain();
    roomWet.gain.value = 0.35;
    mBus.connect(room).connect(roomWet).connect(lp);
  }
  if (!crackle) crackle = vinyl();
  if (!drone) drone = hum();
  musicBus.gain.cancelScheduledValues(actx.currentTime);
  musicBus.gain.setTargetAtTime(0.55, actx.currentTime, 1.2);
  musicAt = actx.currentTime + 0.1;
  musicStep = 0;
  musicPlan();
  musicTimer = setInterval(musicPlan, 200);
}

function musicStop() {
  if (!musicTimer) return;
  clearInterval(musicTimer);
  musicTimer = 0;
  if (crackle) crackle.stop(actx.currentTime + 0.6);
  crackle = null;
  if (drone) drone.forEach((o) => o.stop(actx.currentTime + 0.6));
  drone = null;
  musicBus.gain.cancelScheduledValues(actx.currentTime);
  musicBus.gain.setTargetAtTime(0, actx.currentTime, 0.3);
}

function soundApply() {
  // Keep the user's music preference, but mute it on Watch Together pages.
  const playMusic = soundMode === "music" && !document.hidden && where !== "watch";
  if (!playMusic) {
    musicStop();
  } else if (actx && actx.state !== "running") {
    // Video playback or Discord can suspend Web Audio while watching.
    // Resume the existing context after returning to Games, then restore
    // music only if the user is still on a non-watch page.
    actx.resume().then(() => {
      if (soundMode === "music" && !document.hidden && where !== "watch") musicStart();
    }).catch(() => {});
  } else {
    musicStart();
  }
  const b = document.getElementById("snd");
  if (b) {
    b.innerHTML = icon(soundMode === "off" ? "mute" : soundMode === "music" ? "music" : "sound");
    b.title = b.ariaLabel = SOUND_TEXT[soundMode] + " (tap to change)";
  }
}

(function soundButton() {
  const b = document.createElement("button");
  b.id = "snd";
  b.className = "snd";
  document.getElementById("activitytools").appendChild(b);
  b.onclick = (e) => {
    e.stopPropagation();
    soundMode = SOUND_MODES[(SOUND_MODES.indexOf(soundMode) + 1) % SOUND_MODES.length];
    try {
      localStorage.setItem("otakase-sound", soundMode);
    } catch (_) {}
    if (audio() && actx.state !== "running") actx.resume();
    soundApply();
    const tip = document.createElement("span");
    tip.className = "sndtip";
    tip.textContent = SOUND_TEXT[soundMode];
    document.getElementById("activitytools").appendChild(tip);
    setTimeout(() => tip.remove(), 1400);
    sfx("tap");
  };
  soundApply();
})();

// Account-wide Watch Together defaults. Manual player selections override
// these settings until Automatic is selected again.
(function preferencesButton() {
  const button = document.createElement("button");
  button.type = "button";
  button.id = "prefbtn";
  button.className = "prefbtn";
  button.innerHTML = icon("settings");
  button.title = "Watch Together preferences";
  button.setAttribute("aria-label", "Watch Together preferences");
  button.setAttribute("aria-expanded", "false");
  document.getElementById("activitytools").appendChild(button);

  const languages = [
    ["auto", "Use stream default"], ["ja", "Japanese"], ["en", "English"],
    ["de", "German"], ["es", "Spanish"], ["fr", "French"],
    ["hi", "Hindi"], ["it", "Italian"], ["pt", "Portuguese"],
    ["ta", "Tamil"], ["te", "Telugu"], ["ko", "Korean"],
    ["zh", "Chinese"], ["ru", "Russian"], ["ar", "Arabic"],
    ["pl", "Polish"], ["tr", "Turkish"], ["uk", "Ukrainian"],
    ["id", "Indonesian"], ["nl", "Dutch"], ["th", "Thai"],
  ];
  const choices = (firstLabel) => languages.map(([code, name]) =>
    `<option value="${code}">${esc(code === "auto" ? firstLabel : name)}</option>`).join("");

  button.onclick = async (event) => {
    event.stopPropagation();
    if (document.getElementById("prefsoverlay")) return;
    const layer = document.createElement("div");
    layer.id = "prefsoverlay";
    layer.className = "prefsoverlay";
    layer.innerHTML = `<section class="prefscard" role="dialog" aria-modal="true" aria-labelledby="prefstitle">
      <div class="prefshead"><h2 id="prefstitle">Watch Together preferences</h2>
        <button type="button" id="prefsclose" aria-label="Close preferences">${icon("x")}</button></div>
      <p class="note">These settings belong to your account. Automatic tries your first audio language, then your backup, then the stream default.</p>
      <label>Preferred audio language<select id="prefaudio">${choices("Stream default")}</select></label>
      <label>Backup audio language<select id="prefbackup">${choices("Stream default")}</select></label>
      <label>Subtitles with Japanese audio (sub)<select id="prefsubsub">
        <option value="auto">Automatic</option><option value="off">Subtitles off</option>
        ${choices("Automatic").replace('<option value="auto">Automatic</option>', "")}
      </select></label>
      <label>Subtitles with other audio (dub)<select id="prefsub">
        <option value="auto">Automatic</option><option value="off">Subtitles off</option>
        ${choices("Automatic").replace('<option value="auto">Automatic</option>', "")}
      </select></label>
      <p class="note">Manual player choices take priority until you select Automatic again. Unavailable subtitle languages remain off.</p>
      <p class="prefstatus" id="prefstatus" role="status"></p>
      <div class="prefsactions"><button type="button" class="btn alt" id="prefcancel">Cancel</button>
        <button type="button" class="btn" id="prefsave">Save preferences</button></div>
    </section>`;
    document.body.appendChild(layer);
    button.setAttribute("aria-expanded", "true");
    const audio = layer.querySelector("#prefaudio");
    const backup = layer.querySelector("#prefbackup");
    const sub = layer.querySelector("#prefsub");
    const subSub = layer.querySelector("#prefsubsub");
    const status = layer.querySelector("#prefstatus");
    const save = layer.querySelector("#prefsave");
    const available = (el, value) => {
      el.value = value;
      if (!el.value) el.value = "auto";
    };
    const updateBackup = () => {
      backup.disabled = audio.value === "auto";
      if (backup.disabled || backup.value === audio.value) backup.value = "auto";
    };
    audio.onchange = updateBackup;
    backup.onchange = updateBackup;
    const onKey = (e) => {
      if (e.key === "Escape") { e.stopPropagation(); close(); }
    };
    const close = () => {
      document.removeEventListener("keydown", onKey, true);
      layer.remove();
      button.setAttribute("aria-expanded", "false");
      button.focus();
    };
    document.addEventListener("keydown", onKey, true);
    layer.addEventListener("click", (e) => { if (e.target === layer) close(); });
    layer.querySelector("#prefsclose").onclick = close;
    layer.querySelector("#prefcancel").onclick = close;
    save.disabled = true;
    status.textContent = "Loading your preferences…";
    try {
      const current = await api("watch/preferences");
      if (!layer.isConnected) return;
      available(audio, current.audio || "auto");
      available(backup, current.backup_audio || "auto");
      available(sub, current.subtitles || "auto");
      available(subSub, current.sub_subtitles || "auto");
      updateBackup();
      save.disabled = false;
      status.textContent = "";
      audio.focus();
    } catch (error) {
      status.textContent = error.message;
      return;
    }
    save.onclick = async () => {
      save.disabled = true;
      status.textContent = "Saving…";
      try {
        const saved = await api("watch/preferences", {
          audio: audio.value, backup_audio: backup.value, subtitles: sub.value, sub_subtitles: subSub.value,
        });
        if (typeof wPreferencesChanged === "function") wPreferencesChanged(saved);
        close();
      } catch (error) {
        save.disabled = false;
        status.textContent = error.message;
      }
    };
  };
})();

// The first tap anywhere wakes the audio (browsers block it before).
addEventListener(
  "pointerdown",
  () => {
    if (soundMode === "off" || !audio()) return;
    if (actx.state !== "running") actx.resume().then(soundApply);
  },
  true
);
document.addEventListener("visibilitychange", soundApply);

// Buttons tap; a new hanko stamp thuds as it lands.
addEventListener("click", (e) => {
  const b = e.target.closest && e.target.closest("button");
  if (b && b.id !== "snd" && !b.disabled) sfx("tap");
});
new MutationObserver((list) => {
  for (const m of list)
    for (const n of m.addedNodes)
      if (n.nodeType === 1 && (n.classList.contains("stamp") || n.querySelector(".stamp"))) {
        setTimeout(() => sfx("stamp"), 280);
        return;
      }
}).observe(app, { childList: true, subtree: true });

// ── Admin tools ──
//
// Staff get a shield button with a sheet of steps for the room they are in:
// moderation (pause, skip, end, take back a win, sit someone out, hide their
// answers, fix points, a break from games) and harmless pranks (flipped
// screen, fake wrong stamps, silly hints, runaway buttons, fake points,
// confetti and an air horn, swapped pictures, rigged imposter and bomb,
// Koi-Koi bots talking back). The bot keeps the state: pranks end when the
// room goes back to the lobby or the next game starts after one finished.

let admPick = ""; // the member the sheet has open
let admNote = "";
let fxSeen = -1;
let dodgeUntil = 0;

// adminRoom applies what the room says about pranks and effects on this
// screen, whatever page is open.
function adminRoom(v) {
  const t = v.troll || {};
  document.body.classList.toggle("flip-upside", t.flip === "upside");
  document.body.classList.toggle("flip-mirror", t.flip === "mirror");
  dodgeUntil = t.dodge ? Date.now() + t.dodge * 1000 : 0;
  if (!dodgeUntil) undodge();
  pranks = new Set(t.on || []);
  for (const k of ["comic", "textflip", "fish", "reverse", "drunk", "invisible", "spotlight"]) document.body.classList.toggle("p-" + k, pranks.has(k));
  fakeAdmin(!v.admin && pranks.has("fake_admin"));
  gamesOff = v.off || [];
  const fx = v.fx || [];
  const top = fx.reduce((m, f) => Math.max(m, f.id), 0);
  if (fxSeen < 0 || top < fxSeen) fxSeen = top; // joining: don't replay old ones
  for (const f of fx) if (f.id > fxSeen) playFx(f);
  fxSeen = Math.max(fxSeen, top);
  admButton(v);
  if (document.getElementById("admsheet")) admSheet();
}

// fxPop shows big text in the middle of the screen for a moment.
function fxPop(html, ms, cls = "") {
  const el = document.createElement("div");
  el.className = "fxpop " + cls;
  el.innerHTML = html;
  document.body.appendChild(el);
  setTimeout(() => el.remove(), ms);
  return el;
}

function playFx(f) {
  switch (f.kind) {
    case "confetti":
      confetti(110, true);
      break;
    case "horn":
      sfxForce("horn");
      fxPop("HONK", 1400, "honk");
      break;
    case "boom":
      sfxForce("boom");
      fxPop("💥", 1200, "boom");
      document.body.classList.remove("quake");
      void document.body.offsetWidth;
      document.body.classList.add("quake");
      setTimeout(() => document.body.classList.remove("quake"), 900);
      break;
    case "hint":
      fxPop(`${icon("bulb")}<span><small>Hint</small>${esc(f.text)}</span>`, 6000, "hint");
      sfx("ok");
      break;
    case "disconnect": {
      // A fake lost connection for 5 seconds, then back like nothing happened.
      sfxForce("drop");
      const el = fxPop(`<span class="spin"></span><b>Connection lost</b><small>Reconnecting to Discord…</small>`, 5000, "drop");
      setTimeout(() => (el.innerHTML = `<b>Reconnected</b><small>jk, an admin got you 😜</small>`), 4000);
      break;
    }
    case "tiny":
    case "huge":
      document.body.classList.remove("p-tiny", "p-huge");
      document.body.classList.add("p-" + f.kind);
      clearTimeout(playFx.size);
      playFx.size = setTimeout(() => document.body.classList.remove("p-tiny", "p-huge"), 30000);
      break;
    case "fakekick":
    case "fakeban": {
      const ban = f.kind === "fakeban";
      sfxForce("bad");
      const el = fxPop(
        ban
          ? `${icon("shield")}<b>Banned from games</b><small>30 days · Reason: being too good at this</small>`
          : `${icon("x")}<b>You were kicked from the room</b><small>An admin removed you</small>`,
        6000,
        "fake " + f.kind
      );
      setTimeout(() => {
        el.classList.add("jk");
        el.innerHTML = `<b>jk</b><small>${ban ? "you're not banned" : "you're still here"} 😜</small>`;
        sfx("ok");
      }, 3500);
      break;
    }
    case "fakewin": {
      sfxForce("win");
      confetti(120, true);
      const el = fxPop(`<b>YOU WIN!</b><small>+500 points · new record</small>`, 5200, "pts win");
      setTimeout(() => {
        el.classList.add("nope");
        el.innerHTML = `<b>jk</b><small>🤡 the game isn't over</small>`;
        sfx("bad");
      }, 3000);
      break;
    }
    case "update": {
      const el = fxPop(`<b>Updating Otakase Activity</b><small>Don't close Discord</small><span class="bar"><i></i></span><small class="pc">0%</small>`, 7600, "drop upd");
      const t0 = Date.now();
      const tick = setInterval(() => {
        const pc = Math.min(100, Math.round(((Date.now() - t0) / 5200) * 100 * (0.7 + Math.random() * 0.3)));
        const bar = el.querySelector(".bar i");
        if (!bar) return clearInterval(tick);
        bar.style.width = pc + "%";
        el.querySelector(".pc").textContent = pc >= 99 ? "99%… 99%… 99%…" : pc + "%";
      }, 250);
      setTimeout(() => {
        clearInterval(tick);
        el.innerHTML = `<b>Update failed successfully</b><small>jk, nothing changed 😜</small>`;
      }, 6000);
      break;
    }
    case "facerain": {
      const m = (tv && tv.members.find((x) => x.id === f.text)) || { id: f.text, name: "?" };
      rain(av(m), 40);
      sfx("ok");
      break;
    }
    case "boo":
    case "applause": {
      const m = (tv && tv.members.find((x) => x.id === f.text)) || {};
      const boo = f.kind === "boo";
      sfxForce(boo ? "boo" : "clap");
      rain(boo ? "🍅" : "👏", 30);
      if (m.name) fxPop(`<span><small>${boo ? "The crowd boos" : "Standing ovation for"}</small>${esc(m.name)}</span>`, 3800, "hint");
      break;
    }
    case "jumpscare":
      // Soft: a cute ghost and a gentle "boo", nothing loud.
      sfxForce("ghost");
      fxPop(`<b>👻</b><small>boo!</small>`, 1500, "ghost");
      break;
    case "quote":
      fxPop(`<span><small>An anime character says</small>“${esc(f.text)}”</span>`, 5500, "hint quote");
      sfx("ok");
      break;
    case "warn": {
      // A real warning: it stays until they tap OK.
      sfxForce("bad");
      const el = fxPop(
        `${icon("shield")}<b>Warning from staff</b><small>${esc(f.text)}</small><button class="pill" type="button">OK</button>`,
        120000,
        "warnpop"
      );
      el.querySelector("button").onclick = () => el.remove();
      break;
    }
    case "points": {
      const el = fxPop(`<b>+1000</b><small>bonus points!</small>`, 4200, "pts");
      confetti(80, true);
      setTimeout(() => {
        el.classList.add("nope");
        el.innerHTML = `<b>lol no</b><small>🤡 an admin got you</small>`;
        sfx("bad");
      }, 2300);
      break;
    }
  }
}

// prankStamps turns fake 残念 stamps into "jk" ones.
function prankStamps() {
  document.querySelectorAll(".stamp.prank").forEach((el) => {
    el.className = "stamp ok jk";
    el.innerHTML = "正解<small>JK · CORRECT</small>";
    sfx("ok");
  });
}

// Runaway buttons: while the prank lasts, buttons in the page hop away from
// the pointer most of the time.
function undodge() {
  document.querySelectorAll("#app .dodged").forEach((b) => {
    b.classList.remove("dodged");
    b.style.transform = "";
  });
}
addEventListener(
  "pointerdown",
  (e) => dodge(e),
  true
);
addEventListener("pointerover", (e) => e.pointerType === "mouse" && dodge(e), true);
// The lag prank: taps land a second late, with a spinner meanwhile.
addEventListener(
  "click",
  (e) => {
    if (!pranks.has("lag") || !e.isTrusted) return;
    const b = e.target.closest && e.target.closest("#app button");
    if (!b) return;
    e.preventDefault();
    e.stopPropagation();
    b.classList.add("lagging");
    setTimeout(() => (b.classList.remove("lagging"), b.click()), 1100 + Math.random() * 700);
  },
  true
);
// The cursed keyboard: now and then a key types a neighbour instead.
const KEYROWS = ["qwertyuiop", "asdfghjkl", "zxcvbnm"];
addEventListener(
  "beforeinput",
  (e) => {
    if (!pranks.has("cursed") || e.inputType !== "insertText" || !e.data || e.data.length !== 1 || Math.random() > 0.25) return;
    const ch = e.data.toLowerCase();
    const row = KEYROWS.find((r) => r.includes(ch));
    if (!row) return;
    const i = row.indexOf(ch);
    const near = row[i + (Math.random() < 0.5 && i > 0 ? -1 : i < row.length - 1 ? 1 : -1)];
    const el = e.target;
    if (!el.setRangeText) return;
    e.preventDefault();
    el.setRangeText(near, el.selectionStart, el.selectionEnd, "end");
    el.dispatchEvent(new Event("input", { bubbles: true }));
  },
  true
);
// The spotlight: the page is dark but for a circle round the pointer.
addEventListener("pointermove", (e) => {
  if (!pranks.has("spotlight")) return;
  document.body.style.setProperty("--sx", e.clientX + "px");
  document.body.style.setProperty("--sy", e.clientY + "px");
});

// fakeAdmin gives a member a shield button that looks like staff tools and
// does nothing but say no.
function fakeAdmin(on) {
  let b = document.getElementById("fakeadm");
  if (!on) return b && b.remove();
  if (b) return;
  b = document.createElement("button");
  b.id = "fakeadm";
  b.className = "adm";
  b.title = b.ariaLabel = "Admin tools";
  b.innerHTML = icon("shield");
  const no = ["Access denied lol", "Nice try 🤡", "You're not staff, you're stuff", "Permission: none", "Error 418: I'm a teapot"];
  b.onclick = () => {
    if (document.querySelector(".modal")) return;
    const m = document.createElement("div");
    m.className = "modal";
    m.innerHTML = `<div class="sheet adms" role="dialog"><h2>Admin <span>tools</span></h2><p class="note">You have the power. Use it wisely.</p><div class="abtns">${[
      "Ban everyone",
      "Give me 1000 points",
      "Win this round",
      "Delete the server",
      "Make me host forever",
    ]
      .map((t) => `<button class="ab">${esc(t)}</button>`)
      .join("")}</div><p class="anote" role="status"></p></div>`;
    document.body.appendChild(m);
    m.onclick = (e) => e.target === m && m.remove();
    m.querySelectorAll(".ab").forEach(
      (x) =>
        (x.onclick = () => {
          m.querySelector(".anote").textContent = no[Math.floor(Math.random() * no.length)];
          sfx("bad");
        })
    );
  };
  document.getElementById("activitytools").appendChild(b);
}

// The duck prank: every tap quacks.
addEventListener("pointerdown", () => pranks.has("quack") && sfxForce("quack"), true);
function dodge(e) {
  if (!dodgeUntil) return;
  if (Date.now() > dodgeUntil) {
    dodgeUntil = 0;
    return undodge();
  }
  const b = e.target.closest && e.target.closest("#app button, #app .pick, #app .qo");
  if (!b || Math.random() < 0.3) return;
  b.classList.add("dodged");
  b.style.transform = `translate(${Math.round((Math.random() - 0.5) * 180)}px, ${Math.round((Math.random() - 0.5) * 80)}px) scale(.85) rotate(${Math.round(
    (Math.random() - 0.5) * 20
  )}deg)`;
  if (e.type === "pointerdown") {
    e.preventDefault();
    e.stopPropagation();
  }
}

// adminAfterDraw adds the room's admin state to the party page.
function adminAfterDraw(box, v) {
  const outText = {
    kicked: "An admin sat you out of this game. You're back for the next one.",
    watch: "An admin made you a spectator. You can watch, not play.",
    room: "An admin took you out of this room for 10 minutes. You can watch.",
    locked: "An admin locked this room. You can watch.",
  }[v.out || (v.kicked ? "kicked" : "")];
  const ob = box.querySelector(".admbar.kick");
  if (outText && ob && !ob.textContent.includes(outText)) ob.remove();
  if (outText && !box.querySelector(".admbar.kick")) box.insertAdjacentHTML("afterbegin", `<p class="admbar kick">${icon("shield")} ${esc(outText)}</p>`);
  if (!outText && ob) ob.remove();
  const pb = box.querySelector(".admbar.pause");
  if (v.paused && !pb) box.insertAdjacentHTML("afterbegin", `<p class="admbar pause">${icon("clock")} Paused by an admin</p>`);
  if (!v.paused && pb) pb.remove();
  if (v.kk && v.kk.say) {
    const log = box.querySelector(".klog");
    if (log && !log.querySelector(".ksay")) log.insertAdjacentHTML("afterbegin", `<p class="ksay">${esc(v.kk.say)}</p>`);
  }
  // Swapped buttons: answer choices come in reverse order.
  if (pranks.has("swapbtn"))
    box.querySelectorAll(".qopts, .picks").forEach((c) => {
      if (c.dataset.sw === String(c.children.length)) return;
      c.dataset.sw = String(c.children.length);
      [...c.children].reverse().forEach((x) => c.appendChild(x));
    });
  if (!v.admin) return;
  box.querySelectorAll(".feed li[data-i]:not(.ok)").forEach((li) => {
    if (li.querySelector(".fhide")) return;
    const b = document.createElement("button");
    b.className = "fhide";
    b.title = "Hide this answer";
    b.textContent = "×";
    b.onclick = () => admDo("hide", "", "", +li.dataset.i);
    li.appendChild(b);
  });
}

function admButton(v) {
  let b = document.getElementById("adm");
  if (!v.admin) {
    if (b) b.remove();
    const m = document.getElementById("admsheet");
    if (m) m.closest(".modal").remove();
    return;
  }
  if (b) return;
  b = document.createElement("button");
  b.id = "adm";
  b.className = "adm";
  b.title = b.ariaLabel = "Admin tools";
  b.innerHTML = icon("shield");
  b.onclick = (e) => {
    e.stopPropagation();
    admOpen();
  };
  document.getElementById("activitytools").appendChild(b);
}

function admDo(kind, target = "", text = "", n = 0) {
  return api("party/admin", { room, kind, target, text, n })
    .then((v) => {
      admNote = kind === "tickets" ? `Balance: ${v.balance} watch tickets.` : "Done.";
      if (v.view) setRoom(v.view);
      else if (document.getElementById("admsheet")) admSheet();
    })
    .catch((e) => {
      admNote = e.message;
      if (document.getElementById("admsheet")) admSheet();
    });
}

function admOpen() {
  if (document.querySelector(".modal")) return;
  const m = document.createElement("div");
  m.className = "modal";
  m.innerHTML = `<div class="sheet adms" id="admsheet" role="dialog" aria-label="Admin tools"></div>`;
  document.body.appendChild(m);
  m.onclick = (e) => e.target === m && m.remove();
  admNote = "";
  admSheet();
}

const GAME_NAMES = {
  guess: "Guess the anime race",
  riddle: "Emoji riddle race",
  scramble: "Title scramble race",
  hl: "Higher or lower party",
  quiz: "Quiz show",
  truths: "Two truths and a lie",
  imposter: "Imposter",
  draw: "Draw it",
  bomb: "Hot potato",
  who: "Guess who",
  koikoi: "Koi-Koi",
};

// admSheet draws the sheet from the room as last seen.
function admSheet() {
  const el = document.getElementById("admsheet");
  if (!el || !tv || !tv.admin) return;
  const a = tv.admin;
  const running = tv.phase !== "lobby" && tv.phase !== "over";
  const game = tv.game ? GAME_NAMES[tv.game] || (GAMES.find((g) => g.key === tv.game) || {}).name || tv.game : "";
  const btn = (kind, label, opts = {}) =>
    `<button class="ab${opts.on ? " on" : ""}${opts.prank ? " pr" : ""}" data-k="${kind}" data-t="${esc(opts.target || "")}" data-x="${esc(opts.text || "")}" data-n="${
      opts.n || 0
    }"${opts.off ? " disabled" : ""}>${opts.icon ? icon(opts.icon) : ""}${esc(label)}</button>`;
  const who = (id) => ((a.members.find((m) => m.id === id) || {}).name || "someone");
  const gameBtns = [
    running ? (tv.paused ? btn("resume", "Resume", { icon: "play" }) : btn("pause", "Pause", { icon: "pause" })) : "",
    a.skip ? btn("skip", "Skip round", { icon: "skip" }) : "",
    a.void ? btn("void", `Take back ${who(a.void)}'s win`, { icon: "undo" }) : "",
    a.ink ? btn("clear-ink", "Clear the drawing", { icon: "draw" }) : "",
    running ? btn("end", "End game", { icon: "x" }) : "",
  ].join("");
  const fun = [
    btn("confetti", "Confetti on everyone", { prank: true }),
    btn("horn", "Air horn for everyone", { prank: true }),
    btn("boom", "Earthquake for everyone", { prank: true }),
    btn("swap", "Swap everyone's pictures", { prank: true, on: a.swap }),
    btn("trash", "Koi-Koi bots talk back", { prank: true, on: a.trash }),
    btn("fx", "Fake disconnect for everyone", { text: "disconnect", prank: true }),
    btn("fx", "Anime quote for everyone", { text: "quote", prank: true }),
    btn("fx", "Fake update for everyone", { text: "update", prank: true }),
    btn("fx", "Soft jumpscare for everyone", { text: "jumpscare", prank: true }),
    btn("roulette", "Name roulette", { prank: true, on: a.roulette }),
  ].join("");
  const roomBtns = btn("lock", a.locked ? "Unlock the room" : "Lock the room", { icon: "shield", on: a.locked });
  const offRow = [
    `<select id="aoff" aria-label="Game">${(a.games || [])
      .map((g) => `<option value="${g}">${esc(GAME_NAMES[g] || (GAMES.find((x) => x.key === g) || {}).name || g)}${a.off.includes(g) ? " (off)" : ""}</option>`)
      .join("")}</select>${btn("game-off", "Off / on for everyone")}`,
  ].join("");
  const toggle = (t, m, kind, label) => btn("toggle", label, { ...t, text: kind, prank: true, on: (m.on || []).includes(kind) });
  const rows = a.members
    .map((m) => {
      const tags = [
        m.admin ? "staff" : "",
        m.kicked ? "sitting out" : "",
        m.muted ? "muted" : "",
        m.flip ? (m.flip === "upside" ? "upside down" : "mirrored") : "",
        m.fake_wrong ? "fake wrong" : "",
        m.dodge ? "dodgy buttons" : "",
        a.imp_rig === m.id ? "always imposter" : "",
        a.bomb_rig === m.id ? "bomb magnet" : "",
        m.out === "watch" ? "spectator" : "",
        m.out === "room" ? "out of the room" : "",
        m.slow ? "slowed" : "",
        m.hidden ? "name hidden" : "",
        m.rename ? "called " + m.rename : "",
        (m.on || []).length ? (m.on || []).length + " pranks on" : "",
        tv.host === m.id ? "host" : "",
      ].filter(Boolean);
      const open = admPick === m.id;
      const self = m.id === tv.me;
      const t = { target: m.id };
      const acts = open
        ? `<div class="aacts">
          <h4>Moderation</h4><div class="abtns">
            ${self ? "" : m.kicked ? btn("unkick", "Let back in", t) : btn("kick", "Sit out this game", t)}
            ${m.muted ? btn("unmute", "Show their answers", t) : btn("mute", "Hide their answers", t)}
            ${btn("points", "+1 point", { ...t, n: 1 })}${btn("points", "−1 point", { ...t, n: -1 })}
            ${self ? "" : btn("bench", "Break: 1 hour", { ...t, n: 1 }) + btn("bench", "Break: 1 day", { ...t, n: 24 })}
            ${m.here && tv.host !== m.id ? btn("host", "Make host", t) : ""}
            ${self ? "" : btn("watch", "Spectator", { ...t, on: m.out === "watch" })}
            ${self ? "" : btn("room-out", "Out of room 10 min", { ...t, on: m.out === "room" })}
            ${btn("slow", "1 answer / 3s", { ...t, on: m.slow })}
            ${btn("hide-name", "Hide Discord name", { ...t, on: m.hidden })}
          </div>
          ${self ? "" : `<div class="ahint"><input id="awarn" maxlength="300" placeholder="Why? (counts as a /warn)">${btn("warn", "Warn", t)}</div>`}
          <h4>Pranks</h4><div class="abtns">
            ${btn("flip", "Upside down", { ...t, text: "upside", prank: true, on: m.flip === "upside" })}
            ${btn("flip", "Mirror", { ...t, text: "mirror", prank: true, on: m.flip === "mirror" })}
            ${btn("fake-wrong", "Fake wrong stamp", { ...t, prank: true, on: m.fake_wrong })}
            ${btn("dodge", "Runaway buttons 30s", { ...t, prank: true, on: m.dodge })}
            ${btn("fake-points", "Fake +1000", { ...t, prank: true })}
            ${btn("confetti", "Confetti", { ...t, prank: true })}
            ${btn("horn", "Air horn", { ...t, prank: true })}
            ${btn("rig-imposter", "Always imposter", { ...t, prank: true, on: a.imp_rig === m.id })}
            ${btn("rig-bomb", "Bomb magnet", { ...t, prank: true, on: a.bomb_rig === m.id })}
          </div>
          <h4>Screen pranks</h4><div class="abtns">
            ${btn("fx", "Fake disconnect", { ...t, text: "disconnect", prank: true })}
            ${btn("fx", "Shrink 30s", { ...t, text: "tiny", prank: true })}
            ${btn("fx", "Grow 30s", { ...t, text: "huge", prank: true })}
            ${btn("fx", "Fake kick", { ...t, text: "fakekick", prank: true })}
            ${btn("fx", "Fake ban", { ...t, text: "fakeban", prank: true })}
            ${btn("fx", "Anime quote", { ...t, text: "quote", prank: true })}
            ${btn("fx", "Fake win", { ...t, text: "fakewin", prank: true })}
            ${btn("fx", "Fake update", { ...t, text: "update", prank: true })}
            ${btn("fx", "Soft jumpscare", { ...t, text: "jumpscare", prank: true })}
            ${btn("fx", "Rain their face", { ...t, text: "facerain", prank: true })}
            ${btn("fx", "Boo them", { ...t, text: "boo", prank: true })}
            ${btn("fx", "Applause", { ...t, text: "applause", prank: true })}
          </div>
          <h4>Until the game ends</h4><div class="abtns">
            ${toggle(t, m, "ghost", "Ghost typing")}
            ${toggle(t, m, "fast", "2× timer")}
            ${toggle(t, m, "comic", "Rainbow Comic Sans")}
            ${toggle(t, m, "textflip", "Upside-down text")}
            ${toggle(t, m, "fake_top", "Fake #1")}
            ${toggle(t, m, "fish", "Fish cursor")}
            ${toggle(t, m, "reverse", "Typing backwards")}
            ${toggle(t, m, "pink", "Pink pen")}
            ${toggle(t, m, "quack", "Quacking taps")}
            ${self ? "" : toggle(t, m, "fake_admin", "Fake admin button")}
            ${toggle(t, m, "lag", "Fake lag")}
            ${toggle(t, m, "cursed", "Cursed keyboard")}
            ${toggle(t, m, "invisible", "Invisible typing")}
            ${toggle(t, m, "swapbtn", "Swapped buttons")}
            ${toggle(t, m, "drunk", "Drunk mode")}
            ${toggle(t, m, "spotlight", "Spotlight")}
            ${toggle(t, m, "lie_votes", "Lying vote counts")}
          </div>
          <div class="ahint"><input id="ahint" maxlength="120" placeholder="Fake hint (empty = a silly one)">${btn("hint", "Send", { ...t, prank: true })}</div>
          <div class="ahint"><input id="aname" maxlength="32" placeholder="${esc(m.rename ? "Tap to take " + m.rename + " off" : "Silly name (empty = random)")}">${btn("rename", m.rename ? "Rename / undo" : "Rename", { ...t, prank: true, on: !!m.rename })}</div>
          <div class="ahint"><input id="apup" maxlength="60" placeholder="Say something as them (feed)">${btn("puppet", "Puppet", { ...t, prank: true })}</div>
        </div>`
        : "";
      return `<div class="arow${open ? " open" : ""}${m.here ? "" : " gone"}"><button class="ahead" data-pick="${esc(m.id)}">${av(m)}<span><b>${esc(m.name)}${
        self ? " (you)" : ""
      }</b><small>${esc(tags.join(" · ") || (m.here ? "playing" : "not here"))}</small></span>${icon(open ? "up" : "down")}</button>${acts}</div>`;
    })
    .join("");
  const scroll = el.closest(".modal").scrollTop;
  const boxes = ["ahint", "aname", "apup", "awarn", "aoff", "aticketuser", "aticketcount"];
  const kept = {};
  for (const id of boxes) {
    const b = document.getElementById(id);
    if (b) kept[id] = { v: b.value, focus: document.activeElement === b };
  }
  el.innerHTML = `<button class="pill close" id="admclose">Close</button>
    <h2>Admin <span>tools</span></h2>
    <p class="note">${running ? esc(game) + (tv.paused ? " · paused" : "") : "In the lobby"} · ${tv.members.length} here. Pranks end when the game does; moderation steps go to the staff log.</p>
    ${gameBtns ? `<h3 class="lh">This game</h3><div class="abtns">${gameBtns}</div>` : ""}
    <h3 class="lh">Room and server</h3><div class="abtns">${roomBtns}</div><div class="ahint">${offRow}</div>
    ${a.owner ? `<h3 class="lh">Watch tickets</h3><p class="note">Server owner only. Enter a Discord user ID and select a ticket adjustment.</p>
      <div class="ahint"><input id="aticketuser" inputmode="numeric" placeholder="Discord user ID" maxlength="20" value="${esc(admPick || "")}">
      <input id="aticketcount" type="number" min="1" max="100" value="1" aria-label="Ticket amount"></div>
      <div class="abtns">${btn("tickets", "Add tickets", { n: 1 })}${btn("tickets", "Remove tickets", { n: -1 })}</div>` : ""}
    <h3 class="lh">Pranks for everyone</h3><div class="abtns">${fun}</div>
    <h3 class="lh">Players</h3><div class="arows">${rows}</div>
    <p class="anote" role="status">${esc(admNote)}</p>`;
  el.closest(".modal").scrollTop = scroll;
  for (const id of boxes) {
    const b = document.getElementById(id);
    if (!b || !kept[id]) continue;
    b.value = kept[id].v;
    if (kept[id].focus) b.focus();
  }
  document.getElementById("admclose").onclick = () => el.closest(".modal").remove();
  el.querySelectorAll("[data-pick]").forEach((b) => (b.onclick = () => ((admPick = admPick === b.dataset.pick ? "" : b.dataset.pick), (admNote = ""), admSheet())));
  el.querySelectorAll("[data-k]").forEach(
    (b) =>
      (b.onclick = () => {
        if (b.dataset.k === "end" && !b.classList.contains("sure")) {
          b.classList.add("sure");
          b.lastChild.textContent = "Sure? Tap again";
          return;
        }
        let text = b.dataset.x;
        const from = { hint: "ahint", rename: "aname", puppet: "apup", warn: "awarn", "game-off": "aoff" }[b.dataset.k];
        if (from) text = (document.getElementById(from) || {}).value || "";
        if (b.dataset.k === "puppet" || b.dataset.k === "warn") (document.getElementById(from) || {}).value = "";
        let target = b.dataset.t;
        let amount = +b.dataset.n;
        if (b.dataset.k === "tickets") {
          target = (document.getElementById("aticketuser") || {}).value?.trim() || "";
          const n = +(document.getElementById("aticketcount") || {}).value;
          if (!/^\d{17,20}$/.test(target) || !Number.isInteger(n) || n < 1 || n > 100) {
            admNote = "Enter a Discord user ID and 1 to 100 tickets.";
            admSheet();
            return;
          }
          amount *= n;
        }
        b.disabled = true;
        admDo(b.dataset.k, target, text, amount);
      })
  );
}

start().catch((e) => message("Couldn't start: " + e.message));
