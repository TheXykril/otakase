// "Look around": the app's own screens, rendered from its widgets at each
// Site deploy (app/test/site_tour_test.dart), with every button the walk
// found as a press-able spot on the picture.
(() => {
  const $ = (id) => document.getElementById(id);
  const sec = $('tour');
  if (!sec || !window.fetch) return;
  const src = sec.dataset.src;
  const base = src.slice(0, src.lastIndexOf('/') + 1);
  const ver = src.includes('?') ? src.slice(src.indexOf('?')) : '';

  const dev = $('tour-dev'), scr = $('tour-scr'), img = $('tour-img'), spots = $('tour-spots');
  const backBtn = $('tour-back'), list = $('tour-list'), marks = $('tour-marks');
  const tabs = [...$('tour-devices').querySelectorAll('button')];
  const KIND = { tab: 'Tab', more: 'More', show: 'Show', player: 'Player', page: 'Page' };
  const HINT = {
    computer: 'Click or tap anything that lights up. Scroll on the screen to see further down.',
    phone: 'Tap anything that lights up. Swipe or scroll on the screen to see further down.',
    tv: 'Like a remote: arrow keys move, Enter presses, Backspace goes back. Clicking works too.',
  };

  let tour, device, screens, history = [], cur = null, page = 0, sel = -1;

  const url = (name) => `${base}${device}/${name}.webp${ver}`;
  const preload = (name) => { const i = new Image(); i.src = url(name); };

  function show(id, p = 0, push = true) {
    const s = screens[id];
    if (!s) return;
    if (push && cur) history.push([cur, page]);
    cur = id; page = Math.max(0, Math.min(p, s.pages.length - 1)); sel = -1;
    render();
  }

  function back() {
    const last = history.pop();
    if (last) { cur = last[0]; page = last[1]; sel = -1; render(); }
  }

  function render() {
    const s = screens[cur], pg = s.pages[page];
    dev.style.setProperty('--w', s.w);
    dev.style.setProperty('--h', s.h);
    dev.classList.toggle('wide', s.w > s.h);
    img.classList.add('loading');
    img.onload = () => img.classList.remove('loading');
    img.src = url(pg.img);
    img.alt = `Otakase on a ${device === 'computer' ? 'computer' : device === 'tv' ? 'TV' : 'phone'}: ${s.title}`;
    $('tour-title').textContent = s.title;
    $('tour-kind').textContent = KIND[s.kind] || '';
    backBtn.disabled = !history.length;
    spots.replaceChildren(...pg.links.map((l, i) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'hs';
      b.tabIndex = -1;
      b.dataset.i = i;
      b.setAttribute('aria-label', l.to === 'back' ? 'Back' : l.label);
      b.title = l.label;
      b.style.left = (l.x / s.w * 100) + '%';
      b.style.top = (l.y / s.h * 100) + '%';
      b.style.width = (l.w / s.w * 100) + '%';
      b.style.height = (l.h / s.h * 100) + '%';
      b.addEventListener('click', (e) => { e.stopPropagation(); press(i); });
      b.addEventListener('pointerenter', () => { if (l.to !== 'back' && screens[l.to]) preload(screens[l.to].pages[0].img); });
      return b;
    }));
    // Small spots last, so a button on a card wins over the card.
    [...spots.children].sort((a, b) => area(b) - area(a)).forEach((b) => spots.appendChild(b));
    $('tour-pages').hidden = s.pages.length < 2;
    $('tour-page').textContent = `${page + 1} / ${s.pages.length}`;
    $('tour-up').disabled = page === 0;
    $('tour-down').disabled = page === s.pages.length - 1;
    for (const b of list.querySelectorAll('button')) b.setAttribute('aria-current', b.dataset.id === cur);
    if (device === 'tv') select(firstSpot());
  }
  const area = (b) => parseFloat(b.style.width) * parseFloat(b.style.height);

  function press(i) {
    const l = screens[cur].pages[page].links[i];
    if (!l) return;
    if (l.to === 'back') back(); else show(l.to);
  }

  function turn(dir) {
    const s = screens[cur], p = page + dir;
    if (p < 0 || p >= s.pages.length) return false;
    page = p; sel = -1; render();
    return true;
  }

  // TV: one spot always has the focus ring, the arrows move it to the
  // nearest spot that way, like the app's own remote focus.
  const spotList = () => screens[cur].pages[page].links;
  function firstSpot() {
    const ls = spotList();
    // Start where the app would: the first button under the top bar.
    let best = -1, bestY = Infinity;
    ls.forEach((l, i) => {
      if (l.to === 'back') return;
      const y = l.y + (l.y < 90 ? 10000 : 0) + l.x / 10;
      if (y < bestY) { bestY = y; best = i; }
    });
    return best;
  }
  function select(i) {
    sel = i;
    spots.querySelectorAll('.hs').forEach((b) => b.classList.remove('sel'));
    if (i < 0) return;
    const b = spots.querySelector(`[data-i="${i}"]`);
    if (b) b.classList.add('sel');
  }
  function move(dx, dy) {
    const ls = spotList();
    if (sel < 0) { select(firstSpot()); return true; }
    const a = ls[sel], ax = a.x + a.w / 2, ay = a.y + a.h / 2;
    let best = -1, score = Infinity;
    ls.forEach((l, i) => {
      if (i === sel) return;
      const bx = l.x + l.w / 2, by = l.y + l.h / 2;
      const along = dx ? (bx - ax) * dx : (by - ay) * dy;
      if (along <= 4) return;
      const across = dx ? Math.abs(by - ay) : Math.abs(bx - ax);
      const sc = along + across * 2;
      if (sc < score) { score = sc; best = i; }
    });
    if (best >= 0) { select(best); return true; }
    if (dy && turn(dy)) return true;
    return false;
  }

  scr.addEventListener('keydown', (e) => {
    const k = e.key;
    if (k === 'Backspace' || k === 'Escape') { if (history.length) { back(); e.preventDefault(); } return; }
    if (k === 'Enter' || k === ' ') { if (sel >= 0) { press(sel); e.preventDefault(); } return; }
    const d = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[k];
    if (!d) return;
    e.preventDefault();
    if (device === 'tv') move(...d); else if (d[1]) turn(d[1]);
  });

  // A click that misses every spot shows where they are for a moment.
  let flashT = 0;
  scr.addEventListener('click', () => {
    dev.classList.add('marks');
    clearTimeout(flashT);
    flashT = setTimeout(() => { if (!marks.checked) dev.classList.remove('marks'); }, 900);
  });
  marks.addEventListener('change', () => dev.classList.toggle('marks', marks.checked));

  // Scrolling over the screen scrolls the app, not the website.
  let wheelAt = 0;
  scr.addEventListener('wheel', (e) => {
    const s = screens && screens[cur];
    if (!s || s.pages.length < 2 || Math.abs(e.deltaY) < Math.abs(e.deltaX)) return;
    const dir = e.deltaY > 0 ? 1 : -1;
    if ((dir > 0 && page === s.pages.length - 1) || (dir < 0 && page === 0)) return;
    e.preventDefault();
    e.stopPropagation();
    if (Date.now() - wheelAt < 450 || Math.abs(e.deltaY) < 3) { wheelAt = Date.now(); return; }
    wheelAt = Date.now();
    turn(dir);
  }, { passive: false });
  let touchY = null;
  scr.addEventListener('touchstart', (e) => { touchY = e.touches[0].clientY; }, { passive: true });
  scr.addEventListener('touchend', (e) => {
    if (touchY == null) return;
    const dy = touchY - e.changedTouches[0].clientY;
    touchY = null;
    if (Math.abs(dy) > 40) turn(dy > 0 ? 1 : -1);
  }, { passive: true });
  $('tour-up').addEventListener('click', () => turn(-1));
  $('tour-down').addEventListener('click', () => turn(1));
  backBtn.addEventListener('click', back);

  function buildList() {
    const groups = [['Tabs', 'tab'], ['Shows', 'show'], ['Player', 'player'], ['More', 'more'], ['Other', 'page']];
    list.replaceChildren(...groups.map(([name, kind]) => {
      const ids = Object.keys(screens).filter((id) => screens[id].kind === kind);
      if (!ids.length) return document.createTextNode('');
      const g = document.createElement('div');
      g.className = 'tgroup';
      const h = document.createElement('span');
      h.className = 'mono';
      h.textContent = name;
      g.appendChild(h);
      for (const id of ids) {
        const b = document.createElement('button');
        b.type = 'button';
        b.className = 'chip';
        b.dataset.id = id;
        b.textContent = screens[id].title;
        b.addEventListener('click', () => { if (id !== cur) show(id); });
        g.appendChild(b);
      }
      return g;
    }));
  }

  function pick(name) {
    if (!tour.devices[name]) return;
    device = name;
    screens = tour.devices[name].screens;
    history = []; cur = null;
    tabs.forEach((t) => { const on = t.dataset.device === name; t.setAttribute('aria-selected', on); t.tabIndex = on ? 0 : -1; });
    dev.className = 'tdev ' + name + (marks.checked ? ' marks' : '');
    $('tour-hint').textContent = HINT[name];
    buildList();
    show(tour.devices[name].start, 0, false);
  }
  tabs.forEach((t) => t.addEventListener('click', () => pick(t.dataset.device)));

  fetch(src).then((r) => r.ok ? r.json() : null).then((data) => {
    if (!data || !data.devices) return;
    tour = data;
    tabs.forEach((t) => { if (!data.devices[t.dataset.device]) t.hidden = true; });
    const ua = navigator.userAgent;
    const start = /Android TV|AFT|BRAVIA|SMART-TV|SmartTV/i.test(ua) ? 'tv' : /Android|iPhone|iPad/i.test(ua) ? 'phone' : 'computer';
    sec.hidden = false;
    pick(data.devices[start] ? start : Object.keys(data.devices)[0]);
  }).catch(() => {});
})();
