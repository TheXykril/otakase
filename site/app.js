(() => {
  const $ = (id) => document.getElementById(id);

  // Always open at the start of the story. Browsers otherwise put a reload
  // back where the last visit stopped, and a #download left in the address by
  // the Download button reopens the page down at the downloads.
  if ('scrollRestoration' in history) history.scrollRestoration = 'manual';
  if (location.hash) history.replaceState(null, '', location.pathname + location.search);
  const toTop = () => window.scrollTo(0, 0);
  toTop();
  addEventListener('pageshow', (e) => { if (e.persisted) toTop(); });
  // Fonts and pictures arriving can still move things; until the visitor
  // scrolls on purpose, keep the page at the top.
  let moved = false;
  for (const ev of ['wheel', 'touchstart', 'keydown', 'pointerdown']) addEventListener(ev, () => { moved = true; }, { once: true, passive: true });
  const settle = () => { if (!moved) toTop(); };
  const loaded = new Promise((done) => {
    if (document.readyState === 'complete') done(); else addEventListener('load', done, { once: true });
  });
  const fonts = document.fonts ? document.fonts.ready : Promise.resolve();
  Promise.race([Promise.all([loaded, fonts]), new Promise((done) => setTimeout(done, 4000))]).then(() => {
    settle();
    requestAnimationFrame(() => requestAnimationFrame(() => {
      settle();
      document.documentElement.classList.add('snapping');
    }));
  });
  const pin = $('top'), stage = $('stage'), hero = $('hero');
  const els = { tv: $('tv'), pc: $('pc'), ph: $('ph') };
  // height / width of each device frame
  const ASPECT = { pc: .7, tv: .7, ph: 2.2 }; // replaced by real measurements in measure()
  const SCR = {};
  function measure() {
    for (const k in els) { const el = els[k]; el.style.width = '1000px'; el.style.fontSize = '10px'; ASPECT[k] = el.offsetHeight / 1000; const sc = el.querySelector('.scr'); SCR[k] = sc.offsetWidth / 1000 / +sc.dataset.w; }
  }
  const caps = [...document.querySelectorAll('.cap')];
  const N = 5; // scenes: hero, story, skip, couch, everywhere
  const ease = (t) => t * t * (3 - 2 * t);
  const clamp = (v, a, b) => Math.min(b, Math.max(a, v));

  // One snap point per scene, so every scroll lands on a whole scene. They
  // are in the page from the start (style.css places them), so the browser
  // never sees them move while the page loads and re-snaps further down.
  const snaps = [...pin.querySelectorAll('.snap')];

  // Size a device to fit a box, keeping its shape.
  function fit(k, box, share = 1) {
    const w = Math.min(box.w * share, box.h / ASPECT[k]);
    return { w, h: w * ASPECT[k] };
  }
  const centre = (box) => ({ x: box.x + box.w / 2, y: box.y + box.h / 2 });

  // On a phone a wide screen at full width leaves the bottom empty, so in
  // those scenes the screen sits in the middle and the words go under it.
  function below(s, k, i, W, H) {
    const cap = caps[i - 1];
    const w = W - 24, h = w * ASPECT[k], gap = 28;
    const y0 = Math.max(24, (H - h - gap - cap.offsetHeight) / 2);
    cap.style.top = (y0 + h + gap) + 'px';
    s[k] = { x: W / 2, y: y0 + h / 2, w, o: 1 };
  }

  let S = []; // per scene: { pc:{x,y,w,o}, ... } with x,y = centre in px
  function layout() {
    measure();
    const W = stage.clientWidth, H = stage.clientHeight;
    const narrow = W < 760;
    caps.forEach((c) => { c.style.top = ''; });
    const top = narrow ? 24 : 48;
    const span = pin.offsetHeight - H;
    snaps.forEach((s, i) => { s.style.top = (i * span / (N - 1)) + 'px'; });

    // where screens may go in each scene, clear of the words
    const heroBottom = hero.offsetTop + hero.offsetHeight + (narrow ? 28 : 44);
    const capBottom = (i) => { const c = caps[i - 1]; return c.offsetTop + c.offsetHeight + 28; };
    const side = narrow ? 16 : 0;
    const box = (i) => {
      if (i === 0) return { x: W * (narrow ? .04 : .14), y: heroBottom, w: W * (narrow ? .92 : .72), h: Math.max(H - heroBottom, 200) * 1.6 };
      if (narrow) { const y = capBottom(i); return { x: side, y, w: W - 2 * side, h: H - y - 24 }; }
      return { x: W * .47, y: top, w: W * .47 - 56, h: H - top - 56 };
    };
    const hidden = (b, k) => { const c = centre(b), f = fit(k, b, .5); return { x: c.x, y: c.y + 40, w: f.w, o: 0 }; };

    S = [];
    for (let i = 0; i < N; i++) {
      const b = box(i), c = centre(b), s = {};
      if (i === 0) {
        // computer and phone side by side, peeking up from under the buttons
        const gap = narrow ? 12 : 28;
        const pw = b.w * (narrow ? .24 : .2), cw = b.w - pw - gap;
        s.pc = { x: b.x + cw / 2, y: b.y + cw * ASPECT.pc / 2, w: cw, o: 1 };
        s.ph = { x: b.x + cw + gap + pw / 2, y: b.y + pw * ASPECT.ph / 2, w: pw, o: 1 };
        s.tv = { x: c.x, y: b.y + 200, w: cw, o: 0 };
      } else if (i === 1) {
        const f = fit('ph', b, narrow ? .62 : .5);
        s.ph = { x: c.x, y: narrow ? b.y + f.h / 2 : c.y, w: f.w, o: 1 };
        s.pc = hidden(b, 'pc'); s.tv = hidden(b, 'tv');
      } else if (i === 2) {
        const f = fit('pc', b);
        if (narrow) below(s, 'pc', i, W, H);
        else s.pc = { x: c.x, y: c.y, w: f.w, o: 1 };
        s.ph = hidden(b, 'ph'); s.tv = hidden(b, 'tv');
      } else if (i === 3) {
        const f = fit('tv', b);
        if (narrow) below(s, 'tv', i, W, H);
        else s.tv = { x: c.x, y: c.y, w: f.w, o: 1 };
        s.pc = hidden(b, 'pc'); s.ph = hidden(b, 'ph');
      } else if (narrow || b.h > b.w * .7) {
        // TV on top, computer and phone side by side under it
        const gap = 14;
        const pcw0 = .7, phw0 = .3 - gap / b.w;
        let tvW = Math.min(b.w, (b.h - gap) / (ASPECT.tv + Math.max(pcw0 * ASPECT.pc, phw0 * ASPECT.ph)));
        tvW = Math.min(tvW, b.w);
        const rowW = tvW;
        const phW = (rowW - gap) * .27, pcW = rowW - gap - phW;
        const tvH = tvW * ASPECT.tv, rowH = Math.max(pcW * ASPECT.pc, phW * ASPECT.ph);
        const x0 = c.x - rowW / 2, y0 = narrow ? b.y : c.y - (tvH + gap + rowH) / 2;
        s.tv = { x: c.x, y: y0 + tvH / 2, w: tvW, o: 1 };
        const base = y0 + tvH + gap + rowH;
        s.pc = { x: x0 + pcW / 2, y: base - pcW * ASPECT.pc / 2, w: pcW, o: 1 };
        s.ph = { x: x0 + pcW + gap + phW / 2, y: base - phW * ASPECT.ph / 2, w: phW, o: 1 };
      } else {
        // all three in a row, bottoms lined up, no overlap
        const gap = narrow ? 10 : 22;
        const tvw = 1, pcw = .82, phw = .2; // relative widths
        let unit = (b.w - 2 * gap) / (tvw + pcw + phw);
        unit = Math.min(unit, b.h / (phw * ASPECT.ph), b.h / (tvw * ASPECT.tv));
        const rowW = unit * (tvw + pcw + phw) + 2 * gap;
        const tallest = Math.max(unit * tvw * ASPECT.tv, unit * phw * ASPECT.ph);
        const base = (narrow ? b.y : c.y - tallest / 2) + tallest;
        let x = c.x - rowW / 2;
        for (const [k, rw] of [['tv', tvw], ['pc', pcw], ['ph', phw]]) {
          const w = unit * rw, h = w * ASPECT[k];
          s[k] = { x: x + w / 2, y: base - h / 2, w, o: 1 };
          x += w + gap;
        }
      }
      S.push(s);
    }
  }

  // dots
  const dots = $('dots');
  const go = (i) => window.scrollTo({ top: pin.offsetTop + snaps[i].offsetTop, behavior: 'smooth' });
  ['Start', 'Back to your story', 'Straight to the good part', 'Made for the couch', 'One list, every screen'].forEach((label, i) => {
    const b = document.createElement('button');
    b.type = 'button';
    b.setAttribute('aria-label', label);
    b.addEventListener('click', () => go(i));
    dots.appendChild(b);
  });

  function frame() {
    const H = stage.clientHeight;
    const span = pin.offsetHeight - H;
    const p = clamp((scrollY - pin.offsetTop) / span, 0, 1) * (N - 1);
    const a = Math.min(N - 2, Math.floor(p)), t = ease(p - a);
    for (const k in els) {
      const A = S[a][k], B = S[a + 1][k];
      const m = (q) => A[q] + (B[q] - A[q]) * t;
      const el = els[k];
      el.style.width = m('w') + 'px';
      el.style.fontSize = (m('w') / 100) + 'px';
      el.style.setProperty('--s', m('w') * SCR[k]);
      el.style.transform = `translate(-50%,-50%) translate(${m('x')}px,${m('y')}px)`;
      el.style.left = el.style.top = '0';
      el.style.opacity = m('o');
    }
    // the computer screen turns into the player for the skip scene
    $('img-play').classList.toggle('on', Math.abs(p - 2) < .6);
    const h = clamp(p * 2.2, 0, 1);
    hero.style.opacity = 1 - h;
    hero.style.transform = `translateY(${-h * 50}px)`;
    hero.style.visibility = h >= 1 ? 'hidden' : 'visible';
    $('hint').style.opacity = 1 - clamp(p * 4, 0, 1);
    caps.forEach((c) => {
      const d = p - +c.dataset.i;
      const o = clamp(1 - Math.abs(d) * 2.2, 0, 1);
      c.style.opacity = o;
      c.style.visibility = o ? 'visible' : 'hidden';
      c.classList.toggle('live', o > .5);
      c.style.translate = `0 ${-d * 36}px`;
    });
    const cur = clamp(Math.round(p), 0, N - 1);
    [...dots.children].forEach((d, i) => d.classList.toggle('on', i === cur));
  }
  let raf = 0;
  const kick = () => { if (!raf) raf = requestAnimationFrame(() => { raf = 0; frame(); }); };
  addEventListener('scroll', kick, { passive: true });
  addEventListener('resize', () => { layout(); kick(); });
  if (document.fonts) document.fonts.ready.then(() => { layout(); frame(); });
  layout();
  frame();

  // Real app screens: one random show per visit, kept until the page reloads.
  const SHOTS = [212888, 195604, 21, 195539, 195516, 269, 191788, 208025];
  const pickShow = SHOTS[Math.floor(Math.random() * SHOTS.length)];
  for (const [k, n] of [['pc', 'computer'], ['play', 'player'], ['ph', 'phone'], ['tv', 'tv']]) $('img-' + k).src = `img/shots/${pickShow}-${n}.webp`;
  $('img-mpc').src = `img/shots/${pickShow}-computer.webp`;
  $('img-mph').src = `img/shots/${pickShow}-phone.webp`;

  // one part per wheel or key gesture, however strong the touchpad flick
  const stops = () => [...snaps.map((x) => pin.offsetTop + x.offsetTop), ...[...document.querySelectorAll('.after')].map((e) => e.offsetTop)];
  let busy = false, quietAt = 0, timer = 0;
  function step(dir) {
    const list = stops(), y = scrollY;
    let i = list.findIndex((v) => v > y + 4);
    if (i < 0) i = list.length;
    const cur = list.reduce((best, v, k) => Math.abs(v - y) < Math.abs(list[best] - y) ? k : best, 0);
    const target = list[Math.max(0, Math.min(list.length - 1, cur + dir))];
    if (Math.abs(target - y) < 2) return;
    busy = true;
    window.scrollTo({ top: target, behavior: 'smooth' });
    clearTimeout(timer);
    timer = setTimeout(function wait() {
      // stay locked until the touchpad stops sending its inertia
      if (Date.now() - quietAt < 160) { timer = setTimeout(wait, 80); return; }
      busy = false;
    }, 650);
  }
  addEventListener('wheel', (e) => {
    if (e.ctrlKey) return;
    if (Math.abs(e.deltaY) < Math.abs(e.deltaX)) return;
    e.preventDefault();
    quietAt = Date.now();
    if (busy || Math.abs(e.deltaY) < 3) return;
    step(e.deltaY > 0 ? 1 : -1);
  }, { passive: false });
  addEventListener('keydown', (e) => {
    if (e.target.closest && e.target.closest('input,textarea,[role=tab]')) return;
    const dir = { PageDown: 1, ArrowDown: 1, ' ': e.shiftKey ? -1 : 1, PageUp: -1, ArrowUp: -1 }[e.key];
    if (!dir) return;
    e.preventDefault();
    if (!busy) step(dir);
  });

  // downloads
  const R = 'https://github.com/TheXykril/otakase/releases/latest/download/';
  const P = {
    android: ['One app for phones and Android TV', 'Android 7.0 or newer. Android asks once to allow installing apps from your browser or file manager. Updates arrive inside the app.', 'Get the APK', R + 'otakase-android.apk'],
    windows: ['Otakase for Windows', 'Installs for your user only, no administrator needed, and adds Otakase to the Start menu.', 'Get the installer', R + 'otakase-app-windows-setup.exe'],
    macos: ['Otakase for macOS', 'Unzip and move Otakase to Applications. Runs on Apple Silicon and Intel. The first time, right-click it and choose Open.', 'Get the app', R + 'otakase-app-macos.zip'],
    linux: ['Otakase for Linux', 'One AppImage for any x86_64 distribution, with its player inside. Make it executable with <code>chmod +x</code> and run it.', 'Get the AppImage', R + 'otakase-app-linux-x86_64.AppImage'],
  };
  const tabs = [...document.querySelectorAll('[role=tab]')];
  function pick(key) {
    tabs.forEach((t) => { const on = t.id === 't-' + key; t.setAttribute('aria-selected', on); t.tabIndex = on ? 0 : -1; });
    const [h, txt, btn, url] = P[key];
    $('p-h').textContent = h; $('p-t').innerHTML = txt; $('p-b').textContent = btn; $('p-a').href = url;
  }
  tabs.forEach((t) => t.addEventListener('click', () => pick(t.id.slice(2))));
  const ua = navigator.userAgent;
  const os = /Android/i.test(ua) ? 'android' : /Win/i.test(ua) ? 'windows' : /Mac/i.test(ua) && !/iPhone|iPad/.test(ua) ? 'macos' : /Linux/i.test(ua) ? 'linux' : 'android';
  pick(os);
  const names = { android: 'Android', windows: 'Windows', macos: 'macOS', linux: 'Linux' };
  $('hero-dl').querySelector('span').textContent = 'Download for ' + names[os];
  // Go to the downloads without writing #download into the address.
  $('hero-dl').addEventListener('click', (e) => {
    e.preventDefault();
    window.scrollTo({ top: $('download').offsetTop, behavior: 'smooth' });
  });
})();
