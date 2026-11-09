// "Try it here": the app itself, built for the web (app/demo), in a device
// frame. The page inside is drawn at the device's real size and scaled down
// to fit, so it lays out exactly as on that device.
(() => {
  const $ = (id) => document.getElementById(id);
  const sec = $('tour');
  if (!sec) return;
  const src = sec.dataset.src;
  const dev = $('tour-dev'), scr = $('tour-scr'), frame = $('tour-app');
  const tabs = [...$('tour-devices').querySelectorAll('button')];
  const SIZE = { computer: [1440, 900], phone: [412, 915], tv: [1600, 900] };
  const HINT = {
    computer: 'Click around: tabs, shows, settings and the player. The arrow at the top left goes back.',
    phone: 'Tap around and scroll or swipe, like on your phone. Esc is the Back button.',
    tv: 'Click the screen once, then use the arrow keys, Enter and Esc like a remote.',
  };
  let device = null, started = false;

  // The phone player asks for landscape, as on a real phone: turn the frame.
  let sideways = false;
  const size = () => {
    const [w, h] = SIZE[device];
    return device === 'phone' && sideways ? [h, w] : [w, h];
  };
  function shape() {
    const [w, h] = size();
    dev.className = 'tdev ' + device + (w > h && device === 'phone' ? ' wide' : '');
    dev.style.setProperty('--w', w);
    dev.style.setProperty('--h', h);
    frame.width = w; frame.height = h;
    fit();
  }
  addEventListener('message', (e) => {
    if (e.source !== frame.contentWindow || typeof e.data !== 'string') return;
    const turn = { 'otakase-demo:landscape': true, 'otakase-demo:portrait': false }[e.data];
    if (turn === undefined || turn === sideways) return;
    sideways = turn;
    shape();
  });

  function fit() {
    if (!device) return;
    const [w] = size();
    frame.style.transform = `scale(${scr.clientWidth / w})`;
  }
  function load() {
    started = true;
    // The app draws only as many pixels as end up on screen: the frame is
    // scaled down, and the demo draws without the graphics card.
    const dpr = Math.max(0.5, Math.min(devicePixelRatio, Math.round(devicePixelRatio * scr.clientWidth / size()[0] * 4) / 4));
    frame.src = src + (src.includes('?') ? '&' : '?') + 'device=' + device + '&dpr=' + dpr;
  }
  function pick(name) {
    if (name === device) return;
    device = name;
    sideways = false;
    tabs.forEach((t) => { const on = t.dataset.device === name; t.setAttribute('aria-selected', on); t.tabIndex = on ? 0 : -1; });
    $('tour-hint').textContent = HINT[name];
    shape();
    if (started) load();
  }
  tabs.forEach((t) => t.addEventListener('click', () => pick(t.dataset.device)));
  if (window.ResizeObserver) new ResizeObserver(fit).observe(scr); else addEventListener('resize', fit);

  // Only shown when the demo was deployed with the site.
  fetch(src).then((r) => {
    if (!r.ok) return;
    sec.hidden = false;
    const ua = navigator.userAgent;
    const tv = /Android TV|AFT|BRAVIA|SMART-TV|SmartTV/i.test(ua);
    // On a phone the computer and TV would be too small to use: only the
    // phone is offered there.
    if (!tv && (/Android|iPhone/i.test(ua) || matchMedia('(max-width: 760px)').matches)) {
      $('tour-devices').hidden = true;
      pick('phone');
      $('tour-hint').textContent = 'Tap around and scroll, just like in the app.';
    } else {
      pick(tv ? 'tv' : /iPad/i.test(ua) ? 'phone' : 'computer');
    }
    // The app is a few megabytes: start it once the section is close.
    if (!('IntersectionObserver' in window)) { load(); return; }
    const io = new IntersectionObserver((es) => {
      if (es.some((e) => e.isIntersecting)) { io.disconnect(); load(); }
    }, { rootMargin: '600px 0px' });
    io.observe(sec);
  }).catch(() => {});
})();
