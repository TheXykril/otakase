import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/foundation.dart';

import 'account.dart';
import 'models.dart';
import 'remote_relay.dart';
import 'tv_remote.dart';

// The live connection to the account: one WebSocket per device while the
// app is on screen. The server tells it when synced data or the vault
// changed and hands it inbox items; the phone remote, Play on and send to
// device travel over it between the account's devices on any network.

typedef LiveConnect = Future<WebSocket> Function(Uri url);

Future<WebSocket> liveSocket(Uri url) =>
    WebSocket.connect(url.toString()).timeout(const Duration(seconds: 15));

/// A device of the account that is online now.
class LiveDevice {
  const LiveDevice({
    required this.device,
    required this.name,
    this.tv = false,
    this.host = false,
    this.remote = '',
    this.os = '',
  });
  final String device, name, remote, os;
  final bool tv, host;
  factory LiveDevice.fromJson(Json j) => LiveDevice(
    device: j['device'] as String? ?? '',
    name: j['name'] as String? ?? 'Device',
    tv: j['tv'] == true,
    host: j['host'] == true,
    remote: j['remote'] as String? ?? '',
    os: j['os'] as String? ?? '',
  );
}

/// A message from another device of the account.
class LiveMessage {
  const LiveMessage(this.from, this.fromName, this.body);
  final String from, fromName;
  final Json body;
}

class AccountLive extends ChangeNotifier {
  AccountLive({
    required this.session,
    this.connect = liveSocket,
    this.remoteId = '',
    this.hostsRemote = false,
    this.pingEvery = const Duration(seconds: 30),
  });
  final AccountSession session;
  final LiveConnect connect;

  /// This device's phone remote id, so phones can find it.
  String remoteId;

  /// Whether this device runs the phone remote host (TVs and computers).
  bool hostsRemote;
  final Duration pingEvery;
  WebSocket? _socket;
  Timer? _retry, _ping;
  int _fails = 0;
  bool _wanted = false, closed = false;
  List<LiveDevice> online = const [];
  final _messages = StreamController<LiveMessage>.broadcast();
  final _offline = StreamController<String>.broadcast();

  void Function(int version)? onSync;
  void Function()? onVault;
  void Function()? onDevices;
  Future<void> Function(List<Json> items)? onInbox;

  bool get connected => _socket != null;
  Stream<LiveMessage> get messages => _messages.stream;

  /// Devices that turned out to be offline when written to.
  Stream<String> get offline => _offline.stream;

  @override
  void notifyListeners() {
    if (!closed) super.notifyListeners();
  }

  /// Keeps the connection up while the app is on screen.
  void start() {
    if (closed) return;
    _wanted = true;
    if (_socket == null) unawaited(_connect());
  }

  /// Drops the connection while the app is in the background.
  Future<void> stop() async {
    _wanted = false;
    _retry?.cancel();
    _ping?.cancel();
    final s = _socket;
    _socket = null;
    online = const [];
    await s?.close();
    notifyListeners();
  }

  /// Reconnects with the current remote id and host role.
  Future<void> restart() async {
    if (!_wanted) return;
    await stop();
    start();
  }

  Future<void> _connect() async {
    _retry?.cancel();
    if (!_wanted || closed || session.signedOut) return;
    try {
      final url = await session.liveUri({
        if (remoteId.isNotEmpty) 'remote': remoteId,
        if (hostsRemote) 'host': '1',
      });
      final ws = await connect(url);
      if (!_wanted || closed) {
        await ws.close();
        return;
      }
      _socket = ws;
      _fails = 0;
      ws.listen(
        _frame,
        onDone: () => _lost(ws),
        onError: (Object _) {},
        cancelOnError: true,
      );
      _ping?.cancel();
      // The server answers "ping" without waking up, so this costs nothing.
      _ping = Timer.periodic(pingEvery, (_) => _socket?.add('ping'));
      notifyListeners();
    } catch (_) {
      _again();
    }
  }

  void _lost(WebSocket ws) {
    if (_socket != ws) return;
    _socket = null;
    _ping?.cancel();
    online = const [];
    notifyListeners();
    // Removed from the account: no point coming back.
    if (ws.closeCode == 4001) return;
    _again();
  }

  void _again() {
    if (!_wanted || closed) return;
    _fails++;
    _retry?.cancel();
    final wait = min(300, 2 * (1 << min(_fails - 1, 7)));
    _retry = Timer(Duration(seconds: wait), () => unawaited(_connect()));
  }

  void _frame(dynamic raw) {
    if (raw is! String || raw == 'pong') return;
    final Json j;
    try {
      j = (jsonDecode(raw) as Map).cast<String, dynamic>();
    } catch (_) {
      return;
    }
    switch (j['t']) {
      case 'hello':
      case 'presence':
        online = [
          for (final d in j['online'] as List? ?? const [])
            if (d is Map) LiveDevice.fromJson(d.cast<String, dynamic>()),
        ];
        notifyListeners();
      case 'sync':
        onSync?.call(number(j['v']));
      case 'vault':
        onVault?.call();
      case 'devices':
        onDevices?.call();
      case 'inbox':
        final items = [
          for (final i in j['items'] as List? ?? const [])
            if (i is Map) i.cast<String, dynamic>(),
        ];
        if (items.isNotEmpty) unawaited(_inbox(items));
      case 'msg':
        final body = j['m'];
        if (body is Map) {
          _messages.add(
            LiveMessage(
              j['from'] as String? ?? '',
              j['from_name'] as String? ?? '',
              body.cast<String, dynamic>(),
            ),
          );
        }
      case 'offline':
        _offline.add(j['to'] as String? ?? '');
    }
  }

  Future<void> _inbox(List<Json> items) async {
    try {
      await onInbox?.call(items);
      _send({
        't': 'ack',
        'ids': [for (final i in items) i['id']],
      });
    } catch (_) {
      /* Kept on the server; delivered again on the next connect. */
    }
  }

  bool _send(Json message) {
    final s = _socket;
    if (s == null) return false;
    s.add(jsonEncode(message));
    return true;
  }

  /// Sends [body] to another device of the account.
  bool sendTo(String device, Json body) =>
      _send({'t': 'to', 'to': device, 'm': body});

  /// Passes a playback heartbeat to the Discord bot.
  bool heartbeat(Json body) => _send({'t': 'hb', 'body': body});

  /// The device running the phone remote with [remote] as its id.
  LiveDevice? hostFor(String remote) =>
      online.where((d) => d.host && d.remote == remote).firstOrNull;

  @override
  void dispose() {
    closed = true;
    _wanted = false;
    _retry?.cancel();
    _ping?.cancel();
    unawaited(_socket?.close());
    _socket = null;
    unawaited(_messages.close());
    unawaited(_offline.close());
    super.dispose();
  }
}

/// The phone side of the remote through the account.
class HubRoute implements AwayRoute {
  HubRoute(this.live);
  final AccountLive live;

  @override
  bool get available => live.connected;

  @override
  Future<List<TVDevice>> tvs() async => [
    for (final d in live.online)
      if (d.host && d.remote.isNotEmpty && d.remote != live.remoteId)
        TVDevice(
          id: d.remote,
          name: remoteName(d.name),
          host: '',
          port: 0,
          version: remoteVersion,
        ),
  ];

  @override
  Future<RemoteChannel> open(TVDevice tv, String phone) async {
    final host = live.hostFor(tv.id);
    if (host == null || !live.connected) {
      throw StateError('${tv.name} is not online.');
    }
    return HubChannel(live, host.device, phone);
  }
}

/// Remote messages to one TV through the account, shaped like the
/// WebSocket the remote uses at home.
class HubChannel implements RemoteChannel {
  HubChannel(this.live, this.device, String phone) {
    _subs.add(
      live.messages
          .where((m) => m.from == device)
          .listen((m) => _data.add(jsonEncode(m.body))),
    );
    _subs.add(
      live.offline
          .where((d) => d == device)
          .listen((_) => _end(4404, 'The TV went offline.')),
    );
    live.addListener(_watch);
    live.sendTo(device, {'type': 'join', 'name': phone});
  }
  final AccountLive live;
  final String device;
  final _data = StreamController<dynamic>();
  final _subs = <StreamSubscription<dynamic>>[];
  bool _ended = false;
  @override
  int? closeCode;
  @override
  String? closeReason;

  void _watch() {
    if (!live.connected) _end(1001, null);
  }

  void _end(int code, String? reason) {
    if (_ended) return;
    _ended = true;
    closeCode = code;
    closeReason = reason;
    live.removeListener(_watch);
    for (final s in _subs) {
      unawaited(s.cancel());
    }
    unawaited(_data.close());
  }

  @override
  StreamSubscription<dynamic> listen(
    void Function(dynamic data) onData, {
    void Function()? onDone,
    Function? onError,
    bool? cancelOnError,
  }) => _data.stream.listen(
    onData,
    onDone: onDone,
    onError: onError,
    cancelOnError: cancelOnError,
  );

  @override
  void add(String data) {
    if (_ended) return;
    try {
      live.sendTo(device, (jsonDecode(data) as Map).cast<String, dynamic>());
    } catch (_) {
      /* Only JSON objects travel. */
    }
  }

  @override
  Future<void> close() async {
    if (_ended) return;
    live.sendTo(device, {'type': 'leave'});
    _end(1000, null);
  }
}

/// The TV or computer side: answers the account's phones with what plays
/// and runs their controls.
class HubRemoteHost {
  HubRemoteHost(
    this.live,
    this.remote, {
    this.statusEvery = const Duration(seconds: 1),
  }) {
    _subs.add(live.messages.listen((m) => unawaited(_frame(m))));
    _subs.add(live.offline.listen(_phones.remove));
  }
  final AccountLive live;
  final TVRemote remote;
  final Duration statusEvery;
  final _phones = <String>{};
  final _subs = <StreamSubscription<dynamic>>[];
  Timer? _push;
  String? _last;

  bool get hasPhones => _phones.isNotEmpty;

  Future<void> _frame(LiveMessage m) async {
    final body = m.body;
    switch (body['type']) {
      case 'join':
        _phones.add(m.from);
        _status(always: true);
        _push ??= Timer.periodic(statusEvery, (_) => _status());
        return;
      case 'leave':
        _phones.remove(m.from);
        if (_phones.isEmpty) {
          _push?.cancel();
          _push = null;
        }
        return;
      case 'grant':
        // Same account: the phone gets a home network pairing without Allow.
        final grant = await remote.grant('${body['name'] ?? m.fromName}');
        live.sendTo(m.from, {'type': 'grant', ...grant});
        return;
    }
    if (body['action'] is! String) return;
    _phones.add(m.from);
    final (code, reply) = remote.control(
      body['action'] as String,
      (body['args'] as Map? ?? {}).cast<String, dynamic>(),
    );
    live.sendTo(m.from, {
      'type': 'ack',
      'seq': body['seq'],
      if (code >= 300) 'error': reply['error'],
    });
  }

  void _status({bool always = false}) {
    if (_phones.isEmpty) return;
    final now = remote.liveStatus();
    final text = jsonEncode(now);
    if (!always && text == _last) return;
    _last = text;
    for (final p in _phones.toList()) {
      live.sendTo(p, now);
    }
  }

  Future<void> close() async {
    _push?.cancel();
    for (final s in _subs) {
      await s.cancel();
    }
  }
}
