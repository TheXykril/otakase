import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'models.dart';
import 'tv_remote.dart';

// The phone remote away from home goes through the Discord bot
// (bot/remoterelay.go). A TV or computer linked to Discord keeps one
// WebSocket open there as a host; a phone linked to the same Discord
// account controls it through the bot when the local network can't reach
// it, and gets a local pairing without Allow, since only the member's own
// apps reach their TVs.

/// Opens a WebSocket to the bot at [url] with the link [token].
typedef RelayConnect = Future<WebSocket> Function(Uri url, String token);

Future<WebSocket> relaySocket(Uri url, String token) async {
  final ws = await WebSocket.connect(
    url.toString(),
    headers: {'Authorization': 'Bearer $token'},
  ).timeout(const Duration(seconds: 10));
  ws.pingInterval = const Duration(seconds: 25);
  return ws;
}

/// The bot's address for [path] with [query], as ws:// or wss://.
Uri relayUrl(String base, String path, Map<String, String> query) {
  final u = Uri.parse(base.replaceAll(RegExp(r'/+$'), ''));
  return u.replace(
    scheme: u.scheme == 'http' ? 'ws' : 'wss',
    path: '${u.path}$path',
    queryParameters: query,
  );
}

/// The TV's side: stays connected to the bot while the profile is linked,
/// answers the member's phones with what the TV plays and runs their
/// controls.
class RelayHost {
  RelayHost({
    required this.remote,
    required this.base,
    required this.token,
    this.connect = relaySocket,
  });
  final TVRemote remote;
  final String base;
  final String Function() token;
  final RelayConnect connect;
  WebSocket? _socket;
  Timer? _retry, _push;
  bool _closed = false;
  int _fails = 0;
  final _phones = <String>{};
  String? _last;

  bool get connected => _socket != null;

  void start() => unawaited(_connect());

  Future<void> _connect() async {
    if (_closed) return;
    final link = token();
    if (link.isEmpty) return;
    await remote.initialize();
    try {
      final ws = await connect(
        relayUrl(base, '/v1/remote/host', {
          'id': remote.id,
          'name': remote.name,
        }),
        link,
      );
      if (_closed) {
        await ws.close();
        return;
      }
      _socket = ws;
      _fails = 0;
      ws.listen(
        (data) => unawaited(_frame(data)),
        onDone: () => _lost(ws),
        onError: (Object _) {},
        cancelOnError: true,
      );
      _push = Timer.periodic(
        const Duration(milliseconds: 500),
        (_) => _status(),
      );
    } catch (_) {
      _again();
    }
  }

  void _lost(WebSocket ws) {
    if (_socket != ws) return;
    _socket = null;
    _push?.cancel();
    _phones.clear();
    _last = null;
    _again();
  }

  void _again() {
    if (_closed) return;
    _fails++;
    _retry?.cancel();
    _retry = Timer(
      Duration(seconds: min(300, 5 * (1 << min(_fails - 1, 6)))),
      () => unawaited(_connect()),
    );
  }

  void _send(String to, Json data) =>
      _socket?.add(jsonEncode({'to': to, 'data': data}));

  void _status({bool always = false}) {
    if (_phones.isEmpty) return;
    final now = jsonEncode(remote.liveStatus());
    if (!always && now == _last) return;
    _last = now;
    _socket?.add('{"to":"","data":$now}');
  }

  Future<void> _frame(dynamic raw) async {
    if (raw is! String) return;
    final Json j;
    try {
      j = (jsonDecode(raw) as Map).cast<String, dynamic>();
    } catch (_) {
      return;
    }
    final from = '${j['from'] ?? ''}';
    switch (j['type']) {
      case 'join':
        _phones.add(from);
        _status(always: true);
        return;
      case 'leave':
        _phones.remove(from);
        return;
    }
    final data = j['data'];
    if (data is! Map || from.isEmpty) return;
    final message = data.cast<String, dynamic>();
    if (message['type'] == 'grant') {
      final grant = await remote.grant('${message['name'] ?? 'Phone'}');
      _send(from, {'type': 'grant', ...grant});
      return;
    }
    final (code, reply) = remote.control(
      message['action'] as String? ?? '',
      (message['args'] as Map? ?? {}).cast<String, dynamic>(),
    );
    _send(from, {
      'type': 'ack',
      'seq': message['seq'],
      if (code >= 300) 'error': reply['error'],
    });
  }

  Future<void> close() async {
    _closed = true;
    _retry?.cancel();
    _push?.cancel();
    await _socket?.close();
    _socket = null;
  }
}

/// The phone's side: the member's TVs online through the bot, and a
/// connection to one of them.
class RelayClient {
  RelayClient({
    required this.base,
    required this.token,
    required this.list,
    this.connect = relaySocket,
  });
  final String base;
  final String Function() token;

  /// GET /v1/remote/tvs through the linked Discord bot.
  final Future<Json> Function() list;
  final RelayConnect connect;

  bool get available => token().isNotEmpty;

  /// The TVs on this account that are online now.
  Future<List<TVDevice>> tvs() async {
    if (!available) return const [];
    final j = await list();
    return [
      for (final t in j['tvs'] as List? ?? const [])
        if (t is Map && t['id'] is String)
          TVDevice(
            id: t['id'] as String,
            name: remoteName('${t['name'] ?? 'Otakase TV'}'),
            host: '',
            port: 0,
            version: remoteVersion,
          ),
    ];
  }

  Future<WebSocket> open(TVDevice tv, String phone) => connect(
    relayUrl(base, '/v1/remote/control', {'id': tv.id, 'name': phone}),
    token(),
  );
}
