// Discord's side of the Activity, through the Embedded App SDK: who is in
// this Activity and who is talking in the call, the member's Discord status
// line (what they play or watch here), the device it runs on and how it is
// held, and the small window Discord shrinks it to (picture in picture, or a
// tile in the call grid). Uses app.js's sdk, me and helpers; every call here
// can fail (an older Discord, a scope the member didn't allow) and the page
// works the same without it.
"use strict";

// The scopes the Activity asks for: identify signs in, rpc.voice.read shows
// who talks, rpc.activities.write sets the status line. A member who turns
// the extra ones down signs in with identify alone.
const DS_SCOPES = ["identify", "rpc.voice.read", "rpc.activities.write"];
const DS_ICON = "https://otakase.com/img/icon-512.png";
const DS_LAYOUT = { 0: "focused", 1: "pip", 2: "grid" };
const DS_DOING = { party: "Playing party games", board: "Checking the leaderboard", bingo: "Filling a watch bingo card" };

let dsMe = ""; // this member's id
let dsPeople = []; // everyone in this Activity: {id, name}
let dsPeopleSeen = false; // the first list arrived, so later changes are joins and leaves
const dsTalking = new Set(); // ids talking in the call right now
let dsActivity = null; // the status line last asked for
let dsActivityKey = "";
let dsActivitySent = 0;
let dsActivityTimer = 0;
let dsToastTimer = 0;

// dsStart wires the SDK events once the member is signed in.
function dsStart(id) {
  dsMe = id || "";
  const body = document.body;
  body.dataset.platform = sdk.platform === "mobile" ? "mobile" : "desktop";
  body.dataset.layout = "focused";
  const sub = (evt, fn, args) => {
    sdk.subscribe(evt, fn, args).catch(() => {});
  };
  sub("ORIENTATION_UPDATE", (d) => dsOrient(d.orientation || (d.screen_orientation === 1 ? "landscape" : "portrait")));
  sub("ACTIVITY_LAYOUT_MODE_UPDATE", (d) => dsLayout(DS_LAYOUT[d.layout_mode] || "focused"));
  sub("ACTIVITY_INSTANCE_PARTICIPANTS_UPDATE", (d) => dsPeopleSet(d.participants));
  sdk.commands
    .getInstanceConnectedParticipants()
    .then((d) => dsPeopleSet(d.participants))
    .catch(() => {});
  // Speaking needs a voice call; a launch from a text channel has none.
  if (sdk.channelId) {
    sub("SPEAKING_START", (d) => dsTalk(d.user_id, true), { channel_id: sdk.channelId });
    sub("SPEAKING_STOP", (d) => dsTalk(d.user_id, false), { channel_id: sdk.channelId });
  }
}

// ── Who's here ──

const dsName = (u) => u.nickname || u.global_name || u.username || "Someone";

function dsPeopleSet(list) {
  const next = (list || []).filter((u) => u && u.id && !u.bot).map((u) => ({ id: String(u.id), name: dsName(u) }));
  if (dsPeopleSeen) {
    const was = new Set(dsPeople.map((p) => p.id));
    const now = new Set(next.map((p) => p.id));
    const came = next.filter((p) => !was.has(p.id) && p.id !== dsMe);
    const left = dsPeople.filter((p) => !now.has(p.id) && p.id !== dsMe);
    if (came.length) dsToast(`${dsNames(came)} joined the Activity`);
    else if (left.length) dsToast(`${dsNames(left)} left the Activity`);
  }
  dsPeople = next;
  dsPeopleSeen = true;
  for (const id of [...dsTalking]) if (!next.some((p) => p.id === id)) dsTalking.delete(id);
  dsHereDraw();
  dsActivityRefresh();
}

function dsNames(list) {
  const names = list.map((p) => p.name);
  return names.length > 2 ? `${names[0]}, ${names[1]} and ${names.length - 2} more` : names.join(" and ");
}

function dsTalk(id, on) {
  if (!id) return;
  id = String(id);
  if (on === dsTalking.has(id)) return;
  if (on) dsTalking.add(id);
  else dsTalking.delete(id);
  document.querySelectorAll(`[data-talk="${CSS.escape(id)}"]`).forEach((el) => el.classList.toggle("talking", on));
  dsTalkDraw();
}

// dsFace is a round profile picture that lights up while its owner talks.
function dsFace(id, name) {
  return `<span class="dsface${dsTalking.has(id) ? " talking" : ""}" data-talk="${esc(id)}" title="${esc(name)}"><img src="api/avatar/${encodeURIComponent(id)}" alt="" loading="lazy" onerror="this.remove()"><b>${esc((name || "?").slice(0, 1).toUpperCase())}</b></span>`;
}

// dsHere is the strip of everyone in this Activity, for a page to place
// in a #dshere box on the menu.
function dsHere(label) {
  if (dsPeople.length < 2) return "";
  return `<span class="dslabel">${esc(label)}</span>${dsPeople.map((p) => dsFace(p.id, p.name)).join("")}`;
}

function dsHereDraw() {
  const el = document.getElementById("dshere");
  if (!el) return;
  el.innerHTML = dsHere(el.dataset.label || "Here");
  el.hidden = !el.innerHTML;
}

// dsTalkDraw shows who talks over the video in theater mode, where the
// people list is out of sight.
function dsTalkDraw() {
  const el = document.getElementById("wtalk");
  if (!el) return;
  const talking = dsPeople.filter((p) => dsTalking.has(p.id));
  el.innerHTML = talking.map((p) => `<span>${dsFace(p.id, p.name)}${esc(p.name)}</span>`).join("");
  el.hidden = !talking.length;
}

function dsToast(text) {
  let el = document.getElementById("dstoast");
  if (!el) {
    el = document.createElement("div");
    el.id = "dstoast";
    el.className = "dstoast";
    el.setAttribute("role", "status");
    document.body.appendChild(el);
  }
  el.textContent = text;
  el.classList.add("on");
  clearTimeout(dsToastTimer);
  dsToastTimer = setTimeout(() => el.classList.remove("on"), 3500);
}

// ── Discord status line ──

// dsSetActivity asks Discord to show a on the member's profile. Discord
// limits how often that can change, so a burst only sends the last one,
// at most every 5 seconds.
function dsSetActivity(a) {
  dsActivity = a;
  dsPipDraw();
  const key = JSON.stringify(a);
  if (!sdk || key === dsActivityKey) return;
  clearTimeout(dsActivityTimer);
  const wait = Math.max(0, dsActivitySent + 5000 - Date.now());
  dsActivityTimer = setTimeout(() => {
    dsActivityKey = key;
    dsActivitySent = Date.now();
    const act = { type: a.type || 0, details: a.details, state: a.state || undefined };
    act.assets = { large_image: a.image || DS_ICON, large_text: a.imageText || "Otakase", small_image: a.image ? DS_ICON : undefined, small_text: a.image ? "Otakase" : undefined };
    if (a.start) act.timestamps = { start: a.start };
    if (a.size) act.party = { id: room || undefined, size: a.size };
    // Should a client not take Watching, Playing still shows the line.
    sdk.commands.setActivity({ activity: act }).catch(() => act.type && sdk.commands.setActivity({ activity: { ...act, type: 0 } }).catch(() => {}));
  }, wait);
}

// dsActivityRefresh redraws the status line for where the member is now,
// e.g. after the number of people here changed.
function dsActivityRefresh() {
  if (dsGame !== null) dsGameActivity(dsGame);
}

let dsGame = null; // the game the status line names; "" for the menu

function dsGameActivity(key) {
  dsGame = key;
  const g = GAMES.find((x) => x.key === key);
  const n = dsPeople.length;
  dsSetActivity({
    type: 0,
    details: !g ? "Picking a game" : DS_DOING[key] || `Playing ${g.name}`,
    state: n > 1 ? `${n} in this Activity` : "Otakase Activity",
  });
}

// ── Device and layout ──

function dsOrient(o) {
  document.body.dataset.orient = o;
}

// dsLayout follows Discord shrinking the Activity: in picture in picture
// or a call grid tile the page shows a card saying what is going on.
function dsLayout(mode) {
  document.body.dataset.layout = mode;
  dsPipDraw();
}

function dsPipDraw() {
  let el = document.getElementById("dspip");
  if (!el) {
    el = document.createElement("div");
    el.id = "dspip";
    el.className = "dspip";
    el.setAttribute("aria-hidden", "true");
    document.body.appendChild(el);
  }
  const a = dsActivity || { details: "Otakase Activity" };
  el.innerHTML = `${a.pic ? `<img src="${esc(a.pic)}" alt="">` : ""}<div><b>${esc(a.details || "")}</b><small>${esc(a.state || "")}</small></div>`;
}
