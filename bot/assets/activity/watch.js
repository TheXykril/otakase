// The watch room: an anime episode the bot hosts in the Activity, watched
// in step by everyone in the call (watchroom.go). The host picks and
// controls; everyone else follows the host's position and can ask for a
// pause, the opening skipped or the next episode. Uses app.js's helpers.
"use strict";

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
let wSubPick = "auto";
let wAudioPick = "auto";
let wSettingsDirty = false;
let wSearch = [];
let wFull = false; // the player fills the window
let wControlsTimer = 0;
let wChanging = false; // the host is picking another show while one plays

const wFmt = (s) => {
  s = Math.max(0, Math.floor(s || 0));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = String(s % 60).padStart(2, "0");
  return h ? `${h}:${String(m).padStart(2, "0")}:${ss}` : `${m}:${ss}`;
};

const ASK_TEXT = { pause: "pause", play: "play", skip: "skip the opening", next: "go to the next episode" };

// watchView is the tile's page: the lobby, then the room.
async function watchView() {
  page("Watch <span>room</span>", `<div id="wbox">${loading("Looking for the watch room…")}</div>`);
  where = "watch";
  soundApply();
  const st = await api(`watch/status?room=${encodeURIComponent(room)}`);
  if (where !== "watch") return;
  if (st.busy && !st.here) {
    const at = st.channel_name ? "#" + st.channel_name : "another call";
    return wBox(`<p class="lead">A watch room is running in <b>${esc(at)}</b>${st.show ? `: ${esc(st.show)}` : ""}.</p>
      <p class="note">Only one runs at a time, since the bot streams it to everyone. Join that call to watch, or come back once it's over${
        st.ends ? ` (by ${new Date(st.ends).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })})` : ""
      }.</p>`);
  }
  if (st.busy) return watchJoin();
  const t = st.tickets || 0;
  wBox(`<p class="lead">The bot plays an anime for everyone in this call, in step. Whoever opens the room picks the show and controls it; everyone else can ask for a pause or a skip.</p>
    <p class="note">Up to ${st.max} people · 720p · 4 hours from the first episode · your AniList or MAL moves on as you watch (link them with /link-anilist and /link-mal).</p>
    <p>${icon("star")} You have <b>${t}</b> watch ticket${t === 1 ? "" : "s"}. Opening a room uses one once the first episode plays.</p>
    ${t > 0 ? `<button class="btn" id="wopen">${icon("play")} Open the watch room</button>` : `<p class="note">Get a ticket by redeeming a code with <b>/redeem</b>. Codes come from giveaways, events and somewhere hidden in Otakase.</p>`}
    <div class="err" id="err"></div>`);
  const b = document.getElementById("wopen");
  if (b) b.onclick = () => ((b.disabled = true), watchJoin().catch((e) => ((b.disabled = false), showError(e))));
}

function wBox(html) {
  const detached = document.body.querySelector(":scope > #wstage");
  if (detached) detached.remove();
  const marker = document.getElementById("wstage-placeholder");
  if (marker) marker.remove();
  const el = document.getElementById("wbox");
  if (el) el.innerHTML = html;
}

async function watchJoin() {
  const v = await api("watch/open", { room, channel });
  wvKey = wvMedia = "";
  wSet(v);
  const me = ++wvLoop;
  (async () => {
    while (me === wvLoop && where === "watch") {
      const r = await fetch(`api/watch/state?room=${encodeURIComponent(room)}&v=${wv ? wv.ver : -1}`, {
        headers: { Authorization: "Bearer " + session },
      }).catch(() => null);
      if (me !== wvLoop || where !== "watch") break;
      const nv = r && r.ok ? await r.json().catch(() => null) : null;
      if (nv) wSet(nv);
      else await new Promise((ok) => setTimeout(ok, 3000));
      if (wv && wv.phase === "over") break;
    }
    if (me === wvLoop) where === "watch" ? wStop() : watchLeave();
  })();
}

// wSet takes a new view of the room and redraws what changed.
function wSet(v, fromHost) {
  if (!v || where !== "watch") return;
  const oldAudioMedia = JSON.stringify((wv && wv.audio_media) || {});
  wv = v;
  wvAt = Date.now();
  if (v.languages && !wSettingsDirty) {
    wAudioPick = v.languages.audio || "auto";
    wSubPick = v.languages.subtitles || "auto";
  }
  if (v.phase === "over") {
    wStop();
    return wBox(`<p class="lead">The watch room is over.</p><p class="note">Thanks for watching. Open the games menu for more.</p>`);
  }
  const key = [v.phase, v.is_host, v.show && v.show.id, v.ep, v.err, v.media, v.load_step].join("|");
  if (wChanging && v.is_host) {
    wvKey = key;
    return;
  }
  wChanging = false;
  if (key !== wvKey) {
    if (document.fullscreenElement && document.exitFullscreen) document.exitFullscreen().catch(() => {});
    wFull = false;
    document.body.classList.remove("wfull");
    wvKey = key;
    wDraw();
  }
  const elapsedEl = document.getElementById("wloadelapsed");
  if (elapsedEl && v.load_since) {
    const seconds = Math.max(0, Math.floor((Date.now() - v.load_since) / 1000));
    elapsedEl.textContent = `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
  }
  wPeople();
  wAsks();
  wSyncLine();
  if (oldAudioMedia !== JSON.stringify(v.audio_media || {})) wAudioTracks();
  if (v.media && wEffectiveMedia() !== wvMedia) wLoad();
  else if (fromHost) wApply(true);
}

function wDraw() {
  const v = wv;
  const showLine = v.show ? `<b>${esc(v.show.title)}</b> · episode ${v.ep}${v.last_ep ? ` of ${v.last_ep}` : ""}${v.dub ? " · dub" : ""}` : "";
  let stage;
  if (v.phase === "pick" || (v.phase === "error" && !v.media)) {
    stage = v.is_host
      ? `${v.err ? `<p class="err">${esc(v.err)}</p>` : ""}<div id="wpick"></div>`
      : `<p class="msg">${v.err ? esc(v.err) : "The host is picking what to watch…"}</p>`;
  } else if (v.phase === "loading") {
    const step = v.load_step || "Finding episode sources";
    const elapsed = v.load_since ? Math.max(0, Math.floor((Date.now() - v.load_since) / 1000)) : 0;
    stage = `<div class="msg loading"><p><b id="wloadstep">${esc(step)}</b></p><p class="note">Elapsed: <span id="wloadelapsed">${Math.floor(elapsed / 60)}:${String(elapsed % 60).padStart(2, "0")}</span></p><p class="note">Alternative playback can take several minutes to connect and buffer. The current step updates as preparation advances.</p></div>`;
  } else {
    stage = `<div class="wstage${wFull ? " full" : ""}" id="wstage"><div class="wplayer" id="wplayer"><video id="wvid" playsinline preload="auto"></video>
      <button class="wtap" id="wtap" hidden>${icon("play")} Tap to watch</button><div class="wspin" id="wspin" hidden></div></div>
      <div class="wbar" id="wbar"></div></div>`;
  }
  wBox(`<div class="whead"><span class="wshow">${showLine || "Nothing playing yet"}</span><span class="pill" id="wends"></span></div>
    ${stage}
    <div class="wasks" id="wasks"></div>
    <div class="wside"><h3 class="lh">${icon("users")} Watching <span id="wcount"></span></h3><div class="wpeople" id="wpeople"></div>
    <p class="note" id="wsync"></p><div class="row" id="wmore"></div></div>
    <div class="err" id="err"></div>`);
  if (document.getElementById("wpick")) wPicker();
  if (document.getElementById("wbar")) {
    wBar();
    const player = document.getElementById("wplayer");
    if (player) player.addEventListener("click", () => { if (wFull) wRevealControls(); });
    const bar = document.getElementById("wbar");
    bar.addEventListener("pointerdown", () => { clearTimeout(wControlsTimer); });
    bar.addEventListener("pointerup", () => { if (wFull) wRevealControls(); });
    bar.addEventListener("focusin", () => { clearTimeout(wControlsTimer); });
    bar.addEventListener("focusout", () => { if (wFull) wRevealControls(); });
  }
  wMore();
  if (v.ends) document.getElementById("wends").textContent = "Ends " + new Date(v.ends).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  else document.getElementById("wends").hidden = true;
}

function wPeople() {
  const el = document.getElementById("wpeople");
  if (!el) return;
  el.innerHTML = wv.members.map((m) => `<span class="pill">${m.host ? icon("crown") + " " : ""}${esc(m.name)}</span>`).join("");
  document.getElementById("wcount").textContent = `${wv.members.length}/${wv.max}`;
}

function wSyncLine() {
  const el = document.getElementById("wsync");
  if (!el) return;
  const linked = wv.linked || [];
  if (!linked.length) {
    el.innerHTML = "Link /link-anilist or /link-mal and episodes you watch here count on your list.";
    return;
  }
  el.innerHTML = `<label class="wsw"><input type="checkbox" id="wsyncbox" ${wv.sync ? "checked" : ""}> Move my ${linked.join(" and ")} on after each episode I watch</label>`;
  document.getElementById("wsyncbox").onchange = (e) =>
    api("watch/sync", { room, on: e.target.checked })
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
  if (v.is_host || v.staff) html += `<button class="btn alt" id="wend">${icon("x")} End room</button>`;
  el.innerHTML = html;
  const on = (id, fn) => document.getElementById(id) && (document.getElementById(id).onclick = fn);
  on("wcancel", () => api("watch/ctl", { room, act: "cancel" }).then((nv) => ((wvKey = ""), wSet(nv))).catch(showError));
  on("wchange", () => {
    wChanging = true;
    wBox(`<div id="wpick"></div>`);
    wDestroy();
    wPicker(true);
  });
  on("wtake", () => api("watch/take", { room }).then((nv) => ((wvKey = ""), (wvMedia = ""), wSet(nv))).catch(showError));
  on("wend", (e) => {
    const b = e.currentTarget;
    if (!b.classList.contains("sure")) {
      b.classList.add("sure");
      b.lastChild.textContent = " Sure? Tap again";
      return;
    }
    api("watch/end", { room }).catch(showError);
  });
}

// ── Requests ──

function wAsks() {
  const el = document.getElementById("wasks");
  if (!el) return;
  if (wv.is_host) {
    el.innerHTML = wv.asks
      .map(
        (a) => `<div class="wask"><span><b>${esc(a.name)}</b> asks to ${ASK_TEXT[a.kind]}</span>
        <button class="btn" data-ok="${a.id}">Yes</button><button class="btn alt" data-no="${a.id}">No</button></div>`
      )
      .join("");
    el.querySelectorAll("[data-ok],[data-no]").forEach(
      (b) =>
        (b.onclick = () => {
          b.disabled = true;
          wHostAct = Date.now();
          api("watch/answer", { room, id: +(b.dataset.ok || b.dataset.no), ok: !!b.dataset.ok })
            .then((nv) => wSet(nv, true))
            .catch(showError);
        })
    );
  } else {
    el.innerHTML = wv.asks.map((a) => `<div class="wask mine"><span>Asked the host to ${ASK_TEXT[a.kind]}…</span></div>`).join("");
  }
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
    t = setTimeout(async () => {
      const text = q.value.trim();
      if (text.length < 2) return (document.getElementById("wres").innerHTML = "");
      try {
        wSearch = await api(`watch/search?room=${encodeURIComponent(room)}&q=${encodeURIComponent(text)}`);
      } catch (e) {
        return showError(e);
      }
      if (q.value.trim() !== text) return;
      const res = document.getElementById("wres");
      res.innerHTML = wSearch.length
        ? wSearch
            .map(
              (s, i) => `<button class="wshowcard" data-i="${i}">${s.cover ? `<img src="${esc(s.cover)}" alt="" loading="lazy">` : "<i></i>"}
              <span><b>${esc(s.title)}</b><small>${[s.year, s.format && s.format.replace("_", " ").toLowerCase(), s.aired ? s.aired + " out" : s.episodes ? s.episodes + " eps" : ""]
                .filter(Boolean)
                .join(" · ")}</small></span></button>`
            )
            .join("")
        : `<p class="note">Nothing found.</p>`;
      res.querySelectorAll("[data-i]").forEach((b) => (b.onclick = () => wEpisode(wSearch[+b.dataset.i])));
    }, 300);
  };
  q.focus();
  const c = document.getElementById("wcancel");
  if (c) c.onclick = () => ((wChanging = false), (wvKey = ""), (wvMedia = ""), wSet(wv));
}

function wPickerHeading(s) {
  return `<div class="wpickhead">${s.cover ? `<img src="${esc(s.cover)}" alt="">` : ""}
    <div><span class="wpickey">EPISODE SELECTION</span><h3>${esc(s.title)}</h3></div></div>`;
}

function wEpisode(s) {
  const previews = new Map();
  for (const item of s.previews || []) {
    const title = item.title || "";
    const match = title.match(/\b(?:episode|ep\.?)[\s:]*([0-9]+)\b/i) || title.match(/^([0-9]+)\s*[-:.]/);
    if (match && !previews.has(Number(match[1]))) previews.set(Number(match[1]), item);
  }
  const last = Math.max(1, s.aired || s.episodes || 1);
  const next = wv.show && wv.show.id === s.id ? Math.min(wv.ep + 1, last) : 1;
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
      <div class="wepfooter"><button class="btn alt" id="wback">Back to results</button>
        <button class="btn" id="wgo">${icon("play")} Play episode ${selected}</button></div>
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
    const go = document.getElementById("wgo");
    go.onclick = () => {
      go.disabled = true;
      wChanging = false;
      wvKey = "";
      api("watch/pick", { room, show: s.id, ep: selected, dub: /^(en|eng)(-|$)/.test(wAudioPick) })
        .then((nv) => wSet(nv))
        .catch((err) => { go.disabled = false; showError(err); });
    };
  };
  draw();
  fetch(`api/watch/episodes?id=${encodeURIComponent(s.id)}&room=${encodeURIComponent(room)}`, {
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

function wEffectiveMedia() {
  if (!wv) return "";
  const variants = wv.audio_media || {};
  const mode = /^(en|eng)(-|$)/.test(wAudioPick) ? "dub" : /^(ja|jpn)(-|$)/.test(wAudioPick) ? "sub" : "";
  return mode && variants[mode] ? variants[mode] : wv.media;
}

async function wLoad() {
  const vid = document.getElementById("wvid");
  if (!vid) return;
  wDestroy();
  wvMedia = wEffectiveMedia();
  wFails = 0;
  const at = wTarget();
  for (const s of wv.subs || []) {
    const tr = document.createElement("track");
    tr.kind = "subtitles";
    tr.label = s.label;
    if (s.lang) tr.srclang = s.lang;
    tr.src = s.url;
    vid.appendChild(tr);
  }
  vid.onloadedmetadata = () => wSubs();
  vid.onwaiting = () => { document.getElementById("wspin").hidden = false; if (wv.is_host) wBeatNow(); };
  vid.onplaying = () => { document.getElementById("wspin").hidden = true; if (wv.is_host) wBeatNow(); };
  vid.oncanplay = () => (document.getElementById("wspin").hidden = true);
  vid.onerror = () => wBroken();
  vid.onended = () => {
    if (wv.is_host && wv.ep < wv.last_ep) wCtl("next");
  };
  vid.ontimeupdate = wTime;
  if (/\.m3u8(?:$|\?)/i.test(wvMedia) && !vid.canPlayType("application/vnd.apple.mpegurl")) {
    try {
      await hlsReady();
    } catch (e) {
      return showError(e);
    }
    if (wvMedia !== wEffectiveMedia()) return;
    if (!Hls.isSupported()) return showError(new Error("This Discord can't play the stream."));
    wHls = new Hls({ startPosition: at, maxBufferLength: 40, backBufferLength: 60, enableWorker: false, renderTextTracksNatively: true });
    wHls.on(Hls.Events.ERROR, (_, d) => {
      if (!d.fatal) return;
      if (d.type === Hls.ErrorTypes.MEDIA_ERROR) return wHls.recoverMediaError();
      if (++wFails > 4) return wBroken();
      setTimeout(() => wHls && wHls.startLoad(), 1500);
    });
    wHls.on(Hls.Events.SUBTITLE_TRACKS_UPDATED, wSubs);
    wHls.on(Hls.Events.AUDIO_TRACKS_UPDATED, wAudioTracks);
    wHls.loadSource(wvMedia);
    wHls.attachMedia(vid);
  } else {
    vid.src = wvMedia;
    vid.currentTime = at;
  }
  wApply(true);
  wFollow = setInterval(() => wApply(false), 1000);
  if (wv.is_host) wBeat = setInterval(wBeatNow, 4000);
}

// wBroken moves the host to the episode's next source; others just wait.
function wBroken() {
  if (wv && wv.is_host) wCtl("broken");
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
  if (wv.is_host && !force) return;
  const t = wTarget();
  if (vid.readyState > 0 && Math.abs(vid.currentTime - t) > (wv.is_host ? 0.5 : 2)) vid.currentTime = t;
  if (wv.playing && vid.paused) {
    vid.play().then(
      () => wTap(false),
      () => wTap(true)
    );
  } else if (!wv.playing && !vid.paused) vid.pause();
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
    body: JSON.stringify({ room, pos: vid.currentTime, playing: !vid.paused && !vid.ended && vid.readyState >= 3, dur: isFinite(vid.duration) ? vid.duration : 0 }),
  }).catch(() => {});
}

function wCtl(act, pos) {
  wHostAct = Date.now();
  const vid = document.getElementById("wvid");
  if (pos === undefined) pos = vid ? vid.currentTime : -1;
  return api("watch/ctl", { room, act, pos })
    .then((nv) => wSet(nv, true))
    .catch(showError);
}

function wAsk(kind) {
  api("watch/ask", { room, kind })
    .then((nv) => wSet(nv))
    .catch(showError);
}

// wBar draws the controls: the host's, or the requests for everyone else.
function wBar() {
  const bar = document.getElementById("wbar");
  const v = wv;
  const btn = (id, ic, label, alt = true) => `<button class="btn${alt ? " alt" : ""}" id="${id}" title="${esc(label)}">${icon(ic)}<span>${esc(label)}</span></button>`;
  const common = `<button class="btn alt" id="wsettings" title="Audio and subtitle languages" aria-label="Audio and subtitle languages"><span>Languages</span></button>
    <div class="wsettings" id="wsettingsmenu" hidden><label>Audio language<select id="waudio" aria-label="Audio language"></select></label><label>Subtitles<select id="wsub" aria-label="Subtitle language"></select></label><p class="note" id="wsettingshint"></p></div>
    <span class="wbarlabel">${icon("volume-2")}</span><input type="range" id="wvol" min="0" max="1" step="0.05" value="1" aria-label="Volume">
    <button class="btn alt wfullscreen" id="wfull" title="Toggle theater mode" aria-label="Toggle theater mode" aria-pressed="${wFull}">${icon(wFull ? "minimize" : "maximize")}</button>`;
  if (v.is_host) {
    bar.innerHTML = `<div class="wseek"><span id="wcur">0:00</span><input type="range" id="wpos" min="0" max="1" step="0.1" value="0" aria-label="Position"><span id="wdur">0:00</span></div>
      <div class="row">${btn("wpp", "pause", "Pause", false)}${btn("wback10", "undo", "10s")}${btn("wskip", "ff", "Skip OP")}
      ${v.ep > 1 ? btn("wprev", "back", "Prev ep") : ""}${v.ep < v.last_ep ? btn("wnext", "skip", "Next ep") : ""}${common}</div>`;
    const vid = document.getElementById("wvid");
    document.getElementById("wpp").onclick = () => wCtl(vid.paused ? "play" : "pause");
    document.getElementById("wback10").onclick = () => wCtl("seek", Math.max(0, vid.currentTime - 10));
    document.getElementById("wskip").onclick = () => wCtl("skip");
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
    bar.innerHTML = `<div class="wseek"><span id="wcur">0:00</span><div class="wprog"><i id="wprog"></i></div><span id="wdur">0:00</span></div>
      <div class="row">${btn("wapp", "pause", "Ask to pause")}${btn("waskip", "ff", "Ask to skip OP")}${v.ep < v.last_ep ? btn("wanext", "skip", "Ask for next ep") : ""}${common}</div>`;
    document.getElementById("wapp").onclick = () => wAsk(wv.playing ? "pause" : "play");
    document.getElementById("waskip").onclick = () => wAsk("skip");
    const n = document.getElementById("wanext");
    if (n) n.onclick = () => wAsk("next");
  }
  const vid = document.getElementById("wvid");
  const vol = document.getElementById("wvol");
  vol.value = vid.volume;
  vol.oninput = () => ((vid.volume = +vol.value), (vid.muted = +vol.value === 0));
  document.getElementById("wfull").onclick = () => wFullscreen(!wFull);
  document.getElementById("wsettings").onclick = () => { const menu = document.getElementById("wsettingsmenu"); menu.hidden = !menu.hidden; };
  document.getElementById("wsub").onchange = (e) => { wSubPick = e.target.value; wSettingsDirty = true; wSubs(); wSaveLanguages(); };
  document.getElementById("waudio").onchange = (e) => { wAudioPick = e.target.value; wSettingsDirty = true; wAudioTracks(); wSaveLanguages(); if (wv.audio_media && wEffectiveMedia() !== wvMedia) wLoad(); };
  wAudioTracks();
  wSubs();
}

// Reveal the overlay controls on touch, then hide them while video is playing.
function wRevealControls() {
  const st = document.getElementById("wstage");
  if (!st || !wFull) return;
  clearTimeout(wControlsTimer);
  st.classList.remove("controls-hidden");
  const vid = document.getElementById("wvid");
  if (vid && !vid.paused) wControlsTimer = setTimeout(() => {
    if (!st.querySelector("#wsettingsmenu:not([hidden])")) st.classList.add("controls-hidden");
  }, 3500);
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
document.addEventListener("fullscreenchange", () => {
  if (!document.fullscreenElement && wFull && document.getElementById("wstage")) {
    // Leaving real full screen (Esc) leaves the filled window too.
    wFullscreen(false);
  }
});

// Changes affect this viewer only. Shared playback position stays unchanged.
function wSaveLanguages() {
  api("watch/languages", { room, audio: wAudioPick, subtitles: wSubPick })
    .then((saved) => {
      if (wv) wv.languages = saved;
      wSettingsDirty = false;
    })
    .catch(showError);
}

function wAudioTracks() {
  const select = document.getElementById("waudio");
  if (!select) return;
  const variants = wv && wv.audio_media || {};
  if (Object.keys(variants).length) {
    select.innerHTML = `<option value="auto">Automatic</option>` +
      (variants.sub ? `<option value="ja">Japanese</option>` : "") +
      (variants.dub ? `<option value="en">English</option>` : "");
    select.disabled = false;
    select.value = /^(en|eng)(-|$)/.test(wAudioPick) && variants.dub ? "en" :
      /^(ja|jpn)(-|$)/.test(wAudioPick) && variants.sub ? "ja" : "auto";
    document.getElementById("wsettingshint").textContent =
      wAudioPick !== "auto" && select.value === "auto" ? "The requested language is not available in the discovered streams." : "Dub and sub options shown here are separate playable streams.";
    return;
  }
  const tracks = wHls ? wHls.audioTracks || [] : [];
  const current = tracks.findIndex((track) => {
    const language = (track.lang || "").toLowerCase();
    const preferred = wAudioPick === "en" ? "eng" : wAudioPick === "ja" ? "jpn" : wAudioPick;
    return wAudioPick !== "auto" && (language === preferred || language.startsWith(preferred + "-") || language === wAudioPick || language.startsWith(wAudioPick + "-"));
  });
  select.innerHTML = `<option value="auto">Automatic</option>` + tracks.map((track, index) =>
    `<option value="${esc((track.lang || "").toLowerCase())}" data-index="${index}">${esc(track.name || track.lang || "Track " + (index + 1))}</option>`).join("");
  if (!tracks.length) {
    select.disabled = true;
    document.getElementById("wsettingshint").textContent = "No alternative audio stream or embedded audio track was found for this episode.";
  } else {
    select.disabled = false;
    select.value = current >= 0 ? (tracks[current].lang || "").toLowerCase() : "auto";
    document.getElementById("wsettingshint").textContent = current < 0 && wAudioPick !== "auto" ? "Preferred audio unavailable; using available audio." : "";
    if (current >= 0 && wHls.audioTrack !== current) wHls.audioTrack = current;
  }
}

// wSubs fills the subtitle picker and shows the picked track: English by
// default.
function wSubs() {
  const vid = document.getElementById("wvid");
  const sel = document.getElementById("wsub");
  if (!vid || !sel) return;
  const tracks = Array.from(vid.textTracks).filter((t) => t.kind === "subtitles" || t.kind === "captions");
  let pick = wSubPick;
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

// Leaving the page leaves the room: app.js calls this as another page
// opens.
function watchLeave() {
  if (!wv || !session) return;
  where = where === "watch" ? "" : where;
  wStop();
  fetch("api/watch/leave", {
    method: "POST",
    keepalive: true,
    headers: { Authorization: "Bearer " + session, "Content-Type": "application/json" },
    body: JSON.stringify({ room }),
  }).catch(() => {});
  wv = null;
  wFull = false;
  document.body.classList.remove("wfull");
  setTimeout(soundApply);
}

addEventListener("pagehide", watchLeave);
