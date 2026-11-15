import 'dart:convert';

/// The page a browser shows once it has handed a sign-in back to the app,
/// on this computer (127.0.0.1) or on a TV in the same home network. Same
/// look as the site's and the account server's sign-in pages.
String loginResultPage({
  required bool ok,
  required String title,
  required String message,
}) {
  const escape = HtmlEscape(HtmlEscapeMode.element);
  final heading = escape.convert(title);
  final text = escape.convert(message);
  final mark = ok ? '&#10003;' : '!';
  return '''<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta name="robots" content="noindex,nofollow">
<title>$heading · Otakase</title>
<link rel="icon" href="data:,">
<style>
:root { color-scheme: dark; }
* { box-sizing: border-box; }
body { margin: 0; min-height: 100vh; display: grid; place-items: center; padding: 24px 16px;
  background: #0e0d0c; color: #ece4d6; font: 16px/1.6 Inter, system-ui, -apple-system, "Segoe UI", sans-serif; }
main { width: min(100%, 460px); }
.brand { display: flex; gap: 12px; align-items: center; font-weight: 700; letter-spacing: .08em; }
.brand span { display: inline-grid; place-items: center; width: 34px; height: 34px; border-radius: 10px;
  background: #d2492f; color: #0e0d0c; font-weight: 800; }
.mark { display: grid; place-items: center; width: 56px; height: 56px; margin-top: 40px; border-radius: 50%;
  border: 2px solid ${ok ? '#d2492f' : '#4a443d'}; color: ${ok ? '#d2492f' : '#b9b0a4'}; font-size: 26px; font-weight: 800; }
h1 { margin: 20px 0 8px; font-size: clamp(26px, 7vw, 36px); line-height: 1.15; }
p { margin: 0; color: #b9b0a4; }
</style>
</head>
<body>
<main>
<div class="brand"><span>任</span>OTAKASE</div>
<div class="mark" aria-hidden="true">$mark</div>
<h1>$heading</h1>
<p>$text</p>
</main>
</body>
</html>''';
}
