// The few web pages the account Worker shows: sign-in, TV approval and
// results. Same look as the site's sign-in page.

import { escapeHTML, randomToken } from './util.js';

const style = `
:root { color-scheme: dark; }
* { box-sizing: border-box; }
body { margin: 0; min-height: 100vh; display: grid; place-items: center; padding: 24px 16px;
  background: #0e0d0c; color: #ece4d6; font: 16px/1.6 Inter, system-ui, -apple-system, "Segoe UI", sans-serif; }
main { width: min(100%, 460px); }
.brand { display: flex; gap: 12px; align-items: center; font-weight: 700; letter-spacing: .08em; }
.brand span { display: inline-grid; place-items: center; width: 34px; height: 34px; border-radius: 10px; background: #d2492f; color: #0e0d0c; font-weight: 800; }
h1 { margin: 36px 0 8px; font-size: clamp(26px, 7vw, 36px); line-height: 1.15; }
p { color: #b9b0a4; }
b { color: #ece4d6; letter-spacing: .08em; }
.buttons { display: grid; gap: 12px; margin: 24px 0 8px; }
button, .button { display: inline-block; text-align: center; text-decoration: none; border: 1px solid #d2492f; border-radius: 999px;
  padding: 13px 22px; min-height: 48px; font: inherit; font-weight: 700; background: #d2492f; color: #0e0d0c; cursor: pointer; }
button.google { background: transparent; color: #ece4d6; border-color: #4a443d; }
button:disabled { opacity: .5; cursor: default; }
button:focus-visible, .button:focus-visible, a:focus-visible { outline: 2px solid #f28b74; outline-offset: 4px; }
.turnstile { margin-top: 20px; min-height: 65px; }
.small { font-size: 14px; }
a { color: #f28b74; }
`;

/** An HTML page. Scripts in body get the page's nonce. */
export function page(env, title, body, status = 200, opts = {}) {
  const nonce = randomToken(16);
  const html = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta name="robots" content="noindex,nofollow">
<title>${escapeHTML(title)} · Otakase</title>
<style>${style}</style>
</head>
<body>
<main>
<div class="brand"><span>任</span>OTAKASE</div>
${body.replace(/<script/g, `<script nonce="${nonce}"`)}
</main>
</body>
</html>`;
  const turnstile = opts.turnstile ? ' https://challenges.cloudflare.com' : '';
  return new Response(html, {
    status,
    headers: {
      'content-type': 'text/html; charset=utf-8',
      'cache-control': 'no-store',
      'x-frame-options': 'DENY',
      'referrer-policy': 'no-referrer',
      'content-security-policy': `default-src 'none'; style-src 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; script-src 'nonce-${nonce}'${turnstile}; frame-src${turnstile || " 'none'"}; base-uri 'none'; form-action 'none'; frame-ancestors 'none'`,
    },
  });
}
