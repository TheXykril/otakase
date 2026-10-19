// otakase site: install tabs, latest version, and the cover art shown through the hero letters.
(() => {
  'use strict';

  // ---- install tabs
  const tabs = [...document.querySelectorAll('[role="tab"]')];
  function select(tab) {
    for (const t of tabs) {
      const on = t === tab;
      t.setAttribute('aria-selected', on);
      t.tabIndex = on ? 0 : -1;
      document.getElementById(t.getAttribute('aria-controls')).hidden = !on;
    }
  }
  tabs.forEach((t, i) => {
    t.addEventListener('click', () => select(t));
    t.addEventListener('keydown', (e) => {
      const step = { ArrowRight: 1, ArrowLeft: -1 }[e.key];
      if (!step) return;
      const next = tabs[(i + step + tabs.length) % tabs.length];
      select(next);
      next.focus();
    });
  });
  // Open on the visitor's own system; picking a system here also switches
  // the menu shots to it.
  const tabFor = { linux: 't-linux', arch: 't-arch', macos: 't-macos', windows: 't-windows' };
  tabs.forEach((t) => t.addEventListener('click', () => {
    const os = Object.keys(tabFor).find((k) => tabFor[k] === t.id);
    document.dispatchEvent(new CustomEvent('otakase-os', { detail: os === 'arch' ? 'linux' : os }));
  }));
  document.addEventListener('otakase-shot', (e) => {
    const os = e.detail === 'rofi' ? 'linux' : e.detail;
    const current = tabs.find((t) => t.getAttribute('aria-selected') === 'true');
    // Arch is Linux too: leave it open when Linux is picked.
    if (os === 'linux' && current && current.id === tabFor.arch) return;
    select(document.getElementById(tabFor[os]));
  });
  const own = document.getElementById(tabFor[visitorOS()]);
  if (own) select(own);

  // ---- latest version
  fetch('https://api.github.com/repos/TheXykril/otakase/releases/latest')
    .then((r) => (r.ok ? r.json() : Promise.reject(r.status)))
    .then((rel) => {
      if (rel.tag_name) document.getElementById('ver').textContent = 'VERSION ' + rel.tag_name.replace(/^v/, '');
    })
    .catch(() => {});

  // ---- covers
  const DELAY = 5000; // Xykril picked 5 s between covers
  const COUNT = 15;
  const CACHE_KEY = 'otakase-covers';
  const CACHE_FOR = 6 * 60 * 60 * 1000;
  // Shown when AniList can't be reached: otakase's own player, so the letters are never empty.
  const FALLBACK = ['img/scene-1.jpg', 'img/scene-2.jpg'].map((img) => ({ img, title: '' }));

  const QUERY = `query($n:Int){Page(perPage:$n){media(type:ANIME,sort:TRENDING_DESC,isAdult:false){
    title{english romaji} coverImage{extraLarge} siteUrl}}}`;

  function readCache() {
    try {
      const c = JSON.parse(localStorage.getItem(CACHE_KEY) || 'null');
      if (c && Date.now() - c.at < CACHE_FOR && c.items.length) return c.items;
    } catch (e) { /* storage blocked or corrupt */ }
    return null;
  }

  async function trending() {
    const cached = readCache();
    if (cached) return cached;
    const r = await fetch('https://graphql.anilist.co', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ query: QUERY, variables: { n: COUNT } }),
    });
    if (!r.ok) throw new Error('AniList ' + r.status);
    const items = (await r.json()).data.Page.media
      .filter((m) => m.coverImage && m.coverImage.extraLarge)
      .map((m) => ({ img: m.coverImage.extraLarge, title: m.title.english || m.title.romaji }));
    if (!items.length) throw new Error('no covers');
    try { localStorage.setItem(CACHE_KEY, JSON.stringify({ at: Date.now(), items })); } catch (e) { /* fine */ }
    return items;
  }

  const fills = [...document.querySelectorAll('.fill')];
  const now = document.getElementById('now');

  function show(item) {
    for (const f of fills) {
      const [a, b] = f.querySelectorAll('.l');
      const [on, off] = a.classList.contains('on') ? [a, b] : [b, a];
      off.style.backgroundImage = `url("${item.img}")`;
      off.classList.add('on');
      on.classList.remove('on');
    }
    now.textContent = item.title ? 'Now showing · ' + item.title : '';
  }

  function load(src) {
    return new Promise((resolve) => {
      const i = new Image();
      i.onload = () => resolve(true);
      i.onerror = () => resolve(false);
      i.src = src;
    });
  }

  async function run(items) {
    let n = 0;
    // start on a cover that is already in the browser, then keep one step ahead
    for (; n < items.length; n++) if (await load(items[n].img)) break;
    if (n === items.length) return false;
    show(items[n]);
    let ready = load(items[(n + 1) % items.length].img);
    setInterval(async () => {
      if (document.hidden) return;
      const ok = await ready;
      n = (n + 1) % items.length;
      if (ok) show(items[n]);
      ready = load(items[(n + 1) % items.length].img);
    }, DELAY);
    return true;
  }

  trending()
    .then((items) => run(items))
    .then((ok) => ok || run(FALLBACK))
    .catch(() => run(FALLBACK));
})();

// visitorOS guesses the visitor's system: windows, macos or linux. Phones get
// the desktop they are most likely to install on: an iPhone a Mac, Android Linux.
function visitorOS() {
  const ua = navigator.userAgent || '';
  const platform = (navigator.userAgentData && navigator.userAgentData.platform) || navigator.platform || '';
  if (/Win/i.test(platform) || /Windows/i.test(ua)) return 'windows';
  if (/Mac|iPhone|iPad|iPod/i.test(platform) || /Macintosh|iPhone|iPad/i.test(ua)) return 'macos';
  return 'linux';
}

// ---- menu shots: one system at a time
(() => {
  const buttons = [...document.querySelectorAll('.os-pick button')];
  const shots = [...document.querySelectorAll('.os-shot')];
  const show = (name) => {
    buttons.forEach((b) => b.setAttribute('aria-pressed', b.dataset.shot === name));
    shots.forEach((s) => { s.hidden = s.dataset.shot !== name; });
  };
  buttons.forEach((b) => b.addEventListener('click', () => {
    show(b.dataset.shot);
    document.dispatchEvent(new CustomEvent('otakase-shot', { detail: b.dataset.shot }));
  }));
  document.addEventListener('otakase-os', (e) => show(e.detail));
  show(visitorOS());
})();
