// Otakase Bot cards for Home Assistant dashboards. The add-on writes this
// file to /config/www and adds it as a dashboard resource, so the cards show
// up in the card picker:
//
//   otakase-bot-card              the whole bot dashboard in a frame
//   otakase-bot-status-card       version, members, latency, failing jobs
//   otakase-bot-controls-card     verification, spoiler guard, invites, …
//   otakase-bot-today-card        airing board, watch parties, voice rooms
//   otakase-bot-leaderboard-card  levels, game points, guess the anime, helpers
//   otakase-bot-support-card      support posts, bugs, known issue notes
//
// They reach the add-on through Home Assistant's ingress, like the add-on
// page, so they work for admin users.
const SLUG = "__SLUG__";
const VERSION = "__VERSION__";
const PAGES = __PAGES__;

// ── Connection: one ingress session and one summary for all cards ──

const otk = (window.otakaseBotShared = window.otakaseBotShared || {
  base: null,
  starting: null,
  summary: null,
  fetched: 0,
  loading: null,
  listeners: new Set(),
});

async function otkStart(hass) {
  if (otk.base) return otk.base;
  if (!otk.starting) {
    otk.starting = (async () => {
      const api = (endpoint, method, data) => hass.callWS({ type: "supervisor/api", endpoint, method, data });
      const info = await api(`/addons/${SLUG}/info`, "get");
      const newSession = async () => {
        const { session } = await api("/ingress/session", "post");
        const secure = location.protocol === "https:" ? ";Secure" : "";
        document.cookie = `ingress_session=${session};path=/api/hassio_ingress/;SameSite=Strict${secure}`;
        return session;
      };
      let session = await newSession();
      setInterval(async () => {
        try {
          await api("/ingress/validate_session", "post", { session });
        } catch (e) {
          session = await newSession();
        }
      }, 60000);
      otk.base = info.ingress_url;
      return otk.base;
    })().catch((e) => {
      otk.starting = null;
      throw e;
    });
  }
  return otk.starting;
}

async function otkSummary(hass, fresh) {
  const base = await otkStart(hass);
  if (!fresh && otk.summary && Date.now() - otk.fetched < 15000) return otk.summary;
  if (!otk.loading) {
    otk.loading = fetch(base + "api/summary", { credentials: "same-origin" })
      .then((r) => {
        if (!r.ok) throw new Error("The add-on answered " + r.status);
        return r.json();
      })
      .then((s) => {
        otk.summary = s;
        otk.fetched = Date.now();
        otk.listeners.forEach((f) => f(s));
        return s;
      })
      .finally(() => {
        otk.loading = null;
      });
  }
  return otk.loading;
}

async function otkDo(hass, action) {
  const base = await otkStart(hass);
  const s = otk.summary || (await otkSummary(hass));
  const r = await fetch(base + "api/do", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", "X-Otakase-Token": s.token },
    body: JSON.stringify(action),
  });
  const res = r.ok ? await r.json() : { ok: false, message: "The bot restarted. Try again." };
  if (r.status === 403) otk.summary = null; // new token after a restart
  otkSummary(hass, true).catch(() => {});
  return res;
}

const esc = (s) =>
  String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);

function ago(sec) {
  if (sec < 60) return "under a minute";
  if (sec < 3600) return Math.floor(sec / 60) + " min";
  if (sec < 172800) return Math.floor(sec / 3600) + " h";
  return Math.floor(sec / 86400) + " days";
}

function clock(unix) {
  return new Date(unix * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

const STYLE = `
  :host { display: block; }
  ha-card { padding: 16px; }
  .head { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
  .head h2 { margin: 0; font-size: 1.15rem; font-weight: 500; flex: 1; }
  .dot { width: 10px; height: 10px; border-radius: 50%; background: var(--success-color, #43a047); }
  .dot.off { background: var(--error-color, #db4437); }
  .muted { color: var(--secondary-text-color); font-size: 0.9rem; }
  .tiles { display: grid; grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 8px; }
  .tile { background: var(--secondary-background-color); border-radius: 10px; padding: 8px 10px; }
  .tile b { display: block; font-size: 1.2rem; font-weight: 600; }
  .tile.bad { outline: 2px solid var(--error-color, #db4437); }
  h3 { font-size: 0.95rem; font-weight: 500; margin: 14px 0 6px; color: var(--secondary-text-color); }
  ul { list-style: none; margin: 0; padding: 0; }
  li { display: flex; gap: 8px; align-items: baseline; padding: 5px 0; border-bottom: 1px solid var(--divider-color); }
  li:last-child { border-bottom: 0; }
  li .grow { flex: 1; min-width: 0; overflow-wrap: anywhere; }
  li .now { color: var(--primary-color); font-weight: 600; }
  li .aired { opacity: 0.55; }
  .bad { color: var(--error-color, #db4437); }
  .row { display: flex; align-items: center; gap: 8px; padding: 6px 0; flex-wrap: wrap; }
  .row .label { flex: 1; min-width: 120px; }
  .seg { display: inline-flex; border: 1px solid var(--divider-color); border-radius: 999px; overflow: hidden; }
  .seg button { border: 0; background: none; color: var(--primary-text-color); padding: 6px 12px; cursor: pointer; font: inherit; }
  .seg button.on { background: var(--primary-color); color: var(--text-primary-color, #fff); }
  .btns { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 10px; }
  .btn { border: 1px solid var(--divider-color); background: none; color: var(--primary-text-color); border-radius: 999px;
         padding: 6px 14px; cursor: pointer; font: inherit; }
  .btn.main { background: var(--primary-color); border-color: var(--primary-color); color: var(--text-primary-color, #fff); }
  .btn:disabled, .seg button:disabled { opacity: 0.5; cursor: progress; }
  .tabs { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 8px; }
  .n { width: 1.6em; color: var(--secondary-text-color); }
  .time { white-space: nowrap; color: var(--secondary-text-color); font-variant-numeric: tabular-nums; }
`;

// ── Base for the native cards ──

class OtakaseBase extends HTMLElement {
  setConfig(config) {
    this._config = config || {};
    if (this._s) this._draw();
  }

  set hass(hass) {
    this._hass = hass;
    if (!this.shadowRoot) {
      this.attachShadow({ mode: "open" }).innerHTML = `<style>${STYLE}</style><ha-card><div class="body muted">Loading…</div></ha-card>`;
      this.shadowRoot.addEventListener("click", (e) => this._click(e));
    }
    if (!this._listening) this._listen();
  }

  _listen() {
    this._listening = true;
    this._onSummary = (s) => {
      this._s = s;
      this._draw();
    };
    otk.listeners.add(this._onSummary);
    const load = () =>
      otkSummary(this._hass)
        .then(this._onSummary)
        .catch(() => this._body(`<p class="muted">Can't reach the Otakase Bot add-on. It needs an admin user and the add-on running.</p>`));
    load();
    this._timer = setInterval(load, 30000);
  }

  disconnectedCallback() {
    clearInterval(this._timer);
    otk.listeners.delete(this._onSummary);
    this._listening = false;
  }

  connectedCallback() {
    if (this._hass && !this._listening) this._listen();
  }

  _body(html) {
    const card = this.shadowRoot.querySelector("ha-card");
    card.innerHTML = html;
  }

  _title(text, dot) {
    const t = this._config.title ?? text;
    return `<div class="head">${dot === undefined ? "" : `<span class="dot${dot ? "" : " off"}"></span>`}<h2>${esc(t)}</h2></div>`;
  }

  async _click(e) {
    const el = e.target.closest("[data-do]");
    if (!el || el.disabled) return;
    const action = { do: el.dataset.do, name: el.dataset.name || "", v: el.dataset.v || "", text: "" };
    if (el.dataset.confirm && !confirm(el.dataset.confirm)) return;
    this.shadowRoot.querySelectorAll("[data-do]").forEach((b) => (b.disabled = true));
    let msg;
    try {
      const res = await otkDo(this._hass, action);
      msg = res.message || (res.ok ? "Done." : "That didn't work.");
    } catch (err) {
      msg = "Couldn't reach the bot.";
    }
    this.dispatchEvent(new CustomEvent("hass-notification", { detail: { message: msg }, bubbles: true, composed: true }));
    this.shadowRoot.querySelectorAll("[data-do]").forEach((b) => (b.disabled = false));
  }

  getCardSize() {
    return 4;
  }

  static getConfigForm() {
    return { schema: [{ name: "title", selector: { text: {} } }] };
  }
}

// ── Status ──

class OtakaseStatusCard extends OtakaseBase {
  _draw() {
    const s = this._s;
    const tiles = [
      ["Members", s.members || "—"],
      ["Latency", s.connected ? s.latency_ms + " ms" : "offline"],
      ["Uptime", ago(s.uptime)],
      ["Version", s.version],
    ];
    const failing = s.jobs.filter((j) => j.state === "failing");
    const jobs = failing.length
      ? `<h3 class="bad">${failing.length} job${failing.length > 1 ? "s" : ""} failing</h3><ul>${failing
          .map((j) => `<li><span class="grow"><b>${esc(j.name)}</b><br><span class="muted">${esc(j.problem)}</span></span></li>`)
          .join("")}</ul>`
      : `<p class="muted">All ${s.jobs.length} background jobs are working.</p>`;
    this._body(
      this._title("Otakase Bot", s.connected) +
        `<div class="tiles">${tiles.map(([l, v]) => `<div class="tile"><b>${esc(v)}</b><span class="muted">${l}</span></div>`).join("")}</div>` +
        jobs +
        (this._config.details
          ? `<p class="muted">Memory ${s.memory_mb} MB · data ${s.data_kb} KB · ${s.follows} members use /follow · ${s.linked_apps} linked apps</p>`
          : "")
    );
  }

  static getStubConfig() {
    return {};
  }

  static getConfigForm() {
    return { schema: [{ name: "title", selector: { text: {} } }, { name: "details", selector: { boolean: {} } }] };
  }
}

// ── Controls ──

class OtakaseControlsCard extends OtakaseBase {
  _seg(label, doName, current, options, name) {
    return `<div class="row"><span class="label">${label}</span><span class="seg">${options
      .map(
        ([v, l]) =>
          `<button data-do="${doName}" data-v="${v}"${name ? ` data-name="${name}"` : ""} class="${v === current ? "on" : ""}">${l}</button>`
      )
      .join("")}</span></div>`;
  }

  _draw() {
    const s = this._s;
    const set = Object.fromEntries(s.settings.map((x) => [x.name, x]));
    const onOff = (name, label) =>
      set[name] ? this._seg(label, "setting", set[name].value, [["true", "On"], ["false", "Off"]], name) : "";
    const verifyNow = s.safety.verify_on ? "on now" : "off now";
    this._body(
      this._title("Bot controls") +
        this._seg(`Join verification <span class="muted">· ${verifyNow}${s.safety.waiting ? `, ${s.safety.waiting} waiting` : ""}</span>`, "verify", s.safety.verify_mode, [
          ["auto", "Auto"],
          ["on", "On"],
          ["off", "Off"],
        ]) +
        this._seg("Spoiler guard", "spoiler", s.safety.spoiler, [
          ["off", "Off"],
          ["nudge", "Nudge"],
          ["hide", "Hide"],
        ]) +
        onOff("daily_airing", "Daily airing board") +
        onOff("welcome_dm", "Welcome DM") +
        `<div class="btns">
          <button class="btn main" data-do="invites-pause" data-confirm="Pause invites for 30 minutes?">Pause invites</button>
          <button class="btn" data-do="invites-resume">Resume invites</button>
          <button class="btn" data-do="airing">Redraw airing board</button>
          <button class="btn" data-do="stale">Check quiet posts</button>
          <button class="btn" data-do="commands">Re-sync commands</button>
        </div>`
    );
  }

  static getStubConfig() {
    return {};
  }
}

// ── Today ──

class OtakaseTodayCard extends OtakaseBase {
  _draw() {
    const s = this._s;
    const max = Number(this._config.shows) || 8;
    const now = s.airing.filter((a) => a.state !== "aired");
    const shows = (now.length ? now : s.airing).slice(0, max);
    const airing = shows.length
      ? `<ul>${shows
          .map(
            (a) =>
              `<li class="${a.state === "aired" ? "aired" : ""}"><span class="time">${clock(a.at)}</span><span class="grow">${esc(a.title)}</span>` +
              `<span class="${a.state === "airing" ? "now" : "muted"}">${a.state === "airing" ? "now" : esc(a.episode)}</span></li>`
          )
          .join("")}</ul>`
      : `<p class="muted">No airing board today.</p>`;
    const parties = s.parties.length
      ? `<ul>${s.parties
          .map(
            (p) =>
              `<li><span class="grow">${esc(p.name)}<br><span class="muted">${p.live ? "live now" : clock(p.start)} · ${esc(p.host)} · ${p.interested} interested</span></span>` +
              `<button class="btn" data-do="party-cancel" data-name="${esc(p.id)}" data-confirm="Cancel ${esc(p.name)}?">Cancel</button></li>`
          )
          .join("")}</ul>`
      : `<p class="muted">No watch parties planned.</p>`;
    const rooms = s.rooms.length
      ? `<ul>${s.rooms
          .map(
            (r) =>
              `<li><span class="grow">${esc(r.name)}<br><span class="muted">${esc(r.owner)} · ${r.people} in it</span></span>` +
              `<button class="btn" data-do="room-close" data-name="${esc(r.id)}" data-confirm="Close ${esc(r.name)}?">Close</button></li>`
          )
          .join("")}</ul>`
      : `<p class="muted">No voice rooms open.</p>`;
    const daily = s.games && s.games.daily && s.games.daily.num
      ? `<p class="muted">Daily puzzle #${s.games.daily.num}: ${s.games.daily.players} played, ${s.games.daily.solved} solved</p>`
      : "";
    const rounds =
      (s.guess.rounds.length ? `<p class="muted">Guess the anime running in ${s.guess.rounds.map(esc).join(", ")}</p>` : "") + daily;
    this._body(
      this._title("Today on Discord") +
        `<h3>Airing</h3>${airing}<h3>Watch parties</h3>${parties}<h3>Voice rooms</h3>${rooms}${rounds}`
    );
  }

  static getStubConfig() {
    return {};
  }

  static getConfigForm() {
    return {
      schema: [
        { name: "title", selector: { text: {} } },
        { name: "shows", selector: { number: { min: 1, max: 30, mode: "box" } } },
      ],
    };
  }
}

// ── Leaderboards ──

const BOARDS = [
  ["levels", "Levels"],
  ["games", "Games · week"],
  ["games-all", "Games · all time"],
  ["week", "Guess · week"],
  ["all", "Guess · all time"],
  ["helpers", "Helpers"],
];

class OtakaseLeaderboardCard extends OtakaseBase {
  _draw() {
    const s = this._s;
    const board = this._board || this._config.board || "levels";
    const games = s.games || { week: [], all: [] };
    const list = { levels: s.levels, games: games.week, "games-all": games.all, week: s.guess.week, all: s.guess.all, helpers: s.support.helpers }[board] || [];
    const tabs = this._config.board
      ? ""
      : `<div class="tabs">${BOARDS.map(
          ([k, l]) => `<button class="btn${k === board ? " main" : ""}" data-board="${k}">${l}</button>`
        ).join("")}</div>`;
    const rows = list.length
      ? `<ul>${list
          .map(
            (r, i) =>
              `<li><span class="n">${i + 1}.</span><span class="grow">${esc(r.name)}</span><span class="muted">${
                board === "levels" ? `lvl ${r.level} · ${r.points} XP` : `${r.points} pts`
              }</span></li>`
          )
          .join("")}</ul>`
      : `<p class="muted">Nobody yet.</p>`;
    const reset =
      board === "week" ? `<div class="btns"><button class="btn" data-do="guess-reset-week" data-confirm="Reset this week's guess points?">Reset week</button></div>` : "";
    this._body(this._title("Leaderboard") + tabs + rows + reset);
  }

  _click(e) {
    const tab = e.target.closest("[data-board]");
    if (tab) {
      this._board = tab.dataset.board;
      this._draw();
      return;
    }
    super._click(e);
  }

  static getStubConfig() {
    return {};
  }

  static getConfigForm() {
    return {
      schema: [
        { name: "title", selector: { text: {} } },
        { name: "board", selector: { select: { mode: "dropdown", options: BOARDS.map(([value, label]) => ({ value, label })) } } },
      ],
    };
  }
}

// ── Support ──

class OtakaseSupportCard extends OtakaseBase {
  _draw() {
    const s = this._s;
    const tiles = [
      ["Open posts", s.support.open, s.support.open > 10],
      ["Bugs waiting", s.support.bugs],
      ["Open suggestions", s.support.suggestions_open],
      ["Warnings, 30 days", s.safety.warnings_30d],
    ];
    const notes = s.support.known_notes.length
      ? `<h3>Known issue notes</h3><ul>${s.support.known_notes
          .map(
            (n) =>
              `<li><span class="grow">${esc(n)}</span><button class="btn" data-do="known-remove" data-name="${esc(n)}" data-confirm="Remove this note?">Remove</button></li>`
          )
          .join("")}</ul>`
      : "";
    this._body(
      this._title("Support") +
        `<div class="tiles">${tiles
          .map(([l, v, bad]) => `<div class="tile${bad ? " bad" : ""}"><b>${esc(v)}</b><span class="muted">${l}</span></div>`)
          .join("")}</div>` +
        notes +
        `<div class="btns"><button class="btn" data-do="stale">Check quiet posts</button><button class="btn" data-do="known-refresh">Update known issues post</button></div>`
    );
  }

  static getStubConfig() {
    return {};
  }
}

// ── Full dashboard in a frame ──

class OtakaseBotCard extends HTMLElement {
  setConfig(config) {
    this._config = config || {};
    if (this._frame) this._frame.style.height = this._height();
  }

  set hass(hass) {
    this._hass = hass;
    if (!this._started) {
      this._started = true;
      this._render();
      this._open();
    }
  }

  _height() {
    const h = Number(this._config.height);
    return (h > 0 ? h : 600) + "px";
  }

  _render() {
    const card = document.createElement("ha-card");
    if (this._config.title) card.header = this._config.title;
    this._frame = document.createElement("iframe");
    this._frame.title = "Otakase Bot";
    this._frame.style.cssText = "display:block;width:100%;border:0;border-radius:var(--ha-card-border-radius,12px)";
    this._frame.style.height = this._height();
    this._note = document.createElement("div");
    this._note.style.cssText = "padding:16px;color:var(--secondary-text-color)";
    this._note.textContent = "Opening the Otakase Bot dashboard…";
    card.append(this._note, this._frame);
    this.replaceChildren(card);
  }

  async _open() {
    try {
      const base = await otkStart(this._hass);
      const page = this._config.page ? "?s=" + encodeURIComponent(this._config.page) : "";
      this._frame.src = base + page;
      this._note.remove();
    } catch (e) {
      this._note.textContent = "Couldn't open the Otakase Bot dashboard. It needs an admin user and the add-on running.";
    }
  }

  getCardSize() {
    return Math.ceil(parseInt(this._height(), 10) / 50);
  }

  getGridOptions() {
    return { columns: "full", rows: Math.ceil(parseInt(this._height(), 10) / 56), min_rows: 4 };
  }

  static getStubConfig() {
    return { page: "overview", height: 600 };
  }

  static getConfigForm() {
    return {
      schema: [
        { name: "title", selector: { text: {} } },
        { name: "page", selector: { select: { mode: "dropdown", options: PAGES } } },
        { name: "height", selector: { number: { min: 200, max: 2000, step: 50, unit_of_measurement: "px" } } },
      ],
    };
  }
}

// ── Register ──

const CARDS = [
  ["otakase-bot-status-card", OtakaseStatusCard, "Otakase Bot: status", "Version, members, latency and failing background jobs."],
  ["otakase-bot-controls-card", OtakaseControlsCard, "Otakase Bot: controls", "Join verification, spoiler guard, invites and other switches."],
  ["otakase-bot-today-card", OtakaseTodayCard, "Otakase Bot: today", "Today's airing board, watch parties and voice rooms."],
  ["otakase-bot-leaderboard-card", OtakaseLeaderboardCard, "Otakase Bot: leaderboard", "Chat levels, game points, guess the anime points and top helpers."],
  ["otakase-bot-support-card", OtakaseSupportCard, "Otakase Bot: support", "Support posts, bugs, suggestions and known issue notes."],
  ["otakase-bot-card", OtakaseBotCard, "Otakase Bot: full dashboard", "The whole bot dashboard with every page and control."],
];

window.customCards = window.customCards || [];
for (const [type, cls, name, description] of CARDS) {
  if (customElements.get(type)) continue;
  customElements.define(type, cls);
  window.customCards.push({
    type,
    name,
    description,
    preview: false,
    documentationURL: "https://github.com/TheXykril/otakase/wiki/Developer-Discord-Bot",
  });
}
console.info("Otakase Bot cards " + VERSION);
