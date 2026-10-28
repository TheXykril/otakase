import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

bool privateAddress(InternetAddress a) {
  if (a.type != InternetAddressType.IPv4) return false;
  final b = a.rawAddress;
  return b[0] == 10 ||
      b[0] == 192 && b[1] == 168 ||
      b[0] == 172 && b[1] >= 16 && b[1] <= 31;
}

Future<String?> lanAddress() async {
  for (final interface in await NetworkInterface.list(
    type: InternetAddressType.IPv4,
  )) {
    for (final a in interface.addresses) {
      if (privateAddress(a)) return a.address;
    }
  }
  return null;
}

String randomSecret() =>
    base64UrlEncode(List.generate(32, (_) => Random.secure().nextInt(256)))
        .replaceAll('=', '');
String htmlText(String value) => const HtmlEscape().convert(value);

class LanPage {
  LanPage({this.findAddress = lanAddress, this.allowLoopback = false});
  final Future<String?> Function() findAddress;
  final bool allowLoopback;
  HttpServer? _server;
  Timer? _expiry;
  Uri? url;
  final _input = StreamController<String>.broadcast();
  Stream<String> get input => _input.stream;
  Future<Uri> open({
    required String title,
    required String message,
    bool textInput = false,
    String initial = '',
    int maxLength = 500,
    Duration lifetime = const Duration(minutes: 10),
  }) async {
    await closeServer();
    final address = await findAddress();
    if (address == null) {
      throw StateError(
        'Connect the TV and phone to the same Wi-Fi, then try again.',
      );
    }
    final host = InternetAddress.tryParse(address);
    if (host == null ||
        !(privateAddress(host) || allowLoopback && host.isLoopback)) {
      throw StateError('A local network address is required.');
    }
    final server = await HttpServer.bind(InternetAddress.anyIPv4, 0);
    _server = server;
    final secret = randomSecret();
    final path = '/input/$secret';
    url = Uri(scheme: 'http', host: address, port: server.port, path: path);
    _expiry = Timer(lifetime, closeServer);
    server.listen((r) async {
      final peer = r.connectionInfo?.remoteAddress;
      final origin = r.headers.value('origin');
      if (peer == null ||
          !(privateAddress(peer) || allowLoopback && peer.isLoopback) ||
          r.uri.path != path ||
          origin != null && origin != url?.origin) {
        r.response.statusCode = 403;
        await r.response.close();
        return;
      }
      r.response.headers.set('Cache-Control', 'no-store');
      r.response.headers.set('X-Content-Type-Options', 'nosniff');
      r.response.headers.set(
        'Content-Security-Policy',
        "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'",
      );
      String body;
      if (r.method == 'POST' && textInput) {
        if ((r.contentLength > maxLength * 16 + 1024) ||
            !(r.headers.contentType?.mimeType ==
                'application/x-www-form-urlencoded')) {
          r.response.statusCode = 413;
          await r.response.close();
          return;
        }
        try {
          var length = 0;
          final chunks = <int>[];
          await for (final chunk in r) {
            length += chunk.length;
            if (length > maxLength * 16 + 1024) {
              throw const FormatException('Input too long');
            }
            chunks.addAll(chunk);
          }
          final value =
              Uri.splitQueryString(utf8.decode(chunks))['text']?.trim() ?? '';
          if (value.runes.length > maxLength) {
            throw const FormatException('Input too long');
          }
          if (!_input.isClosed) _input.add(value);
          body = '<p>Sent to your TV. You can return to Otakase.</p>';
        } catch (_) {
          r.response.statusCode = 400;
          body = '<p>Check your text and try again.</p>';
        }
      } else if (r.method == 'GET') {
        body =
            '<p>${htmlText(message)}</p>${textInput ? '<form method="post"><textarea name="text" rows="9" maxlength="$maxLength" autofocus>${htmlText(initial)}</textarea><button>Send to TV</button></form>' : '<a href="https://otakase.xyverion.com/discord/">Open the Otakase Discord</a>'}';
      } else {
        r.response.statusCode = 405;
        await r.response.close();
        return;
      }
      r.response.headers.contentType = ContentType.html;
      r.response.write(
        '<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Otakase · ${htmlText(title)}</title><style>body{background:#151411;color:#e9e2d4;font:18px system-ui;margin:0;padding:7vh 7vw;max-width:620px}h1{font-family:serif}p{line-height:1.6;white-space:pre-wrap}a{color:#e78b71}textarea{box-sizing:border-box;width:100%;padding:14px;background:#211e19;color:inherit;font:inherit;border:1px solid #5f5144;border-radius:8px}button{margin-top:18px;background:#d2492f;color:white;border:0;padding:14px 24px;border-radius:8px;font:inherit}</style><small>OTAKASE</small><h1>${htmlText(title)}</h1>$body</html>',
      );
      await r.response.close();
    });
    return url!;
  }

  Future<void> closeServer() async {
    _expiry?.cancel();
    _expiry = null;
    final s = _server;
    _server = null;
    url = null;
    await s?.close(force: true);
  }

  Future<void> close() async {
    await closeServer();
    await _input.close();
  }
}
