// The watch room: an anime episode the bot hosts in the Activity, watched
// in step by everyone in the call (watchroom.go). The host picks and
// controls; everyone else follows the host's position and can ask for a
// pause, the opening skipped or the next episode. Uses app.js's helpers.
"use strict";

let wRoom = ""; // the watch room this page is in: this call's, or one joined from the lobby
let wv = null; // the room as the server last showed it
let wvAt = 0; // when it arrived, for the follow clock
let wvMedia = ""; // the media the player has loaded
let wvKey = ""; // what the page has drawn
let wvLoop = 0; // bumps to stop an old poll loop
let wHls = null;
let wFollow = 0;
let wBeat = 0;
let wHostAct = 0; // when the host last did something, so its own echo isn't followed
let wNeedTap = false;
let wFails = 0;
// wNoNative is set after the player's built-in HLS failed on this media;
// Hls.js gets the next try before the source counts as broken.
let wNoNative = "";
// wDirect is set once a segment failed to load through the edge cache
// (wv.cdn): from then on this player asks the bot for its playlists with
// ?direct=1, which keeps every segment on the bot.
let wDirect = false;
let wLastErr = "";
let wSubPick = "auto";
let wAudioPick = "auto";
let wSettingsDirty = false;
let wSearch = [];
let wLists = null; // the host's own Continue list
let wFull = false; // the player fills the window
let wControlsTimer = 0;
let wChanging = false; // the host is picking another show while one plays
let wOsdTimer = 0;
let wKeySeek = null; // where the last key seek went, so held arrows add up
let wReadySent = 0; // the ready check this player last said it was ready for
let wJump = false; // the host's spot moved: follow it at once
let wChatSeen = 0; // the last chat message drawn
let wEventSeen = 0; // the last room event flashed
let wEventTimer = 0;
let wAskSeen = 0; // the last request the host was pinged for
let wAskKey = ""; // the requests the theater pop-ups show now
let wChatUnread = 0;
let wChatOpen = false; // the chat panel in theater mode
let wChatDraft = "";
let wQuality = -1; // Hls.js level this viewer picked, -1 for automatic
let wSubLook = null; // this viewer's subtitle size, colour and background

const wFmt = (s) => {
  s = Math.max(0, Math.floor(s || 0));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = String(s % 60).padStart(2, "0");
  return h ? `${h}:${String(m).padStart(2, "0")}:${ss}` : `${m}:${ss}`;
};

const ASK_TEXT = { pause: "pause", play: "play", forward: "skip ahead 10 seconds", next: "go to the next episode" };

// wSecs is a jump's length: 50s, or 2:30 from a minute up.
const wSecs = (n) => (n < 60 ? `${Math.round(n)}s` : wFmt(n));

// wAskText is what a request asks for, after "asks to".
function wAskText(a) {
  if (a.kind === "seek") return `jump to ${wFmt(a.pos || 0)}`;
  if (a.kind === "forward" && a.by && a.by !== 10) return a.by > 0 ? `skip ahead ${wSecs(a.by)}` : `go back ${wSecs(-a.by)}`;
  return ASK_TEXT[a.kind] || a.kind;
}

// watchView is the tile's page: the lobby, then the room. note, when set,
// says why the visitor is back in the lobby (their room ended).
async function watchView(note) {
  const said = note ? `<p class="wnote">${esc(note)}</p>` : "";
  page("Watch <span>room</span>", `<div id="wbox">${loading("Looking for the watch room…")}</div>`);
  where = "watch";
  soundApply();
  const st = await api(`watch/status?room=${encodeURIComponent(room)}`);
  if (where !== "watch") return;
  if (st.here) return watchJoin(room);
  const live = st.rooms || [];
  const liveCards = live.map(wLiveCard).join("");
  if (st.full) {
    const ends = live.map((r) => r.ends).filter(Boolean);
    const by = ends.length ? ` The next one ends by ${new Date(Math.min(...ends)).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}.` : "";
    wBox(`${said}<h3 class="lh">${icon("watch")} Live now</h3>${liveCards}
      <p class="note">All ${st.rooms_max} watch room${st.rooms_max === 1 ? " is" : "s are"} in use, since the bot streams each one itself. Join one to watch along from here, or hop into its voice channel to talk too.${by}</p>
      <div class="err" id="err"></div>`);
    return wLiveButtons();
  }
  const t = st.tickets || 0;
  wBox(`${said}${live.length ? `<h3 class="lh">${icon("watch")} Live now</h3>${liveCards}
    <p class="note">Join a room to watch along from here (go to its voice channel to talk too), or open a room of your own in this call.</p>` : ""}<p class="lead">The bot plays an anime for everyone in this call, in step. Whoever opens the room picks the show and controls it; everyone else can ask for a pause or a skip.</p>
    <p class="note">Up to ${st.max} people · 720p · ${st.perks ? "8" : "4"} hours from the first episode · your AniList or MAL moves on as you watch (link them with /link-anilist and /link-mal).</p>
    ${st.perks
      ? `<p>${icon("star")} ${st.perks === "staff" ? "You're <b>staff</b>" : "You're this week's <b>Game Champion</b>"}: your rooms need no ticket, last 8 hours and fit up to ${st.max} people.</p>`
      : `<p>${icon("star")} You have <b>${t}</b> watch ticket${t === 1 ? "" : "s"}. Opening a room uses one once the first episode plays.</p>`}
    ${t > 0 || st.perks ? `<button class="btn" id="wopen">${icon("play")} Open the watch room</button>` : `<p class="note">Get a ticket by redeeming a code with <b>/redeem</b>. Codes come from giveaways, events and somewhere hidden in Otakase.</p>`}
    <div class="err" id="err"></div>`);
  const b = document.getElementById("wopen");
  if (b) b.onclick = () => ((b.disabled = true), watchJoin(room).catch((e) => ((b.disabled = false), showError(e))));
  wLiveButtons();
}

// wLiveCard is a room running in another call, as the lobby shows it, with
// a button to join it from here and one to go to its voice channel. An 18+
// room only says that one runs and where, and can't be joined from another
// call.
function wLiveCard(st) {
  const at = st.channel_name ? "#" + st.channel_name : "another call";
  const people = `${st.people || 0}/${st.max || 0} watching`;
  const full = (st.people || 0) >= (st.max || 0);
  const join = st.id
    ? `<div class="wlivebtns"><button class="btn" data-wjoin="${esc(st.id)}"${full ? " disabled" : ""}>${icon("play")} ${full ? "Full" : "Join"}</button>${
        st.link ? `<button class="btn alt" data-wvoice="${esc(st.link)}">${icon("users")} Go to ${esc(at)}</button>` : ""
      }</div>`
    : "";
  if (st.adult || !st.show) {
    return `<div class="wlive"><i></i><span><b>${st.adult ? "A private room" : "Picking a show"}</b><small>${esc(at)} · ${people}</small>${join}</span></div>`;
  }
  const ep = st.ep ? `Episode ${st.ep}${st.last_ep ? " of " + st.last_ep : ""}` : "";
  return `<div class="wlive">${st.cover ? `<img src="${esc(st.cover)}" alt="" loading="lazy">` : "<i></i>"}
    <span><b>${esc(st.show)}</b><small>${[ep, st.playing ? "playing" : "paused"].filter(Boolean).join(" · ")}</small>
    <small>${icon("crown")} ${esc(st.host || "Someone")} · ${people} · <b>${esc(at)}</b></small>${join}</span></div>`;
}

// wLiveButtons wires the lobby's Join and voice channel buttons.
function wLiveButtons() {
  for (const b of document.querySelectorAll("#wbox [data-wjoin]")) {
    b.onclick = () => ((b.disabled = true), watchJoin(b.dataset.wjoin).catch((e) => ((b.disabled = false), showError(e))));
  }
  for (const b of document.querySelectorAll("#wbox [data-wvoice]")) b.onclick = () => openLink(b.dataset.wvoice);
}

function wBox(html) {
  const detached = document.body.querySelector(":scope > #wstage");
  const el = document.getElementById("wbox");
  if (!el) return;
  if (detached && wFull) {
    const next = document.createElement("div");
    next.innerHTML = html;
    const nextStage = next.querySelector("#wstage");
    if (nextStage) {
      // Keep the fullscreen element itself mounted. Replacing it forces
      // native fullscreen to exit and can't be undone without another tap.
      detached.className = nextStage.className;
      detached.innerHTML = nextStage.innerHTML;
      const marker = document.createElement("div");
      marker.id = "wstage-placeholder";
      nextStage.replaceWith(marker);
      el.replaceChildren(...next.childNodes);
      return;
    }
  }
  if (detached) detached.remove();
  const marker = document.getElementById("wstage-placeholder");
  if (marker) marker.remove();
  el.innerHTML = html;
}

// watchJoin joins the room with this id: this call's own (opening it when
// none runs yet), or one running in another call.
async function watchJoin(id) {
  const v = await api("watch/open", { room: id, channel });
  wRoom = id;
  wvKey = wvMedia = "";
  // Messages from before this member came in aren't news.
  wChatSeen = Math.max(0, ...(v.chat || []).map((c) => c.id));
  wEventSeen = Math.max(0, ...(v.events || []).map((e) => e.id));
  wAskSeen = Math.max(0, ...(v.asks || []).map((a) => a.id));
  wReadySent = 0;
  wAskKey = "";
  wSet(v);
  const me = ++wvLoop;
  (async () => {
    while (me === wvLoop && where === "watch") {
      const r = await fetch(`api/watch/state?room=${encodeURIComponent(wRoom)}&v=${wv ? wv.ver : -1}`, {
        headers: { Authorization: "Bearer " + session },
      }).catch(() => null);
      if (me !== wvLoop || where !== "watch") break;
      // Dropped from the room (gone quiet too long, or it moved on without
      // this member): back to the lobby rather than waiting on it forever.
      if (r && r.status === 403) return wGone("You're no longer in that watch room.");
      const nv = r && r.ok ? await r.json().catch(() => null) : null;
      if (nv) wSet(nv);
      else await new Promise((ok) => setTimeout(ok, 3000));
      if (wv && wv.phase === "over") break;
    }
    if (me === wvLoop) where === "watch" ? wStop() : watchLeave();
  })();
}

// wGone leaves a room that ended (or this member was dropped from) and puts
// the lobby back, so a new room can be opened or a live one joined without
// closing the Activity. A watch launch has no games menu to fall back to.
function wGone(note) {
  if (wFull) wFullscreen(false);
  wStop();
  wv = null;
  wvKey = wvMedia = "";
  wChanging = false;
  watchView(note).catch(showError);
}

// wSet takes a new view of the room and redraws what changed.
function wSet(v, fromHost) {
  if (!v || where !== "watch") return;
  const previous = wv;
  const previousAt = wvAt;
  const oldSkipSeq = previous ? previous.skip_seq || 0 : 0;
  const oldAudioMedia = JSON.stringify((wv && wv.audio_media) || {});
  const oldPreferences = JSON.stringify((wv && wv.preferences) || {});
  wv = v;
  wvAt = Date.now();
  if (v.languages && !wSettingsDirty) {
    wAudioPick = v.languages.audio || "auto";
    wSubPick = v.languages.subtitles || "auto";
  }
  if (v.phase === "over") return wGone("The watch room is over. Thanks for watching!");
  const key = [v.phase, v.is_host, v.show && v.show.id, v.ep, v.err, v.media, v.load_step].join("|");
  if (wChanging && v.is_host) {
    wvKey = key;
    return;
  }
  wChanging = false;
  if (key !== wvKey) {
    // Episode changes pass through loading, often for several seconds.
    // Keep the theater shell and native fullscreen element for that transition.
    const sameShow = previous && previous.show && v.show && previous.show.id === v.show.id;
    const episodePhase = (phase) => phase === "play" || phase === "loading";
    const keepTheater = wFull && sameShow && episodePhase(previous.phase) && episodePhase(v.phase);
    if (wFull && !keepTheater) wFullscreen(false);
    if (previous && previous.phase === "play" && v.phase !== "play") wDestroy();
    const stage = document.getElementById("wstage");
    const wasHidden = keepTheater && stage && stage.classList.contains("controls-hidden");
    clearTimeout(wControlsTimer);
    wvKey = key;
    wDraw();
    // A new episode keeps the controls the way the viewer left them.
    if (keepTheater) wasHidden ? wHideControls() : wRevealControls();
  }
  const elapsedEl = document.getElementById("wloadelapsed");
  if (elapsedEl && v.load_since) {
    const seconds = Math.max(0, Math.floor((Date.now() - v.load_since) / 1000));
    elapsedEl.textContent = `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
  }
  // A viewer follows the host at once when the room's spot jumps (a seek,
  // an automatic skip); drifting behind on its own offers a catch-up instead.
  if (!v.is_host && previous && previous.phase === "play" && v.phase === "play") {
    const was = previous.pos + (previous.playing ? (wvAt - previousAt) / 1000 : 0);
    if (Math.abs(v.pos - was) > 3 || previous.playing !== v.playing) wJump = true;
  }
  wEvents();
  wPeople();
  wVote();
  wAsks();
  wHoldDraw();
  wChatDraw();
  wSyncLine();
  wSkipControls();
  wSeekMarkers();
  if (oldAudioMedia !== JSON.stringify(v.audio_media || {}) ||
      oldPreferences !== JSON.stringify(v.preferences || {})) {
    wAudioTracks();
    wSubs();
  }
  wUpdateOrigin();
  const holdChanged = previous && (previous.hold || 0) !== (v.hold || 0);
  // A new episode (or the room back after a bot restart) tries the edge
  // cache again: a failure there may have been the bot being away.
  if (previous && previous.media && v.media && previous.media !== v.media) wDirect = false;
  if (v.media && wEffectiveMedia() !== wvMedia) wLoad();
  else if (fromHost || holdChanged || (v.is_host && v.skip_seq && oldSkipSeq !== v.skip_seq)) wApply(true);
  else if (wJump) wApply(false);
}

function wDraw() {
  const v = wv;
  const showLine = v.show ? `<b>${esc(v.show.title)}</b> · episode ${v.ep}${v.last_ep ? ` of ${v.last_ep}` : ""}` : "";
  let stage;
  if (v.phase === "pick" || (v.phase === "error" && !v.media)) {
    stage = v.is_host
      ? `${v.err ? `<p class="err">${esc(v.err)}</p>` : ""}<div id="wpick"></div>`
      : `<p class="msg">${v.err ? esc(v.err) : "The host is picking what to watch…"}</p>`;
  } else if (v.phase === "loading") {
    const step = v.load_step || "Finding episode sources";
    const elapsed = v.load_since ? Math.max(0, Math.floor((Date.now() - v.load_since) / 1000)) : 0;
    const loadingStage = `<div class="msg loading"><p><b id="wloadstep">${esc(step)}</b></p><p class="note">Elapsed: <span id="wloadelapsed">${Math.floor(elapsed / 60)}:${String(elapsed % 60).padStart(2, "0")}</span></p><p class="note">Alternative playback can take several minutes to connect and buffer. The current step updates as preparation advances.</p></div>`;
    stage = wFull
      ? `<div class="wstage full" id="wstage"><div class="wplayer wloading" id="wplayer">${loadingStage}</div>
        <button class="btn alt wloadingexit" id="wfull" aria-label="Exit theater mode" title="Exit theater mode">${icon("minimize")}</button></div>`
      : loadingStage;
  } else {
    stage = `<div class="wstage${wFull ? " full" : ""}" id="wstage"><div class="wplayer" id="wplayer"><video id="wvid" playsinline preload="auto"></video>
      <button class="wtap" id="wtap" hidden>${icon("play")} Tap to watch</button><div class="wspin" id="wspin" hidden></div><div class="wosd" id="wosd" hidden></div><div class="wevent" id="wevent" hidden></div><div class="wreq" id="wreq"></div>
      <div class="whold" id="whold" hidden></div><button class="wcatch" id="wcatch" hidden></button>
      <div class="wvote" id="wvote" hidden></div><div class="wconfirm" id="wconfirm" hidden></div></div>
      <div class="wbar" id="wbar"></div></div>`;
  }
  wBox(`<div class="whead"><span class="wshow">${showLine || "Nothing playing yet"}</span><span class="pill" id="wends"></span></div>
    <div class="wmain"><div class="wleft">${stage}
    <div class="wasks" id="wasks"></div></div>
    <div class="wchatslot" id="wchatslot">${wChatBox()}</div></div>
    <div class="wside"><h3 class="lh">${icon("users")} Watching <span id="wcount"></span></h3><div class="wpeople" id="wpeople"></div>
    <p class="note" id="wsync"></p><div class="row" id="wmore"></div></div>
    <div class="err" id="err"></div>`);
  if (document.getElementById("wpick")) wPicker();
  const loadingExit = v.phase === "loading" && wFull && document.getElementById("wfull");
  if (loadingExit) loadingExit.onclick = () => { wFullscreen(false); wDraw(); };
  if (document.getElementById("wbar")) {
    wBar();
    const player = document.getElementById("wplayer");
    if (player) player.addEventListener("click", (e) => {
      if (!wFull || e.target.closest("button, input, select, a, label")) return;
      const stage = document.getElementById("wstage");
      if (!stage) return;
      if (stage.classList.contains("controls-hidden")) {
        wRevealControls();
      } else {
        wHideControls();
      }
    });
    const bar = document.getElementById("wbar");
    bar.addEventListener("pointerdown", () => { clearTimeout(wControlsTimer); });
    bar.addEventListener("pointerup", () => { if (wFull) wRevealControls(); });
    bar.addEventListener("focusin", () => { clearTimeout(wControlsTimer); });
    bar.addEventListener("focusout", () => { if (wFull) wRevealControls(); });
  }
  wMore();
  wChatSetup();
  wHoldDraw();
  if (v.ends) document.getElementById("wends").textContent = "Ends " + new Date(v.ends).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  else document.getElementById("wends").hidden = true;
}

function wPeople() {
  const el = document.getElementById("wpeople");
  if (!el) return;
  const canHand = wv.is_host || wv.staff;
  const mark = { loading: icon("clock"), behind: icon("ff") };
  el.innerHTML = wv.members.map((m) => {
    const tag = m.state === "behind" ? ` title="${m.behind}s behind"` : m.state === "loading" ? ` title="Loading"` : "";
    const dot = mark[m.state] ? `<i class="wdot ${m.state}">${mark[m.state]}</i>` : "";
    const hand = canHand && !m.host ? ` data-host="${esc(m.id)}"` : "";
    return `<button class="pill wperson ${m.state || ""}"${tag}${hand}${hand ? "" : " disabled"}>${m.host ? icon("crown") + " " : ""}${esc(m.name)}${dot}</button>`;
  }).join("");
  el.querySelectorAll("[data-host]").forEach((b) => (b.onclick = () => {
    if (!b.classList.contains("sure")) {
      b.classList.add("sure");
      b.title = "Make them the host? Tap again";
      return;
    }
    api("watch/host", { room: wRoom, user: b.dataset.host }).then((nv) => wSet(nv)).catch(showError);
  }));
  document.getElementById("wcount").textContent = `${wv.members.length}/${wv.max}`;
}

// wVote draws the skip-vote banner over the video: a button to call one
// during an opening or ending, the tally once it is running.
function wVote() {
  const el = document.getElementById("wvote");
  if (!el) return;
  const v = wv;
  if (v.phase !== "play" || (!v.can_vote && !v.vote)) {
    el.hidden = true;
    return;
  }
  el.hidden = false;
  const kind = (v.vote && v.vote.kind) || "op";
  const label = kind === "ed" ? "ending" : "opening";
  if (v.vote) {
    el.innerHTML = `<span>${icon("skip")} Skip the ${label}? <b>${v.vote.votes}/${v.vote.need}</b></span>
      <button class="btn" id="wvotebtn"${v.vote.mine ? " disabled" : ""}>${v.vote.mine ? "Voted" : "Skip"}</button>`;
  } else {
    el.innerHTML = `<span>${icon("skip")} ${label.charAt(0).toUpperCase() + label.slice(1)} playing</span>
      <button class="btn" id="wvotebtn">Vote to skip</button>`;
  }
  const b = document.getElementById("wvotebtn");
  if (b) b.onclick = () => { b.disabled = true; api("watch/vote", { room: wRoom }).then((nv) => wSet(nv)).catch((e) => { b.disabled = false; showError(e); }); };
}

function wSyncLine() {
  const el = document.getElementById("wsync");
  if (!el) return;
  const linked = wv.linked || [];
  if (!linked.length) {
    el.innerHTML = "Link /link-anilist or /link-mal and episodes you watch here count on your list.";
    return;
  }
  el.innerHTML = `<label class="wsw"><input type="checkbox" id="wsyncbox" ${wv.sync ? "checked" : ""}> Move my ${linked.join(" and ")} on after each episode I watch and save where I stop</label>`;
  document.getElementById("wsyncbox").onchange = (e) =>
    api("watch/sync", { room: wRoom, on: e.target.checked })
      .then((r) => (wv.sync = r.sync))
      .catch(showError);
}

function wMore() {
  const el = document.getElementById("wmore");
  if (!el) return;
  const v = wv;
  let html = "";
  if (v.is_host && v.phase === "loading") html += `<button class="btn alt" id="wcancel">${icon("x")} Cancel loading</button>`;
  if (v.is_host && v.show && v.phase !== "loading") html += `<button class="btn alt" id="wchange">${icon("shuffle")} Change show</button>`;
  if (v.staff && !v.is_host) html += `<button class="btn alt" id="wtake">${icon("shield")} Take control</button>`;
  if (v.phase === "loading") html += `<button class="btn alt" id="wstopme">${icon("stop")} Stop watching</button>`;
  if (v.is_host || v.staff) html += `<button class="btn alt" id="winvite">${icon("users")} Invite someone</button>`;
  if (v.is_host || v.staff) html += `<button class="btn alt" id="wend">${icon("x")} End room</button>`;
  el.innerHTML = html;
  const on = (id, fn) => document.getElementById(id) && (document.getElementById(id).onclick = fn);
  on("wcancel", () => api("watch/ctl", { room: wRoom, act: "cancel" }).then((nv) => ((wvKey = ""), wSet(nv))).catch(showError));
  on("wchange", () => {
    wChanging = true;
    wBox(`<div id="wpick"></div>`);
    wDestroy();
    wPicker(true);
  });
  on("wstopme", wStopWatching);
  on("winvite", wInvite);
  on("wtake", () => api("watch/take", { room: wRoom }).then((nv) => ((wvKey = ""), (wvMedia = ""), wSet(nv))).catch(showError));
  on("wend", (e) => {
    const b = e.currentTarget;
    if (!b.classList.contains("sure")) {
      b.classList.add("sure");
      b.lastChild.textContent = " Sure? Tap again";
      return;
    }
    api("watch/end", { room: wRoom }).catch(showError);
  });
}

// wStopWatching is quitting the player: this member leaves the room, which
// keeps playing for everyone else, and their list notes get where they
// stopped so Otakase can pick it up from there.
async function wStopWatching() {
  const v = wv;
  if (wFull) wFullscreen(false);
  wStop();
  wv = null;
  wvKey = wvMedia = "";
  wBox(loading("Stopping…"));
  const r = await api("watch/leave", { room: wRoom }).catch(() => ({}));
  if (where !== "watch") return;
  const synced = v && v.sync && (v.linked || []).length;
  const at = r.done ? `You finished episode ${r.ep}.` : r.ep ? `You stopped at <b>${wFmt(r.secs)}</b> of episode ${r.ep}.` : "You stopped watching.";
  const note = !(r.ep && synced)
    ? "The room keeps playing for everyone else."
    : r.done
      ? `Your ${v.linked.join(" and ")} moved on, so Otakase starts the next episode.`
      : `Saving where you stopped to your ${v.linked.join(" and ")}, so Otakase can resume from there.`;
  const ask = r.ask && synced
    ? `<div class="wask" id="wcount"><span>You joined partway. Count episode ${r.ep} as watched on your ${esc(v.linked.join(" and "))}?</span>
      <button class="btn" data-count="1">Yes</button><button class="btn alt" data-count="0">No</button></div>`
    : "";
  wBox(`<p class="lead">${at}</p><p class="note">${note}</p>${ask}
    <button class="btn" id="wagain">${icon("play")} Watch again</button><div class="err" id="err"></div>`);
  const c = document.getElementById("wcount");
  if (c) {
    c.querySelectorAll("[data-count]").forEach(
      (x) =>
        (x.onclick = () => {
          c.querySelectorAll("button").forEach((y) => (y.disabled = true));
          api("watch/count", { room, ok: x.dataset.count === "1" })
            .then((res) => (c.innerHTML = `<span>${res.counted ? `Episode ${r.ep} counted.` : "Left as it was."}</span>`))
            .catch(showError);
        })
    );
  }
  const b = document.getElementById("wagain");
  b.onclick = () => ((b.disabled = true), watchView().catch((e) => ((b.disabled = false), showError(e))));
}

// wInvite opens a small finder to message someone into the room.
function wInvite() {
  const host = document.getElementById("wmore");
  if (!host || document.getElementById("winvitebox")) return;
  const box = document.createElement("div");
  box.className = "winvite";
  box.id = "winvitebox";
  box.innerHTML = `<div class="winvitehead"><b>${icon("users")} Invite someone</b><button class="btn alt" id="winviteclose" aria-label="Close">${icon("x")}</button></div>
    <input id="winviteq" autocomplete="off" placeholder="Search people…" aria-label="Search people">
    <div class="winvitelist" id="winvitelist"><p class="note">They get a direct message with a link to this call.</p></div>`;
  host.after(box);
  const q = document.getElementById("winviteq");
  const list = document.getElementById("winvitelist");
  document.getElementById("winviteclose").onclick = () => box.remove();
  let t = 0;
  const load = async () => {
    let people;
    try {
      people = await api(`watch/people?room=${encodeURIComponent(wRoom)}&q=${encodeURIComponent(q.value.trim())}`);
    } catch (e) {
      return showError(e);
    }
    if (!document.getElementById("winviteq")) return;
    list.innerHTML = people.length
      ? people.map((p) => `<button class="wshowcard" data-id="${esc(p.id)}"><i></i><span><b>${esc(p.name)}</b></span></button>`).join("")
      : `<p class="note">Nobody found.</p>`;
    list.querySelectorAll("[data-id]").forEach((b) => (b.onclick = () => {
      b.disabled = true;
      api("watch/invite", { room: wRoom, user: b.dataset.id })
        .then((nv) => { b.querySelector("b").textContent += " · invited"; wSet(nv); })
        .catch((e) => { b.disabled = false; showError(e); });
    }));
  };
  q.oninput = () => { clearTimeout(t); t = setTimeout(load, 250); };
  q.focus();
  load();
}

// ── Requests ──

function wAsks() {
  const asks = wv.asks || [];
  const el = document.getElementById("wasks");
  if (el) {
    el.innerHTML = wv.is_host
      ? asks
          .map(
            (a) => `<div class="wask"><span><b>${esc(a.name)}</b> asks to ${esc(wAskText(a))}</span>
        <button class="btn" data-ok="${a.id}">Yes</button><button class="btn alt" data-no="${a.id}">No</button></div>`
          )
          .join("")
      : asks.map((a) => `<div class="wask mine"><span>Asked the host to ${esc(wAskText(a))}…</span></div>`).join("");
    el.innerHTML += wCountAsk("wask");
    wAnswerButtons(el);
    wCountButtons(el);
  }
  // A new request pings the host, who may be in theater mode.
  const newest = Math.max(0, ...asks.map((a) => a.id));
  if (wv.is_host && newest > wAskSeen) sfx("tick");
  wAskSeen = Math.max(wAskSeen, newest);
  wAskPops();
}

// wAskPops shows the requests over the video in theater mode, where the list
// under the player is out of sight. The host's stay up with Yes and No until
// answered or until the request lapses; a member sees the one they sent.
// They only redraw when the requests change, so the countdowns run smoothly.
function wAskPops() {
  const el = document.getElementById("wreq");
  if (!el) return;
  const asks = wv.asks || [];
  const count = wCountAsk("wreqpop");
  const key = (wv.is_host ? "h" : "m") + asks.map((a) => [a.id, a.kind, a.by, a.pos].join(":")).join(",") + (count ? "c" + wv.count_ask : "");
  if (key === wAskKey && el.childElementCount === asks.length + (count ? 1 : 0)) return;
  wAskKey = key;
  el.innerHTML = count + asks
    .map((a) => {
      const left = Math.max(0, a.left_ms || 0) / 1000;
      const bar = `<i class="wreqtime" style="animation-duration:${left.toFixed(1)}s;--from:${Math.min(1, left / 30).toFixed(3)}"></i>`;
      return wv.is_host
        ? `<div class="wreqpop"><span><b>${esc(a.name)}</b> asks to ${esc(wAskText(a))}</span>
          <span class="wreqbtns"><button class="btn" data-ok="${a.id}">Yes</button><button class="btn alt" data-no="${a.id}">No</button></span>${bar}</div>`
        : `<div class="wreqpop mine"><span>Asked the host to ${esc(wAskText(a))}…</span>${bar}</div>`;
    })
    .join("");
  wAnswerButtons(el);
  wCountButtons(el);
}

// wCountAsk is the question for a member who came in late to the episode
// and stayed to near its end: the room only counts it for them if they say
// so. cls is the box it sits in.
function wCountAsk(cls) {
  if (!wv.count_ask || !wv.sync || !(wv.linked || []).length) return "";
  return `<div class="${cls}"><span>You joined partway. Count episode ${wv.count_ask} as watched on your ${esc(wv.linked.join(" and "))}?</span>
    <span class="wreqbtns"><button class="btn" data-count="1">Yes</button><button class="btn alt" data-count="0">No</button></span></div>`;
}

function wCountButtons(el) {
  el.querySelectorAll("[data-count]").forEach(
    (b) =>
      (b.onclick = (e) => {
        e.stopPropagation();
        el.querySelectorAll("[data-count]").forEach((x) => (x.disabled = true));
        api("watch/count", { room: wRoom, ok: b.dataset.count === "1" })
          .then((nv) => wSet(nv, true))
          .catch(showError);
      })
  );
}

// wAnswerButtons wires the host's Yes and No buttons in a list of requests.
function wAnswerButtons(el) {
  el.querySelectorAll("[data-ok],[data-no]").forEach(
    (b) =>
      (b.onclick = (e) => {
        e.stopPropagation();
        el.querySelectorAll(`[data-ok="${b.dataset.ok || b.dataset.no}"],[data-no="${b.dataset.ok || b.dataset.no}"]`).forEach((x) => (x.disabled = true));
        wHostAct = Date.now();
        api("watch/answer", { room: wRoom, id: +(b.dataset.ok || b.dataset.no), ok: !!b.dataset.ok })
          .then((nv) => wSet(nv, true))
          .catch(showError);
      })
  );
}

// ── Picking a show ──

function wPicker(changing) {
  const box = document.getElementById("wpick");
  box.innerHTML = `<p class="lead">Pick something to watch.</p>
    ${searchBox("Search AniList…")}<div class="wresults" id="wres"></div>
    ${changing ? `<button class="btn alt" id="wcancel">Back to the episode</button>` : ""}`;
  document.getElementById("list").remove();
  const q = document.getElementById("q");
  let t = 0;
  q.oninput = () => {
    clearTimeout(t);
    const text = q.value.trim();
    if (text.length < 2) return wListsDraw();
    t = setTimeout(async () => {
      try {
        wSearch = await api(`watch/search?room=${encodeURIComponent(wRoom)}&q=${encodeURIComponent(text)}`);
      } catch (e) {
        return showError(e);
      }
      if (q.value.trim() !== text) return;
      const res = document.getElementById("wres");
      res.innerHTML = wSearch.length ? wSearch.map((s, i) => wShowCard(s, i, wShowFacts(s))).join("") : `<p class="note">Nothing found.</p>`;
      res.querySelectorAll("[data-i]").forEach((b) => (b.onclick = () => wEpisode(wSearch[+b.dataset.i])));
    }, 300);
  };
  q.focus();
  const c = document.getElementById("wcancel");
  if (c) c.onclick = () => ((wChanging = false), (wvKey = ""), (wvMedia = ""), wSet(wv));
  wLists = null;
  wListsDraw();
  api(`watch/lists?room=${encodeURIComponent(wRoom)}`)
    .then((lists) => {
      wLists = lists;
      if (document.getElementById("q") === q && q.value.trim().length < 2) wListsDraw();
    })
    .catch(() => {
      wLists = { anilist: false, mal: false, continue: [], note: "Couldn't load your lists." };
      if (document.getElementById("q") === q && q.value.trim().length < 2) wListsDraw();
    });
}

function wShowFacts(s) {
  return [s.year, s.format && s.format.replace("_", " ").toLowerCase(), s.aired ? s.aired + " out" : s.episodes ? s.episodes + " eps" : ""].filter(Boolean).join(" · ");
}

function wShowCard(s, i, facts) {
  return `<button class="wshowcard" data-i="${i}">${s.cover ? `<img src="${esc(s.cover)}" alt="" loading="lazy">` : "<i></i>"}
    <span><b>${esc(s.title)}</b><small>${esc(facts)}</small></span></button>`;
}

// wListsDraw shows the host's Continue list under the search while it is
// empty: each show starts at the next episode, from a saved position when
// there is one; caught-up shows come last, at the latest episode.
function wListsDraw() {
  const res = document.getElementById("wres");
  if (!res) return;
  if (!wLists) {
    res.innerHTML = `<p class="note">Loading your lists…</p>`;
    return;
  }
  if (!wLists.anilist && !wLists.mal) {
    res.innerHTML = `<p class="note">${esc(wLists.note || "Link AniList with /link-anilist or MyAnimeList with /link-mal to pick from your own lists here.")}</p>`;
    return;
  }
  const list = wLists.continue;
  res.innerHTML = (list.length
    ? list.map((s, i) => wShowCard(s, i, s.caught_up
      ? `Caught up · episode ${s.next} is the latest`
      : `Episode ${s.next}${s.aired || s.episodes ? " of " + (s.aired || s.episodes) : ""}${s.resume ? " · resume at " + wFmt(s.resume) : ""}`)).join("")
    : `<p class="note">Nothing to continue: you're not watching anything on your lists.</p>`) +
    (wLists.note ? `<p class="note">${esc(wLists.note)}</p>` : "");
  res.querySelectorAll("[data-i]").forEach((b) => (b.onclick = () => wEpisode(list[+b.dataset.i], list[+b.dataset.i])));
}

function wPickerHeading(s) {
  return `<div class="wpickhead">${s.cover ? `<img src="${esc(s.cover)}" alt="">` : ""}
    <div><span class="wpickey">EPISODE SELECTION</span><h3>${esc(s.title)}</h3></div></div>`;
}

// wEpisode is the episode grid for show s; item is its entry on the host's
// list, which picks the next episode and may carry a position to resume.
function wEpisode(s, item) {
  const previews = new Map();
  for (const item of s.previews || []) {
    const title = item.title || "";
    const match = title.match(/\b(?:episode|ep\.?)[\s:]*([0-9]+)\b/i) || title.match(/^([0-9]+)\s*[-:.]/);
    if (match && !previews.has(Number(match[1]))) previews.set(Number(match[1]), item);
  }
  const last = Math.max(1, s.aired || s.episodes || 1);
  const next = item && item.next ? Math.min(item.next, last) : wv.show && wv.show.id === s.id ? Math.min(wv.ep + 1, last) : 1;
  const resumeEp = item && item.resume ? item.next : 0;
  let selected = next;
  let page = Math.floor((selected - 1) / 24);
  const res = document.getElementById("wres");
  if (!res) return;
  const draw = () => {
    const start = page * 24 + 1;
    const end = Math.min(last, start + 23);
    res.innerHTML = `<div class="wpickflow">
      ${wPickerHeading(s)}
      <div class="weptop"><div><h4>Select an episode</h4><p class="note">${last} episode${last === 1 ? "" : "s"} available to select</p></div>
        <span class="pill">Audio in player settings</span></div>
      <div class="wepgrid">${Array.from({ length: end - start + 1 }, (_, i) => {
        const ep = start + i;
        const preview = previews.get(ep);
        const picture = preview && preview.thumbnail ? `<img loading="lazy" src="${esc(preview.thumbnail)}" alt="">` : "";
        const caption = preview && preview.title && preview.title.trim().toLowerCase() !== `episode ${ep}` && preview.title.trim().toLowerCase() !== `ep ${ep}` ? esc(preview.title) : "";
        return `<button class="wepcard${ep === selected ? " chosen" : ""}${picture ? "" : " noimage"}" data-ep="${ep}" aria-pressed="${ep === selected}">
          <span class="wepvisual">${picture}</span><span class="wepdetails"><span class="wepnumber">EP ${String(ep).padStart(2, "0")}</span>${caption ? `<strong>${caption}</strong>` : ""}</span>
          ${ep === selected ? `<span class="wepcheck">${icon("check")}</span>` : ""}
        </button>`;
      }).join("")}</div>
      ${last > 24 ? `<div class="wepnav"><button class="btn alt" id="wprev" ${page === 0 ? "disabled" : ""}>Previous</button>
        <span class="note">Episodes ${start}–${end} of ${last}</span>
        <button class="btn alt" id="wnext" ${end === last ? "disabled" : ""}>Next</button></div>` : ""}
      <div class="wepfooter"><button class="btn alt" id="wback">${item ? "Back to your list" : "Back to results"}</button>
        ${selected === resumeEp ? `<button class="btn alt" id="wfromstart">From the start</button>
        <button class="btn" id="wgo">${icon("play")} Resume episode ${selected} at ${wFmt(item.resume)}</button>`
        : `<button class="btn" id="wgo">${icon("play")} Play episode ${selected}</button>`}</div>
    </div>`;
    res.querySelectorAll(".wepvisual img").forEach((img) => {
      img.onerror = () => {
        const card = img.closest(".wepcard");
        if (card) card.classList.add("noimage");
      };
    });
    res.querySelectorAll("[data-ep]").forEach((button) => {
      button.onclick = () => {
        selected = Number(button.dataset.ep);
        draw();
      };
    });
    document.getElementById("wback").onclick = () => document.getElementById("q").oninput();
    if (last > 24) {
      document.getElementById("wprev").onclick = () => { if (page > 0) { page--; draw(); } };
      document.getElementById("wnext").onclick = () => { if ((page + 1) * 24 < last) { page++; draw(); } };
    }
    const play = (button, pos) => {
      button.disabled = true;
      wChanging = false;
      wvKey = "";
      api("watch/pick", { room: wRoom, show: s.id, ep: selected, pos, dub: /^(en|eng)(-|$)/.test(wAudioPick) })
        .then((nv) => wSet(nv))
        .catch((err) => { button.disabled = false; showError(err); });
    };
    const go = document.getElementById("wgo");
    go.onclick = () => play(go, selected === resumeEp ? item.resume : 0);
    const fromStart = document.getElementById("wfromstart");
    if (fromStart) fromStart.onclick = () => play(fromStart, 0);
  };
  draw();
  fetch(`api/watch/episodes?id=${encodeURIComponent(s.id)}&room=${encodeURIComponent(wRoom)}`, {
    headers: { Authorization: "Bearer " + session },
  }).then((r) => r.ok ? r.json() : null).then((episodes) => {
    if (!episodes || document.getElementById("wres") !== res) return;
    for (const [number, preview] of Object.entries(episodes)) {
      const ep = Number(number);
      if (!Number.isInteger(ep) || ep < 1 || ep > last) continue;
      const old = previews.get(ep) || {};
      previews.set(ep, { title: preview.title || old.title, thumbnail: preview.thumbnail || old.thumbnail });
    }
    if (res.querySelector(".wpickflow")) draw();
  }).catch(() => {});
}

// ── The player ──

function wDestroy() {
  clearInterval(wFollow);
  clearInterval(wBeat);
  if (wHls) wHls.destroy();
  wHls = null;
  const vid = document.getElementById("wvid");
  if (vid) {
    vid.pause();
    vid.removeAttribute("src");
    vid.load();
  }
  wvMedia = "";
}

function wStop() {
  wFull = false;
  document.body.classList.remove("wfull");
  const detached = document.body.querySelector(":scope > #wstage");
  if (detached) detached.remove();
  const marker = document.getElementById("wstage-placeholder");
  if (marker) marker.remove();
  wvLoop++;
  wDestroy();
}

// hlsReady loads hls.js the first time a room needs it.
let hlsLoading = null;
function hlsReady() {
  if (window.Hls) return Promise.resolve();
  if (!hlsLoading)
    hlsLoading = new Promise((ok, fail) => {
      const s = document.createElement("script");
      s.src = "hls.js";
      s.onload = ok;
      s.onerror = () => fail(new Error("Couldn't load the player."));
      document.head.appendChild(s);
    });
  return hlsLoading;
}

// Normalize ISO 639-2 tags exposed by HLS, torrents and separate releases.
function wNormAudio(code) {
  return String(code || "").toLowerCase().replace(/^jpn$/, "ja")
    .replace(/^eng$/, "en").replace(/^deu$/, "de").replace(/^ger$/, "de")
    .replace(/^spa$/, "es").replace(/^fra$/, "fr").replace(/^fre$/, "fr")
    .replace(/^hin$/, "hi").replace(/^ita$/, "it").replace(/^por$/, "pt")
    .replace(/^tam$/, "ta").replace(/^tel$/, "te").replace(/^kor$/, "ko")
    .replace(/^rus$/, "ru").replace(/^zho$/, "zh").replace(/^chi$/, "zh");
}

// Manual language selection wins. Automatic follows this member's preferred
// language, backup language, and finally the original stream default.
function wAudioCandidates() {
  if (wAudioPick !== "auto") return [wNormAudio(wAudioPick)];
  const pref = (wv && wv.preferences) || {};
  const codes = [wNormAudio(pref.audio), wNormAudio(pref.backup_audio)];
  return codes.filter((code, i) => code && code !== "auto" && codes.indexOf(code) === i);
}

function wEffectiveMedia() {
  if (!wv) return "";
  const variants = wv.audio_media || {};
  const embedded = wHls && wvMedia === wDirectURL(wv.media) ? wHls.audioTracks || [] : [];
  for (const pick of wAudioCandidates()) {
    // Each HLS rendition has a relay URL even on native players.
    if (variants[pick]) return wDirectURL(variants[pick]);
    if (variants[pick.split("-")[0]]) return wDirectURL(variants[pick.split("-")[0]]);
    if (/^(en|eng)(-|$)/.test(pick) && variants.dub) return wDirectURL(variants.dub);
    if (/^(ja|jpn)(-|$)/.test(pick) && variants.sub) return wDirectURL(variants.sub);
    if (embedded.some((track) => wNormAudio(track.lang) === pick)) return wDirectURL(wv.media);
  }
  return wDirectURL(wv.media);
}

// wDirectURL is a playlist address as this player loads it.
function wDirectURL(u) {
  if (!wDirect || !u || !/\.m3u8$/i.test(u)) return u;
  return u + "?direct=1";
}

// wEdgeFailed moves this player off the edge cache after a segment failed
// to load through it, and reloads. False when it already was off.
function wEdgeFailed(url) {
  if (wDirect || !wv || !wv.cdn || (url && !url.includes(wv.cdn))) return false;
  wDirect = true;
  wLastErr = "edge cache failed, loading from the bot";
  wLoad();
  return true;
}

async function wLoad() {
  const vid = document.getElementById("wvid");
  if (!vid) return;
  wDestroy();
  wvMedia = wEffectiveMedia();
  if (wNoNative !== wvMedia) wLastErr = "";
  wFails = 0;
  const at = wTarget();
  // The video element survives episode and language switches; drop the
  // previous source's tracks so they don't pile up in the picker.
  vid.querySelectorAll("track").forEach((t) => t.remove());
  for (const s of wv.subs || []) {
    const tr = document.createElement("track");
    tr.kind = "subtitles";
    tr.label = s.label;
    if (s.lang) tr.srclang = s.lang;
    tr.src = s.url;
    vid.appendChild(tr);
    wSubsFollow(tr, s.url);
  }
  vid.onloadedmetadata = () => {
    wSubs();
    if (vid.readyState > 0 && Math.abs(vid.currentTime - at) > 2) vid.currentTime = at;
  };
  vid.onwaiting = () => { document.getElementById("wspin").hidden = false; if (wv.is_host) wBeatNow(); };
  vid.onplaying = () => { document.getElementById("wspin").hidden = true; if (wv.is_host) wBeatNow(); if (wFull && !wControlsHidden()) wRevealControls(); };
  vid.oncanplay = () => {
    document.getElementById("wspin").hidden = true;
    if (wv.hold) wReadyCheck(vid);
  };
  vid.onseeked = () => { if (wv.hold) wReadyCheck(vid); };
  vid.onerror = () => {
    const e = vid.error;
    const why = (wHls ? "hls.js" : "native") + " video error " + (e ? e.code + " " + (e.message || "") : "");
    // A native player doesn't say which file failed; try the bot first.
    if (!wHls && wEdgeFailed("")) return;
    // Hls.js is only loaded once the built-in player has failed.
    if (!wHls && wNoNative !== wvMedia && /\.m3u8(?:$|\?)/i.test(wvMedia)) {
      wNoNative = wvMedia;
      wLastErr = why;
      return wLoad();
    }
    wBroken(why);
  };
  vid.onended = () => {
    if (wv.is_host && wv.ep < wv.last_ep) wCtl("next");
  };
  vid.ontimeupdate = wTime;
  if (/\.m3u8(?:$|\?)/i.test(wvMedia)) {
    // Native HLS decodes in Discord environments where MSE playback may
    // expose tracks but fail to output audio. The relay provides a separate
    // master for each available language, so native playback can switch too.
    if (vid.canPlayType("application/vnd.apple.mpegurl") && wNoNative !== wvMedia) {
      vid.src = wvMedia;
    } else {
      try {
        await hlsReady();
      } catch (e) {
        return showError(e);
      }
      if (wvMedia !== wEffectiveMedia()) return;
      if (window.Hls && Hls.isSupported()) {
        // Segments the bot encodes as they are asked for can take a while on
        // a small host; wait up to a minute for each before counting it lost.
        const fragLoadPolicy = { default: { maxTimeToFirstByteMs: 60000, maxLoadTimeMs: 120000,
          timeoutRetry: { maxNumRetry: 3, retryDelayMs: 0, maxRetryDelayMs: 0 },
          errorRetry: { maxNumRetry: 6, retryDelayMs: 1000, maxRetryDelayMs: 8000 } } };
        const hls = new Hls({ startPosition: at, maxBufferLength: 40, backBufferLength: 60, enableWorker: false, renderTextTracksNatively: true, fragLoadPolicy });
        wHls = hls;
        hls.on(Hls.Events.ERROR, (_, d) => {
          if (wHls !== hls) return;
          wLastErr = "hls.js " + d.type + " " + d.details + (d.response && d.response.code ? " HTTP " + d.response.code : "")
            + (d.reason ? " " + d.reason : "") + (d.error && d.error.message ? " " + d.error.message : "");
          if (!d.fatal) return;
          if (d.type === Hls.ErrorTypes.NETWORK_ERROR && d.frag && wEdgeFailed(d.frag.url)) return;
          if (d.type === Hls.ErrorTypes.MEDIA_ERROR && wFails < 2) {
            wFails++;
            return hls.recoverMediaError();
          }
          if (++wFails > 4) return wBroken(wLastErr);
          setTimeout(() => { if (wHls === hls) hls.startLoad(); }, 1500);
        });
        hls.on(Hls.Events.MANIFEST_PARSED, () => {
          if (wQuality >= 0 && wQuality < hls.levels.length) hls.currentLevel = wQuality;
          wQualityDraw();
        });
        hls.on(Hls.Events.LEVEL_SWITCHED, wQualityDraw);
        hls.on(Hls.Events.SUBTITLE_TRACKS_UPDATED, wSubs);
        hls.on(Hls.Events.AUDIO_TRACKS_UPDATED, wAudioTracks);
        hls.on(Hls.Events.AUDIO_TRACK_SWITCHED, wSubs);
        hls.loadSource(wvMedia);
        hls.attachMedia(vid);
      } else {
        return showError(new Error("This Discord can't play the stream."));
      }
    }
  } else {
    vid.src = wvMedia;
  }
  wApply(true);
  wFollow = setInterval(() => {
    wApply(false);
    if (wv && wv.hold) wHoldDraw();
  }, 1000);
  if (wv.is_host) wBeat = setInterval(wBeatNow, 4000);
}

// wBroken moves the host to the episode's next source; others just wait.
// why goes to the bot's log, with the last error before it.
function wBroken(why) {
  if (!wv || !wv.is_host) return;
  if (wLastErr && wLastErr !== why) why = (why || "") + " (before: " + wLastErr + ")";
  const vid = document.getElementById("wvid");
  wHostAct = Date.now();
  api("watch/ctl", { room: wRoom, act: "broken", pos: vid ? vid.currentTime : -1, why: String(why || "").slice(0, 300) })
    .then((nv) => wSet(nv, true))
    .catch(showError);
}

// wTarget is where the host is now, as far as this page knows.
function wTarget() {
  if (!wv) return 0;
  let p = wv.pos;
  if (wv.playing) p += (Date.now() - wvAt) / 1000;
  return wv.dur ? Math.min(p, wv.dur) : p;
}

// wApply keeps the player on the host's clock. The host's own page only
// follows when told to (a request it accepted), never its own echo.
function wApply(force) {
  const vid = document.getElementById("wvid");
  if (!vid || !wv || wv.phase !== "play") return;
  if (wv.hold) wReadyCheck(vid);
  if (wv.is_host && !force) return;
  const t = wTarget();
  if (wv.is_host) {
    if (vid.readyState > 0 && Math.abs(vid.currentTime - t) > 0.5) vid.currentTime = t;
  } else wFollowHost(vid, t, force);
  if (wv.playing && vid.paused) {
    vid.play().then(
      () => wTap(false),
      () => wTap(true)
    );
  } else if (!wv.playing && !vid.paused) vid.pause();
}

// wFollowHost keeps a viewer on the host's clock. Small drift is played
// away a little faster; a jump of the room's spot is followed at once; a
// player that fell behind on its own (it was buffering) gets a catch-up
// button rather than being yanked forward into another wait.
function wFollowHost(vid, t, force) {
  if (vid.readyState === 0) return;
  const off = t - vid.currentTime; // seconds behind the host
  const stalled = vid.seeking || vid.readyState < 3;
  // While the room waits for everyone, a player counts as ready within 1.5s
  // of the held spot, so it has to get closer than that.
  if (off < -2 || ((force || wJump || !wv.playing) && Math.abs(off) > 2) || (wv.hold && Math.abs(off) > 1)) {
    vid.currentTime = t;
    vid.playbackRate = 1;
    wJump = false;
    return wCatchUp(0);
  }
  wJump = false;
  if (off > 4) {
    vid.playbackRate = 1;
    return wCatchUp(off);
  }
  wCatchUp(0);
  vid.playbackRate = off > 0.6 && !stalled && wv.playing ? 1.15 : 1;
}

function wCatchUp(behind) {
  const b = document.getElementById("wcatch");
  if (!b) return;
  b.hidden = !behind;
  if (!behind) return;
  b.innerHTML = `${icon("ff")} ${Math.round(behind)}s behind · Catch up`;
  b.onclick = () => {
    const vid = document.getElementById("wvid");
    if (vid) vid.currentTime = wTarget();
    wCatchUp(0);
  };
}

// wReadyCheck tells the room once this player has the held spot loaded.
function wReadyCheck(vid) {
  if (!wv || !wv.hold || wv.hold_ready || wReadySent === wv.hold) return;
  // The host's own player isn't moved by the room's clock; while the room
  // waits it belongs at the held spot too, or it never counts as ready.
  // Not right after the host's own seek, which the room hasn't answered yet.
  if (wv.is_host && Date.now() - wHostAct > 2000 && vid.readyState > 0 && !vid.seeking && Math.abs(vid.currentTime - wv.pos) > 1) {
    vid.currentTime = wv.pos;
    return;
  }
  if (vid.readyState < 3 || vid.seeking || Math.abs(vid.currentTime - wv.pos) > 1.5) return;
  const id = wv.hold;
  wReadySent = id;
  api("watch/ready", { room: wRoom, id })
    .then((nv) => wSet(nv))
    .catch(() => { if (wReadySent === id) wReadySent = 0; });
}

// wHoldDraw shows who the room is waiting for while a ready check runs.
function wHoldDraw() {
  const el = document.getElementById("whold");
  if (!el) return;
  if (!wv || !wv.hold) {
    el.hidden = true;
    return;
  }
  const waiting = wv.hold_waiting || [];
  const left = wv.hold_ends ? Math.max(0, Math.ceil((wv.hold_ends - Date.now()) / 1000)) : 0;
  el.hidden = false;
  el.innerHTML = `<b>Waiting for everyone to load</b>
    <small>${waiting.length ? "Still loading: " + waiting.map(esc).join(", ") : "Starting…"}${left ? ` · plays in ${left}s at most` : ""}</small>
    ${wv.is_host ? `<button class="btn" id="wholdgo">${icon("play")} Start now</button>` : ""}`;
  const go = document.getElementById("wholdgo");
  if (go) go.onclick = () => wCtl("play");
}

function wTap(show) {
  wNeedTap = show;
  const b = document.getElementById("wtap");
  if (!b) return;
  b.hidden = !show;
  if (show) document.getElementById("wspin").hidden = true;
  b.onclick = () => {
    wTap(false);
    wApply(true);
  };
}

function wBeatNow() {
  const vid = document.getElementById("wvid");
  if (!vid || !wv || !wv.is_host || vid.readyState === 0) return;
  fetch("api/watch/beat", {
    method: "POST",
    headers: { Authorization: "Bearer " + session, "Content-Type": "application/json" },
    // A player loading after a seek or skip still means to play: reporting it
    // as paused would pause the room and leave it paused once it loads.
    body: JSON.stringify({
      room: wRoom,
      pos: vid.currentTime,
      playing: !vid.paused && !vid.ended,
      buffering: vid.seeking || vid.readyState < 3,
      dur: isFinite(vid.duration) ? vid.duration : 0,
    }),
  }).catch(() => {});
}

function wCtl(act, pos) {
  wHostAct = Date.now();
  const vid = document.getElementById("wvid");
  if (pos === undefined) pos = vid ? vid.currentTime : -1;
  return api("watch/ctl", { room: wRoom, act, pos })
    .then((nv) => wSet(nv, true))
    .catch(showError);
}

function wAsk(kind, extra) {
  api("watch/ask", { room: wRoom, kind, ...extra })
    .then((nv) => wSet(nv))
    .catch(showError);
}

// wAskBy asks the host to jump step seconds ahead (back when below 0).
// Presses close together add up into one request, so +10s five times asks
// for +50s, up to a minute; a jump already waiting for the host is changed, not repeated.
let wAskDraft = null;
let wAskDraftTimer = 0;
function wAskBy(step) {
  const now = Date.now();
  const mine = (wv.asks || []).find((a) => a.kind === "forward");
  let by = wAskDraft && now - wAskDraft.at < 4000 ? wAskDraft.by : mine ? mine.by || 10 : 0;
  by = Math.max(-60, Math.min(60, by + step)); // a minute either way; the bar goes further
  wAskDraft = { by, at: now };
  clearTimeout(wAskDraftTimer);
  if (!by) {
    wOsd("No jump");
    return;
  }
  wOsd(by > 0 ? `Ask +${wSecs(by)}` : `Ask -${wSecs(-by)}`);
  wAskDraftTimer = setTimeout(() => wAsk("forward", { by }), 450);
}

// wAskJump asks, after a yes from this member, for the host to jump to pos:
// a tap on the bar or a number key is easy to do by accident.
let wConfirmTimer = 0;
function wAskJump(pos) {
  const el = document.getElementById("wconfirm");
  if (!el || !wv) return;
  const dur = wv.dur || 0;
  pos = Math.max(0, Math.round(dur ? Math.min(pos, Math.max(0, dur - 1)) : pos));
  const off = pos - wTarget();
  const rel = Math.abs(off) < 1 ? "" : ` (${off > 0 ? "+" : "-"}${wSecs(Math.abs(off))})`;
  el.innerHTML = `<span>Ask the host to jump to <b>${wFmt(pos)}</b>${rel}?</span>
    <span class="wreqbtns"><button class="btn" id="wconfirmyes">Ask</button><button class="btn alt" id="wconfirmno">Cancel</button></span>`;
  el.hidden = false;
  const close = () => {
    clearTimeout(wConfirmTimer);
    el.hidden = true;
  };
  document.getElementById("wconfirmyes").onclick = (e) => {
    e.stopPropagation();
    close();
    wAsk("seek", { pos });
    wOsd("Asked to jump to " + wFmt(pos));
  };
  document.getElementById("wconfirmno").onclick = (e) => {
    e.stopPropagation();
    close();
  };
  clearTimeout(wConfirmTimer);
  wConfirmTimer = setTimeout(close, 10000);
}

// wBar draws the controls: the host's, or the requests for everyone else.
function wBar() {
  wSubLook = wSubLook || wSubLookLoad();
  const bar = document.getElementById("wbar");
  const v = wv;
  const btn = (id, ic, label, alt = true) => `<button class="btn${alt ? " alt" : ""}" id="${id}" title="${esc(label)}">${icon(ic)}<span>${esc(label)}</span></button>`;
  const origin = v.origin ? '<span class="worigin" id="worigin"></span>' : "";
  const common = `<button class="btn alt" id="wsettings" title="Audio, subtitles and automatic skips" aria-label="Playback settings"><span>Settings</span></button>
    <div class="wsettings" id="wsettingsmenu" hidden>
      <label>Audio language<select id="waudio" aria-label="Audio language"></select></label>
      <label>Subtitles<select id="wsub" aria-label="Subtitle language"></select></label>
      <label>Quality<select id="wquality" aria-label="Video quality"></select></label>
      <p class="note" id="wsettingshint"></p>
      <div class="wsublook">
        <span class="wskiptitle">Subtitle look · only you</span>
        <label>Size<select id="wsubsize">${wOptions(WSUB_SIZES, wSubLook.size)}</select></label>
        <label>Colour<select id="wsubcolor">${wOptions(WSUB_COLORS, wSubLook.color)}</select></label>
        <label>Background<select id="wsubback">${wOptions(WSUB_BACKS, wSubLook.back)}</select></label>
      </div>
      <div class="wskipsettings">
        <span class="wskiptitle">Automatic skips · everyone in this room</span>
        <div class="wskiptoggles">
          <button type="button" class="btn alt wskiptoggle" id="wautoop" aria-pressed="false">OP · Off</button>
          <button type="button" class="btn alt wskiptoggle" id="wautoed" aria-pressed="false">ED · Off</button>
        </div>
        <p class="note" id="wskiphint"></p>
      </div>
      <p class="note wkeyhelp">${v.is_host
        ? "Keys: Space pause · ←/→ 10s · ↑/↓ volume · M mute · F theater · Shift+N next ep · 0–9 jump · Home start"
        : "Tap the bar to ask for a spot. Keys: Space ask to pause · ←/→ ask -10s/+10s (press more to add up) · 0–9 ask to jump · ↑/↓ volume · M mute · F theater · Shift+N ask for next ep"}</p>
    </div>
    <span class="wbarlabel">${icon("volume-2")}</span><input type="range" id="wvol" min="0" max="3" step="0.05" value="1" aria-label="Volume (up to 300%)">
    ${origin}
    <button class="btn alt" id="wchatbtn" title="Room chat" aria-label="Room chat">${icon("message")}<span>Chat</span><i class="wbadge" id="wchatbadge" hidden></i></button>
    <button class="btn alt" id="wstopbar" title="Stop watching" aria-label="Stop watching">${icon("stop")}<span>Stop</span></button>
    <button class="btn alt wfullscreen" id="wfull" title="Toggle theater mode" aria-label="Toggle theater mode" aria-pressed="${wFull}">${icon(wFull ? "minimize" : "maximize")}</button>`;
  if (v.is_host) {
    bar.innerHTML = `<div class="wseek"><span id="wcur">0:00</span><div class="wseektrack"><input type="range" id="wpos" min="0" max="1" step="0.1" value="0" aria-label="Position"><div class="wskipmarkers" id="wskipmarkers" aria-hidden="true"></div></div><span id="wdur">0:00</span></div>
      <div class="row">${btn("wpp", "pause", "Pause", false)}${btn("wback10", "rewind", "-10s")}${btn("wforward10", "ff", "+10s")}
      ${v.ep > 1 ? btn("wprev", "back", "Prev ep") : ""}${v.ep < v.last_ep ? btn("wnext", "skip", "Next ep") : ""}${common}</div>`;
    const vid = document.getElementById("wvid");
    document.getElementById("wpp").onclick = () => wCtl(vid.paused ? "play" : "pause");
    document.getElementById("wback10").onclick = () => wCtl("seek", Math.max(0, vid.currentTime - 10));
    document.getElementById("wforward10").onclick = () => wCtl("forward");
    const on = (id, fn) => document.getElementById(id) && (document.getElementById(id).onclick = fn);
    on("wprev", () => wCtl("prev"));
    on("wnext", () => wCtl("next"));
    const pos = document.getElementById("wpos");
    pos.oninput = () => (pos.dataset.dragging = "1");
    pos.onchange = () => {
      delete pos.dataset.dragging;
      wCtl("seek", +pos.value);
    };
  } else {
    bar.innerHTML = `<div class="wseek"><span id="wcur">0:00</span><div class="wseektrack wasktrack" id="wasktrack" title="Ask the host to jump here"><div class="wprog"><i id="wprog"></i></div><div class="wskipmarkers" id="wskipmarkers" aria-hidden="true"></div></div><span id="wdur">0:00</span></div>
      <div class="row">${btn("wapp", "pause", "Ask to pause")}${btn("waback", "rewind", "Ask -10s")}${btn("waforward", "ff", "Ask +10s")}${v.ep < v.last_ep ? btn("wanext", "skip", "Ask for next ep") : ""}${common}</div>`;
    const vid0 = document.getElementById("wvid");
    document.getElementById("wapp").onclick = () => wAsk(wv.playing ? "pause" : "play");
    document.getElementById("waback").onclick = () => wAskBy(-10);
    document.getElementById("waforward").onclick = () => wAskBy(10);
    const track = document.getElementById("wasktrack");
    track.onclick = (e) => {
      const box = track.getBoundingClientRect();
      const dur = wv.dur || (isFinite(vid0.duration) ? vid0.duration : 0);
      if (!dur || !box.width) return;
      wAskJump((Math.min(Math.max(e.clientX - box.left, 0), box.width) / box.width) * dur);
    };
    const n = document.getElementById("wanext");
    if (n) n.onclick = () => wAsk("next");
  }
  const vid = document.getElementById("wvid");
  const vol = document.getElementById("wvol");
  wVolumeApply(vid);
  vol.oninput = () => {
    wVol = +vol.value;
    wMuted = false;
    wVolumeApply(vid);
    wVolumeSave();
  };
  document.getElementById("wfull").onclick = () => wFullscreen(!wFull);
  document.getElementById("wstopbar").onclick = wStopWatching;
  document.getElementById("wsettings").onclick = () => { const menu = document.getElementById("wsettingsmenu"); menu.hidden = !menu.hidden; };
  document.getElementById("wsub").onchange = (e) => { wSubPick = e.target.value; wSettingsDirty = true; wSubs(); wSaveLanguages(); };
  document.getElementById("waudio").onchange = (e) => { wAudioPick = e.target.value; wSettingsDirty = true; wAudioTracks(); wSaveLanguages(); wUpdateOrigin(); if (wv.audio_media && wEffectiveMedia() !== wvMedia) wLoad(); };
  document.getElementById("wquality").onchange = (e) => {
    wQuality = +e.target.value;
    if (wHls) wHls.currentLevel = wQuality;
    wQualityDraw();
  };
  for (const [id, key] of [["wsubsize", "size"], ["wsubcolor", "color"], ["wsubback", "back"]]) {
    document.getElementById(id).onchange = (e) => {
      wSubLook[key] = e.target.value;
      wSubLookSave();
      wSubLookApply();
    };
  }
  document.getElementById("wchatbtn").onclick = () => {
    wChatOpen = !wChatOpen;
    wChatPlace();
    if (wChatOpen) {
      const input = document.getElementById("wchatin");
      if (input) input.focus();
    }
  };
  const op = document.getElementById("wautoop");
  const ed = document.getElementById("wautoed");
  op.onclick = () => wCtl(wv.auto_skip_op ? "op-off" : "op-on");
  ed.onclick = () => wCtl(wv.auto_skip_ed ? "ed-off" : "ed-on");
  wAudioTracks();
  wSubs();
  wQualityDraw();
  wSubLookApply();
  wChatPlace();
  wUpdateOrigin();
  wSkipControls();
  wSeekMarkers();
}

// wQualityDraw fills the quality picker. Hls.js steps down on its own when
// the connection is slow; a viewer can also hold one size. A player with
// built-in HLS picks for itself.
function wQualityDraw() {
  const sel = document.getElementById("wquality");
  if (!sel) return;
  const levels = wHls ? wHls.levels || [] : [];
  if (levels.length < 2) {
    sel.innerHTML = `<option value="-1">Automatic</option>`;
    sel.disabled = true;
    return;
  }
  const name = (l, i) => (l.height ? l.height + "p" : Math.round((l.bitrate || 0) / 1000) + " kbps") || "Level " + (i + 1);
  const now = wHls.currentLevel >= 0 ? levels[wHls.currentLevel] : null;
  const order = levels.map((l, i) => i).sort((a, b) => (levels[b].height || levels[b].bitrate) - (levels[a].height || levels[a].bitrate));
  sel.innerHTML = `<option value="-1">Automatic${wQuality < 0 && now ? " (" + esc(name(now, wHls.currentLevel)) + ")" : ""}</option>`
    + order.map((i) => `<option value="${i}">${esc(name(levels[i], i))}</option>`).join("");
  sel.disabled = false;
  sel.value = String(wQuality);
}

// ── Subtitle look ──

const WSUB_SIZES = [["75", "Small"], ["100", "Normal"], ["130", "Large"], ["165", "Huge"]];
const WSUB_COLORS = [["#ffffff", "White"], ["#ffe14d", "Yellow"], ["#7fe7ff", "Cyan"], ["#8dff9a", "Green"], ["#ffb3d9", "Pink"]];
const WSUB_BACKS = [["shadow", "Shadow"], ["box", "Dark box"], ["none", "None"]];

function wOptions(list, value) {
  return list.map(([v, label]) => `<option value="${v}"${v === value ? " selected" : ""}>${label}</option>`).join("");
}

function wSubLookLoad() {
  const look = { size: "100", color: "#ffffff", back: "shadow" };
  try {
    const saved = JSON.parse(localStorage.getItem("otakase-watch-subs") || "{}");
    for (const [key, list] of [["size", WSUB_SIZES], ["color", WSUB_COLORS], ["back", WSUB_BACKS]]) {
      if (list.some(([v]) => v === saved[key])) look[key] = saved[key];
    }
  } catch (e) {}
  return look;
}

function wSubLookSave() {
  try {
    localStorage.setItem("otakase-watch-subs", JSON.stringify(wSubLook));
  } catch (e) {}
}

// wSubLookApply styles the subtitle cues in this viewer's player only.
function wSubLookApply() {
  wSubLook = wSubLook || wSubLookLoad();
  let el = document.getElementById("wsubstyle");
  if (!el) {
    el = document.createElement("style");
    el.id = "wsubstyle";
    document.head.appendChild(el);
  }
  const back = wSubLook.back === "box" ? "background: rgba(0, 0, 0, .72); text-shadow: none;"
    : wSubLook.back === "none" ? "background: transparent; text-shadow: none;"
      : "background: transparent; text-shadow: 0 0 4px #000, 0 2px 3px #000, 0 0 2px #000;";
  el.textContent = `#wvid::cue { font-size: ${+wSubLook.size}%; color: ${wSubLook.color}; ${back} }`;
}

// ── Room chat ──

function wChatBox() {
  return `<div class="wchat" id="wchat"><div class="wchathead"><b>${icon("message")} Room chat</b><button class="btn alt" id="wchatclose" aria-label="Close chat">${icon("x")}</button></div>
    <div class="wchatlog" id="wchatlog"><p class="note">Say hi. Put spoilers between || marks: ||like this||.</p></div>
    <form class="wchatform" id="wchatform"><input id="wchatin" maxlength="300" autocomplete="off" placeholder="Message · ||spoiler||" aria-label="Chat message"><button class="btn" aria-label="Send">${icon("send")}</button></form></div>`;
}

function wChatSetup() {
  const form = document.getElementById("wchatform");
  if (!form) return;
  const input = document.getElementById("wchatin");
  input.value = wChatDraft;
  input.oninput = () => (wChatDraft = input.value);
  form.onsubmit = (e) => {
    e.preventDefault();
    const text = input.value.trim();
    if (!text) return;
    input.disabled = true;
    api("watch/chat", { room: wRoom, text })
      .then((nv) => {
        input.value = wChatDraft = "";
        wSet(nv);
      })
      .catch(showError)
      .finally(() => {
        input.disabled = false;
        input.focus();
      });
  };
  document.getElementById("wchatclose").onclick = () => {
    wChatOpen = false;
    wChatPlace();
  };
  wChatDraw();
  wChatPlace();
}

// wChatPlace docks the chat inside the theater stage, where the chat
// button opens and closes it, and puts it back beside the player after.
function wChatPlace() {
  const chat = document.getElementById("wchat");
  if (!chat) return;
  const st = document.getElementById("wstage");
  const docked = wFull && st;
  const target = docked ? st : document.getElementById("wchatslot");
  if (target && chat.parentElement !== target) target.appendChild(chat);
  chat.classList.toggle("docked", !!docked);
  chat.classList.toggle("open", !!docked && wChatOpen);
  if (!docked || wChatOpen) wChatUnread = 0;
  const badge = document.getElementById("wchatbadge");
  if (badge) {
    badge.hidden = !wChatUnread;
    badge.textContent = wChatUnread > 9 ? "9+" : String(wChatUnread);
  }
  const btn = document.getElementById("wchatbtn");
  if (btn) btn.hidden = !wFull;
}

// wSpoilers escapes a message and blurs its ||spoilers||.
function wSpoilers(text) {
  return esc(text).replace(/\|\|(.+?)\|\|/g, '<span class="wspoil" tabindex="0" title="Spoiler: tap to show">$1</span>');
}

function wChatDraw() {
  const log = document.getElementById("wchatlog");
  if (!log || !wv) return;
  const chat = wv.chat || [];
  const last = chat.length ? chat[chat.length - 1].id : 0;
  const key = chat.map((c) => c.id).join(",");
  if (log.dataset.key === key) return;
  log.dataset.key = key;
  const fresh = chat.filter((c) => c.id > wChatSeen && c.user !== wv.me).length;
  wChatSeen = Math.max(wChatSeen, last);
  const nearBottom = log.scrollHeight - log.scrollTop - log.clientHeight < 40;
  const mod = wv.is_host || wv.staff;
  log.innerHTML = chat.length
    ? chat.map((c) => `<p class="wmsg${c.user === wv.me ? " mine" : ""}"><b>${esc(c.name)}</b> <span>${wSpoilers(c.text)}</span>${
      mod || c.user === wv.me ? `<button class="wmsgdel" data-del="${c.id}" aria-label="Remove message" title="Remove">${icon("x")}</button>` : ""}</p>`).join("")
    : `<p class="note">Say hi. Put spoilers between || marks: ||like this||.</p>`;
  log.querySelectorAll(".wspoil").forEach((s) => {
    s.onclick = () => s.classList.toggle("shown");
    s.onkeydown = (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); s.classList.toggle("shown"); } };
  });
  log.querySelectorAll("[data-del]").forEach((b) => {
    b.onclick = () => api("watch/chat", { room: wRoom, act: "delete", id: +b.dataset.del }).then((nv) => wSet(nv)).catch(showError);
  });
  if (nearBottom || fresh) log.scrollTop = log.scrollHeight;
  const chatEl = document.getElementById("wchat");
  if (fresh && chatEl && chatEl.classList.contains("docked") && !wChatOpen) {
    wChatUnread += fresh;
    wChatPlace();
  }
}

// Keep room-wide automatic skip toggles current without rebuilding the player.
function wSkipControls() {
  if (!wv) return;
  for (const [id, enabled] of [["wautoop", !!wv.auto_skip_op], ["wautoed", !!wv.auto_skip_ed]]) {
    const button = document.getElementById(id);
    if (!button) continue;
    button.setAttribute("aria-pressed", String(enabled));
    button.textContent = (id === "wautoop" ? "OP" : "ED") + " · " + (enabled ? "On" : "Off");
    button.disabled = !wv.is_host;
  }
  const hint = document.getElementById("wskiphint");
  if (!hint) return;
  if (!wv.skip_ready) {
    hint.textContent = "Looking up episode-specific opening and ending times…";
    return;
  }
  if (!wv.dur) {
    hint.textContent = "Waiting for the video duration to verify skip times…";
    return;
  }
  const times = wv.skip_times || {};
  const span = (s) => s && s.end > s.start ? wFmt(s.start) + "–" + wFmt(s.end) : "unavailable";
  hint.textContent = "Opening: " + span(times.op) + " · Ending: " + span(times.ed)
    + (!wv.is_host ? " · Only the host can change these switches." : "");
}

// Mark the actual OP/ED intervals along the entire video timeline.
// Render ranges rather than a guessed fixed point; inactive switches leave
// a faint guide so the host still knows where the opening/ending would be.
// Every update is based on the current episode view, so stale ranges vanish.
function wSeekMarkers() {
  const rail = document.getElementById("wskipmarkers");
  if (!rail || !wv) return;
  const duration = Number(wv.dur);
  const times = wv.skip_times || {};
  const track = rail.parentElement;
  if (!wv.skip_ready || !Number.isFinite(duration) || duration <= 0) {
    rail.replaceChildren();
    if (track) track.title = "";
    return;
  }
  const fragment = document.createDocumentFragment();
  const descriptions = [];
  for (const [kind, span, enabled] of [
    ["OP", times.op, wv.auto_skip_op],
    ["ED", times.ed, wv.auto_skip_ed],
  ]) {
    if (!span) continue;
    const start = Number(span.start), end = Number(span.end);
    if (!Number.isFinite(start) || !Number.isFinite(end) || start < 0 || end <= start || end > duration + 1) continue;
    const from = Math.max(0, Math.min(100, 100 * start / duration));
    const to = Math.max(from, Math.min(100, 100 * end / duration));
    const marker = document.createElement("span");
    marker.className = "wskipband " + kind.toLowerCase() + (enabled ? " active" : "");
    marker.style.left = from + "%";
    marker.style.width = (to - from) + "%";
    descriptions.push(kind + " " + wFmt(start) + "–" + wFmt(end) + (enabled ? " (auto-skip on)" : " (auto-skip off)"));
    const label = document.createElement("span");
    label.className = "wskipbandlabel";
    label.textContent = kind;
    marker.appendChild(label);
    fragment.appendChild(marker);
  }
  rail.replaceChildren(fragment);
  if (track) track.title = descriptions.join(" · ");
}

// Update the private source hint when the viewer switches to a separate
// audio release. The server only supplies these details to developers/admins.
function wUpdateOrigin() {
  const indicator = document.getElementById("worigin");
  if (!indicator || !wv) return;
  const alternate = wv.alternate_origin;
  const media = wv.audio_media || {};
  const pick = String(wAudioPick || "auto").toLowerCase().split("-")[0];
  const locale = (wv.audio_origins || {})[pick];
  const actual = wEffectiveMedia();
  const origin = locale && media[pick] === actual ? locale
    : alternate && media[alternate.mode] === actual ? alternate : wv.origin;
  if (!origin) {
    indicator.hidden = true;
    return;
  }
  const names = { anikoto: "Anikoto", megavid: "Megavid", kickassanime: "KickAssAnime",
    anizone: "AniZone", nyaa: "Nyaa", sukebei: "Sukebei" };
  const provider = names[String(origin.provider || "").toLowerCase()]
    || origin.provider || "Unknown";
  indicator.hidden = false;
  indicator.textContent = "via " + provider + (origin.link ? " · #" + origin.link : "");
  indicator.title = provider + (origin.host ? " · " + origin.host : "")
    + (origin.link ? " · link " + origin.link : "");
}

// Reveal the overlay controls on touch, then hide them while video is playing.
function wRevealControls() {
  const st = document.getElementById("wstage");
  if (!st || !wFull) return;
  clearTimeout(wControlsTimer);
  st.classList.remove("controls-hidden");
  const vid = document.getElementById("wvid");
  if (vid && !vid.paused) wControlsTimer = setTimeout(() => {
    if (!st.querySelector("#wsettingsmenu:not([hidden]), #wchat.open")) st.classList.add("controls-hidden");
  }, 3500);
}

function wControlsHidden() {
  const st = document.getElementById("wstage");
  return !!st && st.classList.contains("controls-hidden");
}

function wHideControls() {
  const st = document.getElementById("wstage");
  if (!st || !wFull) return;
  clearTimeout(wControlsTimer);
  const menu = document.getElementById("wsettingsmenu");
  if (menu) menu.hidden = true;
  st.classList.add("controls-hidden");
}

// Theater mode always works inside the embedded Activity; native fullscreen
// is optional because Discord may block the browser fullscreen API.
function wFullscreen(on) {
  const st = document.getElementById("wstage");
  if (!st) return;
  wFull = on;
  clearTimeout(wControlsTimer);
  // Move the theater stage out of animated Activity containers. Their
  // transforms otherwise turn position:fixed into an offset local box.
  if (on && st.parentElement !== document.body) {
    const marker = document.createElement("div");
    marker.id = "wstage-placeholder";
    st.parentNode.insertBefore(marker, st);
    document.body.appendChild(st);
  } else if (!on && st.parentElement === document.body) {
    const marker = document.getElementById("wstage-placeholder");
    if (marker && marker.parentNode) {
      marker.parentNode.replaceChild(st, marker);
    }
  }
  st.classList.toggle("full", on);
  st.classList.remove("controls-hidden");
  if (!on) wChatOpen = false;
  wChatPlace();
  if (on) wRevealControls();
  document.body.classList.toggle("wfull", on);
  const b = document.getElementById("wfull");
  if (b) {
    b.innerHTML = icon(on ? "minimize" : "maximize");
    b.setAttribute("aria-pressed", String(on));
    b.title = on ? "Exit theater mode" : "Theater mode";
  }
  if (on && document.fullscreenEnabled && !document.fullscreenElement && st.requestFullscreen) {
    st.requestFullscreen().catch(() => {});
  } else if (!on && document.fullscreenElement && document.exitFullscreen) {
    document.exitFullscreen().catch(() => {});
  }
}

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && wFull) wFullscreen(false);
});

// wEvents flashes what just happened in the room (a pause, a skip, someone
// joining) over the video for a moment. Several at once show together.
function wEvents() {
  const fresh = ((wv && wv.events) || []).filter((e) => e.id > wEventSeen);
  if (!fresh.length) return;
  wEventSeen = fresh[fresh.length - 1].id;
  const el = document.getElementById("wevent");
  if (!el) return;
  el.innerHTML = fresh.slice(-3).map((e) => `<span>${esc(e.text)}</span>`).join("");
  el.hidden = false;
  el.classList.remove("show");
  void el.offsetWidth;
  el.classList.add("show");
  clearTimeout(wEventTimer);
  wEventTimer = setTimeout(() => (el.hidden = true), 1800 + 400 * Math.min(fresh.length - 1, 2));
}

// wOsd flashes a short note over the video, for keys with no button to watch.
function wOsd(text) {
  const el = document.getElementById("wosd");
  if (!el) return;
  el.textContent = text;
  el.hidden = false;
  clearTimeout(wOsdTimer);
  wOsdTimer = setTimeout(() => (el.hidden = true), 900);
}

// Volume goes to 300%, for this viewer only. Up to 100% is the player's own
// volume; above it a Web Audio gain boosts the sound. The boost is wired in
// only once someone goes past 100% (a tap or a key, so the browser lets the
// audio start), and a video element can be wired only once.
let wVol = wVolumeLoad();
let wMuted = false;
let wAudioCtx = null;
const wGains = new WeakMap();

function wVolumeLoad() {
  try {
    const v = parseFloat(localStorage.getItem("otakase-watch-volume"));
    if (v >= 0 && v <= 3) return v;
  } catch (e) {}
  return 1;
}

function wVolumeSave() {
  try {
    localStorage.setItem("otakase-watch-volume", String(wVol));
  } catch (e) {}
}

function wBoost(vid) {
  if (wGains.has(vid)) return wGains.get(vid);
  const Ctx = window.AudioContext || window.webkitAudioContext;
  if (!Ctx) return null;
  try {
    wAudioCtx = wAudioCtx || new Ctx();
    const gain = wAudioCtx.createGain();
    wAudioCtx.createMediaElementSource(vid).connect(gain);
    gain.connect(wAudioCtx.destination);
    wGains.set(vid, gain);
    return gain;
  } catch (e) {
    return null;
  }
}

function wVolumeApply(vid) {
  if (!vid) return;
  vid.volume = Math.max(0, Math.min(1, wVol));
  vid.muted = wMuted || wVol === 0;
  const gain = wVol > 1 ? wBoost(vid) : wGains.get(vid);
  if (gain) {
    gain.gain.value = Math.max(1, wVol);
    if (wAudioCtx && wAudioCtx.state === "suspended") wAudioCtx.resume().catch(() => {});
  }
  const vol = document.getElementById("wvol");
  if (vol) vol.value = vid.muted ? 0 : wVol;
}

function wVolume(vid, volume, muted) {
  wVol = Math.round(Math.max(0, Math.min(3, volume)) * 100) / 100;
  wMuted = muted;
  wVolumeApply(vid);
  wVolumeSave();
  wOsd(vid.muted ? "Muted" : "Volume " + Math.round(wVol * 100) + "%");
}

// wKeySeek moves the host by step seconds (or to an absolute spot), from
// where the last key press sent it while the room hasn't caught up yet.
function wSeekBy(vid, step, to) {
  const now = Date.now();
  const from = wKeySeek && now - wKeySeek.at < 1500 ? wKeySeek.to : vid.currentTime;
  const dur = wv.dur || (isFinite(vid.duration) ? vid.duration : 0);
  let pos = to !== undefined ? to : from + step;
  pos = Math.max(0, dur ? Math.min(pos, Math.max(0, dur - 0.5)) : pos);
  wKeySeek = { to: pos, at: now };
  wCtl("seek", pos);
  wOsd(wFmt(pos) + (dur ? " / " + wFmt(dur) : ""));
}

// Player keys. Pause and seeking go through the room like the buttons do;
// volume, mute and theater mode only change this page.
document.addEventListener("keydown", (e) => {
  if (where !== "watch" || !wv || wv.phase !== "play" || e.ctrlKey || e.metaKey || e.altKey) return;
  const vid = document.getElementById("wvid");
  if (!vid) return;
  const t = e.target || {};
  if (t.isContentEditable || /^(TEXTAREA|SELECT)$/.test(t.tagName || "")) return;
  if (t.tagName === "INPUT" && t.type !== "range") return;
  const host = wv.is_host;
  const key = e.key.length === 1 ? e.key.toLowerCase() : e.key;
  switch (key) {
    case " ":
    case "k":
      if (e.repeat) break;
      if (host) wCtl(vid.paused ? "play" : "pause");
      else {
        wAsk(wv.playing ? "pause" : "play");
        wOsd(wv.playing ? "Asked to pause" : "Asked to play");
      }
      break;
    case "ArrowLeft":
    case "j":
      if (host) wSeekBy(vid, -10);
      else wAskBy(-10);
      break;
    case "ArrowRight":
    case "l":
      if (host) wSeekBy(vid, 10);
      else wAskBy(10);
      break;
    case "ArrowUp":
      wVolume(vid, wVol + (wVol >= 1 ? 0.1 : 0.05), false);
      break;
    case "ArrowDown":
      wVolume(vid, wVol - (wVol > 1 ? 0.1 : 0.05), false);
      break;
    case "m":
      if (e.repeat) break;
      if (vid.muted || wVol === 0) wVolume(vid, wVol || 0.5, false);
      else wVolume(vid, wVol, true);
      break;
    case "f":
      if (!e.repeat) wFullscreen(!wFull);
      break;
    case "n":
      if (!e.shiftKey || e.repeat || !(wv.ep < wv.last_ep)) return;
      if (host) wCtl("next");
      else {
        wAsk("next");
        wOsd("Asked for the next episode");
      }
      break;
    case "Home":
      if (!host) return;
      wSeekBy(vid, 0, 0);
      break;
    default: {
      const dur = wv.dur || (isFinite(vid.duration) ? vid.duration : 0);
      if (!dur || !/^[0-9]$/.test(key) || e.repeat) return;
      if (host) wSeekBy(vid, 0, (dur * +key) / 10);
      else wAskJump((dur * +key) / 10);
    }
  }
  e.preventDefault();
});
document.addEventListener("fullscreenchange", () => {
  if (!document.fullscreenElement && wFull && document.getElementById("wstage")) {
    // Leaving real full screen (Esc) leaves the filled window too.
    wFullscreen(false);
    if (wv && wv.phase === "loading") wDraw();
  }
});

// Saving account-wide preferences resets manual player choices to Automatic.
function wPreferencesChanged(preferences) {
  if (!wv) return;
  wv.preferences = preferences;
  wv.languages = { audio: "auto", subtitles: "auto" };
  wSettingsDirty = false;
  wAudioPick = "auto";
  wSubPick = "auto";
  wAudioTracks();
  wSubs();
  if (wv.media && wEffectiveMedia() !== wvMedia) wLoad();
}

// Changes affect this viewer only. Shared playback position stays unchanged.
function wSaveLanguages() {
  api("watch/languages", { room: wRoom, audio: wAudioPick, subtitles: wSubPick })
    .then((saved) => {
      if (wv) wv.languages = saved;
      wSettingsDirty = false;
    })
    .catch(showError);
}

function wAudioTracks() {
  const select = document.getElementById("waudio");
  if (!select) return;
  const variants = (wv && wv.audio_media) || {};
  const tracks = wHls ? wHls.audioTracks || [] : [];
  const options = [{ value: "auto", label: "Automatic" }];
  const seen = new Set();
  const names = { ja: "Japanese", en: "English", de: "German", es: "Spanish",
    fr: "French", hi: "Hindi", it: "Italian", pt: "Portuguese",
    ta: "Tamil", te: "Telugu", ko: "Korean", zh: "Chinese",
    ru: "Russian", ar: "Arabic", pl: "Polish", tr: "Turkish",
    uk: "Ukrainian", id: "Indonesian", nl: "Dutch", th: "Thai" };
  const normalized = wNormAudio;
  const add = (code, label) => {
    const lang = normalized(code);
    if (!lang || seen.has(lang)) return;
    seen.add(lang);
    const fallback = /^track-(\d+)$/.test(lang) ? "Audio track " + lang.split("-")[1]
      : /^hls-(\d+)$/.test(lang) ? "Audio track " + (+lang.split("-")[1] + 1)
      : names[lang.split("-")[0]] || lang;
    options.push({ value: lang, label: label || fallback });
  };
  // Language-specific torrent or provider releases appear even if their
  // manifest has only one muxed audio track.
  Object.keys(variants).sort().forEach((code) => {
    add(code === "sub" ? "ja" : code === "dub" ? "en" : code);
  });
  // HLS alternative renditions may have no LANGUAGE tag; keep those
  // selectable by track index instead of silently omitting them.
  tracks.forEach((track, index) => add(track.lang || "hls-" + index, track.name));
  select.innerHTML = options.map((o) =>
    `<option value="${esc(o.value)}">${esc(o.label)}</option>`).join("");
  select.disabled = options.length === 1;
  const preferred = normalized(wAudioPick);
  const candidates = wAudioCandidates();
  select.value = preferred === "auto" || !seen.has(preferred) ? "auto" : preferred;
  const hint = document.getElementById("wsettingshint");
  if (hint) hint.textContent = preferred === "auto" && candidates.length
    ? "Automatic: " + candidates.map((code) => names[code.split("-")[0]] || code).join(" → ") + " → stream default."
    : options.length === 1
      ? "No selectable audio tracks were reported by this stream."
      : preferred !== "auto" && !seen.has(preferred)
        ? "Selected audio unavailable; using the current stream."
        : "";
  if (wHls && tracks.length) {
    for (const code of candidates) {
      const index = tracks.findIndex((track, i) =>
        normalized(track.lang || "hls-" + i) === code);
      if (index >= 0) {
        if (wHls.audioTrack !== index) wHls.audioTrack = index;
        break;
      }
    }
  }
}

// wSubsFollow keeps a growing subtitle file up to date. A torrent's
// subtitles are converted while it downloads, and the bot marks the file
// as partial until then. The browser loads a track once, so newer copies
// are handed to it as a local file until the marker is gone.
function wSubsFollow(tr, url) {
  const media = wvMedia;
  let last = "";
  let local = "";
  let tries = 0;
  const live = () => wvMedia === media && tr.isConnected;
  const step = async () => {
    if (!live()) {
      if (local) URL.revokeObjectURL(local);
      return;
    }
    let text = "";
    try {
      const res = await fetch(url, { cache: "no-store" });
      if (res.ok) text = await res.text();
    } catch (e) {
      text = "";
    }
    if (!live()) {
      if (local) URL.revokeObjectURL(local);
      return;
    }
    const partial = /\nNOTE otakase-partial\n/.test(text);
    if (text && text !== last && (partial || local)) {
      const old = local;
      local = URL.createObjectURL(new Blob([text], { type: "text/vtt" }));
      tr.src = local;
      if (old) URL.revokeObjectURL(old);
    }
    if (text) last = text;
    // Keep following a partial file; retry a failed fetch only briefly.
    if (partial ? ++tries < 360 : !text && ++tries < 3) setTimeout(step, 10000);
  };
  setTimeout(step, 3000);
}

// wPlayingSub tells whether this viewer hears the original Japanese audio:
// the language release they play, else the Hls.js audio track, else the
// room's release.
function wPlayingSub() {
  if (!wv) return false;
  for (const [code, url] of Object.entries(wv.audio_media || {})) {
    if (wDirectURL(url) === wvMedia) return code === "sub" || wNormAudio(code).split("-")[0] === "ja";
  }
  const track = wHls && wHls.audioTracks ? wHls.audioTracks[wHls.audioTrack] : null;
  if (track && track.lang) return wNormAudio(track.lang).split("-")[0] === "ja";
  return !wv.dub;
}

// wSubs fills the subtitle picker and shows the picked track: English by
// default.
function wSubs() {
  const vid = document.getElementById("wvid");
  const sel = document.getElementById("wsub");
  if (!vid || !sel) return;
  const tracks = Array.from(vid.textTracks).filter((t) => t.kind === "subtitles" || t.kind === "captions");
  // Automatic subtitles use the viewer's saved preference for the audio
  // they hear: one for the original Japanese audio, one for dubs.
  const pref = (wv && wv.preferences) || {};
  let pick = wSubPick !== "auto" ? wSubPick : (wPlayingSub() ? pref.sub_subtitles : pref.subtitles) || "auto";
  if (pick === "off") pick = "-1";
  else if (pick !== "auto" && !/^[0-9]+$/.test(pick)) {
    const match = tracks.findIndex((t) => {
      const lang = (t.language || "").toLowerCase();
      return lang === pick || lang.startsWith(pick + "-") || pick === "en" && lang === "eng" || pick === "ja" && lang === "jpn";
    });
    pick = String(match >= 0 ? match : -1);
  }
  if (pick === "auto") {
    const en = tracks.findIndex((t) => /^en/i.test(t.language) || /english/i.test(t.label));
    pick = String(en >= 0 ? en : tracks.length ? 0 : -1);
  }
  sel.innerHTML = `<option value="off">Subtitles off</option><option value="auto">Automatic</option>` + tracks.map((t, i) => `<option value="${esc((t.language || "und").toLowerCase())}">${esc(t.label || t.language || "Track " + (i + 1))}</option>`).join("");
  sel.value = wSubPick === "auto" ? "auto" : wSubPick === "off" ? "off" : wSubPick;
  if (sel.selectedIndex < 0) sel.value = "auto";
  tracks.forEach((t, i) => (t.mode = String(i) === pick ? "showing" : "disabled"));
}

function wTime() {
  const vid = document.getElementById("wvid");
  if (!vid) return;
  const dur = isFinite(vid.duration) ? vid.duration : wv.dur || 0;
  const cur = document.getElementById("wcur");
  if (cur) cur.textContent = wFmt(vid.currentTime);
  const d = document.getElementById("wdur");
  if (d) d.textContent = wFmt(dur);
  const pos = document.getElementById("wpos");
  if (pos && !pos.dataset.dragging) {
    pos.max = dur || 1;
    pos.value = vid.currentTime;
  }
  const prog = document.getElementById("wprog");
  if (prog) prog.style.width = dur ? (100 * vid.currentTime) / dur + "%" : "0";
  const pp = document.getElementById("wpp");
  if (pp) {
    const label = vid.paused ? "Play" : "Pause";
    if (pp.dataset.l !== label) (pp.dataset.l = label), (pp.innerHTML = icon(vid.paused ? "play" : "pause") + `<span>${label}</span>`);
  }
  const ap = document.getElementById("wapp");
  if (ap) {
    const label = wv.playing ? "Ask to pause" : "Ask to play";
    if (ap.dataset.l !== label) (ap.dataset.l = label), (ap.innerHTML = icon(wv.playing ? "pause" : "play") + `<span>${label}</span>`);
  }
}

// Leaving the Watch Together page restores Activity sound even when the
// visitor never joined a room (the lobby and busy-room pages have no wv).
// Only tell the server to leave when there was an actual room session.
function watchLeave(pageClosing = false) {
  if (where !== "watch" && !wv) return;
  const wasInRoom = !!wv;
  if (where === "watch") where = "";
  wStop();
  if (wasInRoom && session) {
    fetch("api/watch/leave", {
      method: "POST",
      keepalive: true,
      headers: { Authorization: "Bearer " + session, "Content-Type": "application/json" },
      body: JSON.stringify({ room: wRoom }),
    }).catch(() => {});
  }
  wv = null;
  wvKey = "";
  // A page transition can immediately resume sound; closing the Discord
  // Activity should not restart music while the document is unloading.
  if (!pageClosing) soundApply();
}

addEventListener("pagehide", () => watchLeave(true));
