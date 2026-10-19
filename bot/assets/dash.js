// The dashboard's two touches that need a script: the top line that fills
// as you scroll (there is no scrollbar), and the faint お任せ · OTAKASE
// lines behind the page, the same as the games Activity.
"use strict";

(function scrollLine() {
  const root = document.documentElement;
  const update = () => {
    const room = root.scrollHeight - innerHeight;
    root.classList.toggle("long", room > 2);
    root.style.setProperty("--scrolled", room > 2 ? Math.min(100, (100 * scrollY) / room) + "%" : "0%");
  };
  addEventListener("scroll", update, { passive: true });
  addEventListener("resize", update);
  new ResizeObserver(update).observe(document.body);
  update();
})();

(function bgText() {
  const el = document.getElementById("bgtext");
  if (!el) return;
  const run = "お任せ　OTAKASE　".repeat(14);
  el.innerHTML = Array.from({ length: 46 }, (_, k) => `<div style="--t:${80 + (k % 5) * 12}s">${run}${run}</div>`).join("");
})();

// The phone page picker closes when you tap outside it or press Escape.
(function picker() {
  const d = document.querySelector("details.sub");
  if (!d) return;
  document.addEventListener("click", (e) => { if (d.open && !d.contains(e.target)) d.open = false; });
  document.addEventListener("keydown", (e) => { if (e.key === "Escape" && d.open) { d.open = false; d.querySelector("summary").focus(); } });
})();
