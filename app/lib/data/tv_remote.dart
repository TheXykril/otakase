import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'auth.dart';
import 'lan_page.dart';
import 'models.dart';

const tvDiscoveryPort = 8654;

/// What a TV speaks: 2 adds asking on the TV instead of a code, and the
/// live connection.
const remoteVersion = 2;

class TVDevice {
  const TVDevice({
    required this.id,
    required this.name,
    required this.host,
    required this.port,
    this.version = 1,
  });
  final String id, name, host;
  final int port, version;
  Json toJson() => {
    'id': id,
    'name': name,
    'host': host,
    'port': port,
    'v': version,
  };
  factory TVDevice.fromJson(Json j) => TVDevice(
    id: j['id'] as String,
    name: j['name'] as String? ?? 'Otakase TV',
    host: j['host'] as String,
    port: number(j['port']),
    version: number(j['v']) < 1 ? 1 : number(j['v']),
  );
}

/// A phone asking to control this TV, shown on the TV until someone picks
/// Allow or Not now. [digits] show on both screens so people can tell
/// their phone apart from another one asking.
class RemoteAsk {
  RemoteAsk(this.id, this.phone, this.digits, this.expires);
  final String id, phone, digits;
  final DateTime expires;
  String state = 'waiting';
  Json? grant;
}

class RemotePairCode {
  const RemotePairCode(this.device, this.code, this.expires);
  final TVDevice device;
  final String code;
  final DateTime expires;
  Uri get qr => Uri(
    scheme: 'otakase',
    host: 'remote',
    queryParameters: {
      'id': device.id,
      'host': device.host,
      'port': '${device.port}',
      'code': code,
    },
  );
  static RemotePairCode fromUri(Uri uri) {
    final q = uri.queryParameters,
        address = InternetAddress.tryParse(uri.queryParameters['host'] ?? '');
    final port = int.tryParse(q['port'] ?? '') ?? 0;
    if (uri.scheme != 'otakase' ||
        uri.host != 'remote' ||
        !RegExp(r'^\d{6}$').hasMatch(q['code'] ?? '') ||
        address == null ||
        !privateAddress(address) ||
        port < 1 ||
        port > 65535 ||
        (q['id'] ?? '').isEmpty) {
      throw const FormatException('Invalid TV pairing code.');
    }
    return RemotePairCode(
      TVDevice(id: q['id']!, name: 'Otakase TV', host: q['host']!, port: port),
      q['code']!,
      DateTime.now().add(const Duration(minutes: 10)),
    );
  }
}

/// What the phone shows when the TV or computer at [host] never answered:
/// almost always a firewall on a computer, or the two on different networks.
String unreachableTVMessage(String host, int port) =>
    'Could not reach $host. If it is a computer, open More > Phone remote '
    'there and tap Allow phone remote, or allow port $port (TCP and UDP) in '
    'its firewall. Check that both are on the same Wi-Fi.';

class LanTransport {
  const LanTransport({this.allowLoopback = false});
  final bool allowLoopback;
  bool allowed(String host) {
    final a = InternetAddress.tryParse(host);
    return a != null && (privateAddress(a) || allowLoopback && a.isLoopback);
  }

  Future<Json> post(
    TVDevice tv,
    String path,
    Json body, {
    String token = '',
    Duration connectTimeout = const Duration(seconds: 3),
  }) async {
    if (!allowed(tv.host) || tv.port < 1 || tv.port > 65535) {
      throw StateError('Choose a TV on your local network.');
    }
    // LAN requests use a direct socket, with neither proxies nor redirects.
    final Socket socket;
    try {
      socket = await Socket.connect(tv.host, tv.port, timeout: connectTimeout);
    } on SocketException {
      throw StateError(unreachableTVMessage(tv.host, tv.port));
    }
    try {
      final payload = utf8.encode(jsonEncode(body));
      socket.add(
        utf8.encode(
          'POST $path HTTP/1.1\r\nHost: ${tv.host}:${tv.port}\r\nContent-Type: application/json\r\nContent-Length: ${payload.length}\r\nConnection: close\r\n${token.isEmpty ? '' : 'Authorization: Bearer $token\r\n'}\r\n',
        ),
      );
      socket.add(payload);
      await socket.flush();
      final bytes = <int>[];
      await for (final chunk in socket.timeout(const Duration(seconds: 5))) {
        if (bytes.length + chunk.length > 65536) {
          throw const FormatException('TV response too large');
        }
        bytes.addAll(chunk);
      }
      final raw = utf8.decode(bytes), split = raw.indexOf('\r\n\r\n');
      if (split < 0) throw const FormatException('Unreadable TV response');
      final status =
          int.tryParse(
            raw.split('\r\n').first.split(' ').elementAtOrNull(1) ?? '',
          ) ??
          0;
      final result = (jsonDecode(raw.substring(split + 4)) as Map)
          .cast<String, dynamic>();
      if (status < 200 || status >= 300) {
        throw StateError(
          result['error'] as String? ?? 'TV request failed ($status)',
        );
      }
      return result;
    } finally {
      socket.destroy();
    }
  }

  /// Opens the TV's live connection, also straight to it without proxies.
  Future<WebSocket> live(TVDevice tv, Map<String, String> headers) {
    final client = HttpClient()
      ..findProxy = ((_) => 'DIRECT')
      ..connectionTimeout = const Duration(seconds: 3);
    return WebSocket.connect(
      'ws://${tv.host}:${tv.port}/live',
      headers: headers,
      customClient: client,
    ).timeout(const Duration(seconds: 5));
  }
}

TVDevice? parseTVAdvertisement(
  String raw,
  String host, {
  bool allowLoopback = false,
}) {
  final a = InternetAddress.tryParse(host);
  if (a == null ||
      !(privateAddress(a) || allowLoopback && a.isLoopback) ||
      raw.length > 1024) {
    return null;
  }
  try {
    final j = jsonDecode(raw) as Map;
    if (j['otakase'] != 1 ||
        j['kind'] != 'tv' ||
        j['id'] is! String ||
        (j['id'] as String).isEmpty ||
        number(j['port']) < 1 ||
        number(j['port']) > 65535) {
      return null;
    }
    return TVDevice(
      id: j['id'] as String,
      name: j['name'] as String? ?? 'Otakase TV',
      host: host,
      port: number(j['port']),
      version: number(j['v']) < 1 ? 1 : number(j['v']),
    );
  } catch (_) {
    return null;
  }
}

/// x.y.z.255 for each local address, so the ask reaches the Wi-Fi even when
/// 255.255.255.255 leaves through another link or the router drops it.
List<String> directedBroadcasts(List<String> addresses) => {
  for (final a in addresses)
    if (InternetAddress.tryParse(a) case final ip? when privateAddress(ip))
      '${ip.rawAddress[0]}.${ip.rawAddress[1]}.${ip.rawAddress[2]}.255',
}.toList();

/// Every other host on the same /24 as [addresses], for the direct check
/// when broadcasts never arrive.
List<String> neighbourHosts(List<String> addresses) {
  final own = addresses.toSet(), hosts = <String>{};
  for (final a in addresses) {
    final ip = InternetAddress.tryParse(a);
    if (ip == null || !privateAddress(ip)) continue;
    final b = ip.rawAddress;
    for (var last = 1; last < 255; last++) {
      final host = '${b[0]}.${b[1]}.${b[2]}.$last';
      if (!own.contains(host)) hosts.add(host);
    }
  }
  return hosts.toList();
}

/// Asks one address for the TV on [port], the fixed remote port.
Future<TVDevice?> helloTV(
  String host, {
  int port = tvDiscoveryPort,
  LanTransport transport = const LanTransport(),
  Duration connectTimeout = const Duration(seconds: 3),
}) async {
  try {
    final j = await transport.post(
      TVDevice(id: '', name: '', host: host, port: port),
      '/hello',
      {'otakase': 1},
      connectTimeout: connectTimeout,
    );
    return parseTVAdvertisement(
      jsonEncode(j),
      host,
      allowLoopback: transport.allowLoopback,
    );
  } catch (_) {
    return null;
  }
}

Future<List<TVDevice>> discoverTVs({
  Duration wait = const Duration(seconds: 2),
  int port = tvDiscoveryPort,
  bool sweep = true,
  Future<List<String>> Function() addresses = lanAddresses,
}) async {
  final socket = await RawDatagramSocket.bind(InternetAddress.anyIPv4, 0);
  socket.broadcastEnabled = true;
  final found = <String, TVDevice>{};
  final subscription = socket.listen((event) {
    if (event != RawSocketEvent.read) return;
    Datagram? d;
    while ((d = socket.receive()) != null) {
      final packet = d!;
      try {
        final tv = parseTVAdvertisement(
          utf8.decode(packet.data),
          packet.address.address,
        );
        if (tv != null) found[tv.id] = tv;
      } catch (_) {
        /* Ignore unrelated packets. */
      }
    }
  });
  try {
    final own = await addresses().catchError((Object _) => <String>[]);
    final ask = utf8.encode('{"otakase":1,"kind":"discover"}');
    for (final target in ['255.255.255.255', ...directedBroadcasts(own)]) {
      try {
        socket.send(ask, InternetAddress(target), port);
      } catch (_) {
        /* Some links refuse broadcasts; the others still go out. */
      }
    }
    final checks = <Future<void>>[Future<void>.delayed(wait)];
    if (sweep) {
      // Routers and firewalls often drop broadcasts, so also knock on the
      // fixed remote port of every neighbour, a few at a time.
      final hosts = neighbourHosts(own);
      Future<void> worker() async {
        while (hosts.isNotEmpty) {
          final host = hosts.removeLast();
          final tv = await helloTV(
            host,
            port: port,
            connectTimeout: const Duration(milliseconds: 700),
          );
          if (tv != null) found.putIfAbsent(tv.id, () => tv);
        }
      }

      checks.addAll([for (var i = 0; i < 48; i++) worker()]);
    }
    await Future.wait(checks);
  } finally {
    await subscription.cancel();
    socket.close();
  }
  return found.values.toList();
}

/// Reads "192.168.1.20" or "192.168.1.20:8654" typed from the TV screen.
(String, int)? parseTVAddress(String raw) {
  final value = raw.trim();
  final match = RegExp(r'^(\d{1,3}(?:\.\d{1,3}){3})(?::(\d{1,5}))?$')
      .firstMatch(value);
  if (match == null) return null;
  final ip = InternetAddress.tryParse(match[1]!);
  final port = int.tryParse(match[2] ?? '') ?? tvDiscoveryPort;
  if (ip == null || !privateAddress(ip) || port < 1 || port > 65535) {
    return null;
  }
  return (ip.address, port);
}

String remoteName(String value) =>
    String.fromCharCodes(value.trim().runes.take(40));

class TVRemote extends ChangeNotifier {
  TVRemote({
    required this.storage,
    this.name = 'Otakase TV',
    this.findAddress = lanAddress,
    this.transport = const LanTransport(),
    DateTime Function()? now,
  }) : now = now ?? DateTime.now;
  final TokenStore storage;
  final String name;
  final Future<String?> Function() findAddress;
  final LanTransport transport;
  final DateTime Function() now;
  HttpServer? _server;
  RawDatagramSocket? _discovery;
  bool closed = false, serverStarted = false;
  String id = '', address = '';
  String? error;
  RemotePairCode? code;
  Json _paired = {}, _phones = {};
  Future<void>? _writes, _initializing, _commands;
  int _wrong = 0, _pendingCommands = 0, _denied = 0;
  DateTime? _waitUntil;
  final _asks = <String, RemoteAsk>{};
  final _live = <WebSocket, String>{};
  RemoteAsk? _ask;

  /// The phone waiting for Allow on this TV, if any.
  RemoteAsk? get ask {
    final a = _ask;
    return a != null && a.state == 'waiting' && now().isBefore(a.expires)
        ? a
        : null;
  }

  final _connected = <String, DateTime>{};
  Future<void> Function(String, Json)? command;
  Json Function()? status;
  final _events = StreamController<String>.broadcast();
  Stream<String> get events => _events.stream;
  bool get hasPhones => (_phones['clients'] as Map? ?? {}).isNotEmpty;
  int get port => _server?.port ?? 0;
  List<TVDevice> get paired => _paired.values
      .whereType<Map>()
      .map((m) => TVDevice.fromJson(m.cast<String, dynamic>()))
      .toList();
  @override
  void notifyListeners() {
    if (!closed) super.notifyListeners();
  }

  Future<void> initialize() => _initializing ??= _initialize();
  Future<void> _initialize() async {
    try {
      _paired = (jsonDecode(await storage.read('lan.remote') ?? '{}') as Map)
          .cast<String, dynamic>();
    } catch (_) {
      _paired = {};
    }
    try {
      _phones = (jsonDecode(await storage.read('lan.tv') ?? '{}') as Map)
          .cast<String, dynamic>();
    } catch (_) {
      _phones = {};
    }
    id = _phones['id'] as String? ?? randomSecret();
  }

  Future<void> _persist(String key, Json value) {
    final encoded = jsonEncode(value);
    return _writes = (_writes ?? Future<void>.value())
        .catchError((Object _) {})
        .then((_) async {
          if (!closed) await storage.write(key, encoded);
        });
  }

  Future<void> startTV({
    int discoveryPort = tvDiscoveryPort,
    int serverPort = tvDiscoveryPort,
  }) async {
    if (closed || serverStarted) return;
    await initialize();
    if (closed || serverStarted) return;
    serverStarted = true;
    try {
      final ip = await findAddress();
      if (ip == null || !transport.allowed(ip)) {
        throw StateError(
          'Connect the TV to your local network to use a phone remote.',
        );
      }
      address = ip;
      try {
        // A fixed port lets phones find the TV without broadcasts and lets
        // people open one port in a desktop firewall.
        _server = await HttpServer.bind(InternetAddress.anyIPv4, serverPort);
      } on SocketException {
        _server = await HttpServer.bind(InternetAddress.anyIPv4, 0);
      }
      if (closed) {
        await _server?.close(force: true);
        return;
      }
      _server!.listen(_receive);
      _phones = {..._phones, 'id': id};
      await _persist('lan.tv', _phones);
      try {
        _discovery = await RawDatagramSocket.bind(
          InternetAddress.anyIPv4,
          discoveryPort,
          reuseAddress: true,
        );
        _discovery!.listen((event) {
          if (event != RawSocketEvent.read || closed) return;
          Datagram? d;
          while ((d = _discovery?.receive()) != null) {
            final packet = d!;
            if (!privateAddress(packet.address) || packet.data.length > 128) {
              continue;
            }
            try {
              final j = jsonDecode(utf8.decode(packet.data));
              if (j is Map && j['otakase'] == 1 && j['kind'] == 'discover') {
                _discovery?.send(
                  utf8.encode(
                    jsonEncode({
                      'otakase': 1,
                      'kind': 'tv',
                      'id': id,
                      'name': name,
                      'port': _server!.port,
                      'v': remoteVersion,
                    }),
                  ),
                  packet.address,
                  packet.port,
                );
              }
            } catch (_) {
              /* Ignore unrelated packets. */
            }
          }
        });
        try {
          await const MethodChannel('otakase/device')
              .invokeMethod<void>('localDiscovery', true);
        } catch (_) {
          /* Ethernet needs no Wi-Fi lock. */
        }
      } catch (_) {
        error = 'Use the TV QR code if the phone does not find this TV.';
      }
    } catch (e) {
      serverStarted = false;
      error = e.toString();
    }
    notifyListeners();
  }

  Future<RemotePairCode> beginPair() async {
    if (_server == null) await startTV();
    if (_server == null) throw StateError(error ?? 'TV is unavailable.');
    code = RemotePairCode(
      TVDevice(id: id, name: name, host: address, port: _server!.port),
      Random.secure().nextInt(1000000).toString().padLeft(6, '0'),
      now().add(const Duration(minutes: 10)),
    );
    _wrong = 0;
    _waitUntil = null;
    notifyListeners();
    return code!;
  }

  void endPair() {
    code = null;
    notifyListeners();
  }

  Future<void> _reply(HttpRequest r, int code, Json value) async {
    final bytes = utf8.encode(jsonEncode(value));
    r.response.persistentConnection = false;
    r.response.statusCode = code;
    r.response.headers.contentType = ContentType.json;
    r.response.headers.set('Cache-Control', 'no-store');
    r.response.contentLength = bytes.length;
    r.response.add(bytes);
    await r.response.close();
  }

  Future<void> _receive(HttpRequest r) async {
    final peer = r.connectionInfo?.remoteAddress;
    if (closed ||
        peer == null ||
        !transport.allowed(peer.address) ||
        r.headers.value('origin') != null) {
      await _reply(r, 403, {'error': 'Local app connections only.'});
      return;
    }
    if (r.uri.path == '/live' && r.method == 'GET') {
      await _openLive(r);
      return;
    }
    if (r.method != 'POST') {
      await _reply(r, 403, {'error': 'Local app connections only.'});
      return;
    }
    try {
      if (r.contentLength < 0 ||
          r.contentLength > 4096 ||
          r.headers.contentType?.mimeType != 'application/json') {
        await _reply(r, 400, {'error': 'Invalid request.'});
        return;
      }
      final body = (jsonDecode(
        await utf8.decoder.bind(r).join().timeout(const Duration(seconds: 3)),
      ) as Map).cast<String, dynamic>();
      if (r.uri.path == '/hello') {
        await _reply(r, 200, {
          'otakase': 1,
          'kind': 'tv',
          'id': id,
          'name': name,
          'port': _server!.port,
          'v': remoteVersion,
        });
        return;
      }
      if (r.uri.path == '/pair') {
        if (_waitUntil != null && now().isBefore(_waitUntil!)) {
          await _reply(r, 429, {
            'error': 'Wait 30 seconds before pairing again.',
          });
          return;
        }
        if (code == null || !now().isBefore(code!.expires)) {
          await _reply(r, 410, {'error': 'Open a new pairing code on the TV.'});
          return;
        }
        if (body['code'] != code!.code || body['id'] != id) {
          _wrong++;
          if (_wrong >= 5) {
            _wrong = 0;
            _waitUntil = now().add(const Duration(seconds: 30));
          }
          await _reply(r, 403, {'error': 'The code does not match.'});
          return;
        }
        final grant = await _addClient(body['name']?.toString() ?? 'Phone');
        code = null;
        await _reply(r, 200, grant);
        notifyListeners();
        return;
      }
      if (r.uri.path == '/ask') {
        final (status, reply) = _newAsk(body);
        await _reply(r, status, reply);
        return;
      }
      if (r.uri.path == '/ask/wait') {
        final (status, reply) = _waitAsk(body['request']?.toString() ?? '');
        await _reply(r, status, reply);
        return;
      }
      final key = body['client'] as String? ?? '';
      final saved = _client(key, r.headers.value('authorization'));
      if (saved == null) {
        await _reply(r, 401, {'error': 'Pair this phone with the TV again.'});
        return;
      }
      if (r.uri.path != '/status' && r.uri.path != '/command') {
        await _reply(r, 404, {'error': 'Unknown control.'});
        return;
      }
      _seen(key, saved);
      if (r.uri.path == '/command') {
        final (code, reply) = _command(
          body['action'] as String? ?? '',
          (body['args'] as Map? ?? {}).cast<String, dynamic>(),
        );
        await _reply(r, code, reply);
        return;
      }
      await _reply(r, 200, {'id': id, 'name': name, ...?status?.call()});
    } catch (_) {
      try {
        await _reply(r, 400, {'error': 'TV control failed. Try again.'});
      } catch (_) {
        /* The phone disconnected. */
      }
    }
  }

  /// The saved phone for [key] when [auth] carries its token.
  Map? _client(String key, String? auth) {
    final saved = (_phones['clients'] as Map? ?? {})[key] as Map?;
    final bearer = auth ?? '';
    if (saved == null ||
        !bearer.startsWith('Bearer ') ||
        sha256.convert(utf8.encode(bearer.substring(7))).toString() !=
            saved['hash']) {
      return null;
    }
    return saved;
  }

  void _seen(String key, Map saved) {
    final seen = _connected[key];
    if (seen == null || now().difference(seen) > const Duration(seconds: 30)) {
      _events.add('${saved['name']} connected');
    }
    _connected[key] = now();
  }

  Future<Json> _addClient(String phone) async {
    final token = randomSecret(), key = randomSecret();
    final clients = Map<String, dynamic>.from(_phones['clients'] as Map? ?? {});
    clients[key] = {
      'hash': sha256.convert(utf8.encode(token)).toString(),
      'name': remoteName(phone),
    };
    if (clients.length > 12) clients.remove(clients.keys.first);
    _phones = {..._phones, 'clients': clients};
    await _persist('lan.tv', _phones);
    return {'id': id, 'client': key, 'token': token, 'name': name};
  }

  (int, Json) _newAsk(Json body) {
    final at = now();
    _asks.removeWhere((_, a) => !at.isBefore(a.expires));
    if (_waitUntil != null && at.isBefore(_waitUntil!)) {
      return (429, {'error': 'Wait 30 seconds before asking the TV again.'});
    }
    if (body['id'] != id) {
      return (403, {'error': 'This is a different TV. Find TVs again.'});
    }
    if (ask != null) {
      return (
        429,
        {'error': 'The TV is answering another phone. Try again in a minute.'},
      );
    }
    final request = RemoteAsk(
      randomSecret(),
      remoteName(body['name']?.toString() ?? 'Phone'),
      '${Random.secure().nextInt(90) + 10}',
      at.add(const Duration(minutes: 1)),
    );
    _asks[request.id] = _ask = request;
    notifyListeners();
    return (202, {'request': request.id, 'digits': request.digits});
  }

  /// Puts a phone's question on screen without the network, for tests.
  @visibleForTesting
  RemoteAsk? debugAsk(String phone) {
    _newAsk({'id': id, 'name': phone});
    return ask;
  }

  (int, Json) _waitAsk(String request) {
    final a = _asks[request];
    if (a == null || a.state == 'waiting' && !now().isBefore(a.expires)) {
      _asks.remove(request);
      return (410, {'error': 'Nobody answered on the TV. Ask again.'});
    }
    if (a.state == 'denied') {
      _asks.remove(request);
      return (403, {'error': 'The TV said not now.'});
    }
    if (a.state == 'allowed') {
      _asks.remove(request);
      return (200, {'state': 'allowed', ...?a.grant});
    }
    return (200, {'state': 'waiting'});
  }

  /// Allow on the TV: the asking phone becomes a remote.
  Future<void> allowAsk() async {
    final a = ask;
    if (a == null) return;
    a.grant = await _addClient(a.phone);
    a.state = 'allowed';
    _ask = null;
    _denied = 0;
    _events.add('${a.phone} can control this TV');
    notifyListeners();
  }

  /// Not now on the TV. Three in a row make phones wait 30 seconds, so
  /// nobody on the network can keep the question on screen.
  void denyAsk() {
    final a = _ask;
    if (a == null) return;
    a.state = 'denied';
    _ask = null;
    if (++_denied >= 3) {
      _denied = 0;
      _waitUntil = now().add(const Duration(seconds: 30));
    }
    notifyListeners();
  }

  static const remoteActions = [
    'play',
    'pause',
    'toggle',
    'seek',
    'seek_to',
    'party_skip',
    'next',
    'previous',
    'episode',
    'volume',
    'subtitle',
    'audio',
    'quality',
    'stop',
  ];

  /// Checks one control from a phone and queues it for the player.
  (int, Json) _command(String action, Json args) {
    if (!remoteActions.contains(action)) {
      return (400, {'error': 'Unknown control.'});
    }
    if (command == null || status?.call()['has_player'] != true) {
      return (409, {'error': 'Open an episode on the TV first.'});
    }
    if (action == 'seek' && number(args['seconds']).abs() != 10) {
      return (400, {'error': 'Seek by 10 seconds.'});
    }
    if (action == 'seek_to' &&
        (args['position'] is! num || (args['position'] as num) < 0)) {
      return (400, {'error': 'Choose a place in the episode.'});
    }
    if ((action == 'subtitle' || action == 'audio') &&
        (args['id'] is! String || (args['id'] as String).length > 64)) {
      return (400, {'error': 'Choose a track.'});
    }
    if (action == 'quality' &&
        !RegExp(r'^(best|worst|\d{3,4})$').hasMatch('${args['value']}')) {
      return (400, {'error': 'Choose a quality.'});
    }
    if (_pendingCommands >= 10) {
      return (429, {'error': 'Wait for the TV to finish this action.'});
    }
    _pendingCommands++;
    _commands = (_commands ?? Future<void>.value())
        .catchError((Object _) {})
        .then((_) async {
          if (closed) return;
          try {
            await command?.call(action, args);
          } catch (_) {
            if (!closed) _events.add('TV control failed. Try again.');
          }
        })
        .whenComplete(() => _pendingCommands--);
    return (202, {'id': id, 'name': name, ...?status?.call()});
  }

  /// The live connection: the TV pushes what is playing whenever it
  /// changes and takes controls back, so phones never poll.
  Future<void> _openLive(HttpRequest r) async {
    final key = r.headers.value('x-otakase-client') ?? '';
    final saved = _client(key, r.headers.value('authorization'));
    if (saved == null) {
      await _reply(r, 401, {'error': 'Pair this phone with the TV again.'});
      return;
    }
    if (!WebSocketTransformer.isUpgradeRequest(r) || _live.length >= 8) {
      await _reply(r, 400, {'error': 'Invalid request.'});
      return;
    }
    final WebSocket socket;
    try {
      socket = await WebSocketTransformer.upgrade(r);
    } catch (_) {
      return;
    }
    if (closed) {
      await socket.close();
      return;
    }
    socket.pingInterval = const Duration(seconds: 5);
    _live[socket] = key;
    _seen(key, saved);
    String? last;
    void push() {
      if ((_phones['clients'] as Map? ?? {})[key] == null) {
        unawaited(socket.close(4401, 'Pair this phone with the TV again.'));
        return;
      }
      final fresh = jsonEncode({
        'type': 'status',
        'id': id,
        'name': name,
        ...?status?.call(),
      });
      if (fresh != last) socket.add(last = fresh);
    }

    push();
    final timer = Timer.periodic(
      const Duration(milliseconds: 400),
      (_) => push(),
    );
    socket.listen(
      (data) {
        if (data is! String || data.length > 4096) return;
        try {
          final j = (jsonDecode(data) as Map).cast<String, dynamic>();
          final (code, reply) = _command(
            j['action'] as String? ?? '',
            (j['args'] as Map? ?? {}).cast<String, dynamic>(),
          );
          _connected[key] = now();
          socket.add(
            jsonEncode({
              'type': 'ack',
              'seq': j['seq'],
              if (code >= 300) 'error': reply['error'],
            }),
          );
        } catch (_) {
          /* Ignore what is not a control. */
        }
      },
      onDone: () {
        timer.cancel();
        _live.remove(socket);
      },
      cancelOnError: true,
    );
  }

  /// Opens the live connection to a paired [tv].
  Future<WebSocket> openLive(TVDevice tv) async {
    final saved = _paired[tv.id] as Map?;
    if (saved == null) throw StateError('Pair this TV first.');
    if (!transport.allowed(tv.host) || tv.port < 1 || tv.port > 65535) {
      throw StateError('Choose a TV on your local network.');
    }
    try {
      final socket = await transport.live(tv, {
        'authorization': 'Bearer ${saved['token']}',
        'x-otakase-client': '${saved['client']}',
      });
      socket.pingInterval = const Duration(seconds: 5);
      return socket;
    } on SocketException {
      throw StateError(unreachableTVMessage(tv.host, tv.port));
    } on TimeoutException {
      throw StateError(unreachableTVMessage(tv.host, tv.port));
    }
  }

  /// Asks [tv] to let this phone control it and waits for Allow there.
  /// [digits] receives the two numbers both screens show.
  Future<void> requestControl(
    TVDevice tv, {
    String phoneName = 'Phone',
    void Function(String digits)? digits,
    bool Function()? cancelled,
    Duration every = const Duration(seconds: 1),
  }) async {
    await initialize();
    final asked = await transport.post(tv, '/ask', {
      'id': tv.id,
      'name': phoneName,
    });
    final request = asked['request'];
    if (request is! String) {
      throw const FormatException('Invalid TV answer.');
    }
    digits?.call('${asked['digits'] ?? ''}');
    while (!closed && !(cancelled?.call() ?? false)) {
      await Future<void>.delayed(every);
      if (closed || (cancelled?.call() ?? false)) return;
      final j = await transport.post(tv, '/ask/wait', {'request': request});
      if (j['state'] != 'allowed') continue;
      if (j['id'] != tv.id || j['token'] is! String || j['client'] is! String) {
        throw const FormatException('Invalid TV pairing response.');
      }
      _paired[tv.id] = {
        ...tv.toJson(),
        'name': j['name'] ?? tv.name,
        'token': j['token'],
        'client': j['client'],
      };
      await _persist('lan.remote', _paired);
      notifyListeners();
      return;
    }
  }

  Future<void> pair(
    TVDevice tv,
    String digits, {
    String phoneName = 'Phone',
  }) async {
    await initialize();
    if (closed) return;
    final result = await transport.post(tv, '/pair', {
      'id': tv.id,
      'code': digits,
      'name': phoneName,
    });
    if (closed) return;
    if (result['id'] != tv.id ||
        result['token'] is! String ||
        result['client'] is! String) {
      throw const FormatException('Invalid TV pairing response.');
    }
    _paired[tv.id] = {
      ...tv.toJson(),
      'name': result['name'] ?? tv.name,
      'token': result['token'],
      'client': result['client'],
    };
    await _persist('lan.remote', _paired);
    notifyListeners();
  }

  /// Pairs with the TV showing [digits]. [address] is what the TV shows
  /// under the code; when it is empty every TV found nearby is tried.
  Future<TVDevice> pairByCode(
    String digits, {
    String address = '',
    String phoneName = 'Phone',
    Future<List<TVDevice>> Function() findTVs = discoverTVs,
  }) async {
    final List<TVDevice> candidates;
    if (address.trim().isNotEmpty) {
      final parsed = parseTVAddress(address);
      if (parsed == null) {
        throw StateError(
          'Type the address shown on the TV, like 192.168.1.20.',
        );
      }
      final tv = await helloTV(
        parsed.$1,
        port: parsed.$2,
        transport: transport,
      );
      if (tv == null) {
        throw StateError(
          'No Otakase TV answered at ${address.trim()}. Check that both '
          'are on the same Wi-Fi and the TV shows its pairing code.',
        );
      }
      candidates = [tv];
    } else {
      candidates = await findTVs();
      if (candidates.isEmpty) {
        throw StateError(
          'No TV found. Type the address shown under the code on the TV.',
        );
      }
    }
    Object? last;
    for (final tv in candidates) {
      try {
        await pair(tv, digits, phoneName: phoneName);
        return tv;
      } catch (e) {
        last = e;
      }
    }
    throw last ?? StateError('The code does not match.');
  }

  void found(TVDevice tv) {
    final old = _paired[tv.id];
    if (old is Map) {
      _paired[tv.id] = {...old, ...tv.toJson()};
      unawaited(_persist('lan.remote', _paired));
      notifyListeners();
    }
  }

  Future<Json> send(TVDevice tv, {String? action, Json args = const {}}) async {
    final saved = _paired[tv.id] as Map?;
    if (saved == null) throw StateError('Pair this TV first.');
    return transport.post(tv, action == null ? '/status' : '/command', {
      'client': saved['client'],
      'action': ?action,
      if (action != null) 'args': args,
    }, token: saved['token'] as String);
  }

  Future<void> forget(String id) async {
    _paired.remove(id);
    await _persist('lan.remote', _paired);
    notifyListeners();
  }

  Future<void> forgetPhones() async {
    _phones = {'id': id, 'clients': {}};
    _connected.clear();
    for (final socket in _live.keys.toList()) {
      unawaited(socket.close(4401, 'Pair this phone with the TV again.'));
    }
    await _persist('lan.tv', _phones);
    notifyListeners();
  }

  Future<void> close() async {
    if (closed) return;
    closed = true;
    code = null;
    _ask = null;
    _discovery?.close();
    for (final socket in _live.keys.toList()) {
      unawaited(socket.close());
    }
    _live.clear();
    await _server?.close(force: true);
    try {
      await const MethodChannel('otakase/device')
          .invokeMethod<void>('localDiscovery', false);
    } catch (_) {
      /* No native lock is required in tests. */
    }
    if (_writes != null) await _writes;
    await _events.close();
    super.dispose();
  }
}

enum LinkState { connecting, live, polling, lost }

/// The phone's side of the live connection to one paired TV. It keeps
/// [status] current, reconnects on its own, finds the TV again by its id
/// when its address changed, and falls back to asking every two seconds
/// for TVs on an older Otakase.
class RemoteLink extends ChangeNotifier {
  RemoteLink(
    this.remote,
    this.tv, {
    this.findTVs = discoverTVs,
    this.pollEvery = const Duration(seconds: 2),
  });
  final TVRemote remote;
  final Future<List<TVDevice>> Function() findTVs;
  final Duration pollEvery;
  TVDevice tv;
  Json status = {};
  LinkState state = LinkState.connecting;
  String? error;
  WebSocket? _socket;
  Timer? _retry, _poll;
  int _fails = 0, _seq = 0;
  bool _closed = false, _finding = false;
  final _acks = <int, Completer<void>>{};

  bool get live => state == LinkState.live || state == LinkState.polling;

  void start() => unawaited(_connect());

  void _set(LinkState next, [String? message]) {
    if (_closed) return;
    state = next;
    error = message;
    notifyListeners();
  }

  Future<void> _connect() async {
    if (_closed) return;
    _retry?.cancel();
    if (state != LinkState.lost) _set(LinkState.connecting);
    final WebSocket socket;
    try {
      socket = await remote.openLive(tv);
    } on WebSocketException {
      // Not upgraded: an older TV, or this phone is no longer paired.
      try {
        status = await remote.send(tv);
        _fails = 0;
        _set(LinkState.polling);
        _poll?.cancel();
        _poll = Timer.periodic(pollEvery, (_) => unawaited(_refresh()));
      } catch (e) {
        _lost(e);
      }
      return;
    } catch (e) {
      _lost(e);
      return;
    }
    if (_closed) {
      await socket.close();
      return;
    }
    _socket = socket;
    _fails = 0;
    _set(LinkState.live);
    socket.listen(
      _message,
      onDone: () {
        if (_socket != socket) return;
        _socket = null;
        _lost(
          socket.closeCode == 4401
              ? StateError(socket.closeReason ?? 'Pair this TV again.')
              : null,
        );
      },
      onError: (Object _) {},
      cancelOnError: true,
    );
  }

  Future<void> _refresh() async {
    if (_closed || state != LinkState.polling) return;
    try {
      status = await remote.send(tv);
      _set(LinkState.polling);
    } catch (e) {
      _poll?.cancel();
      _lost(e);
    }
  }

  void _message(dynamic data) {
    if (data is! String) return;
    try {
      final j = (jsonDecode(data) as Map).cast<String, dynamic>();
      if (j['type'] == 'status') {
        status = j;
        notifyListeners();
      } else if (j['type'] == 'ack') {
        final done = _acks.remove(number(j['seq']));
        final problem = j['error'];
        if (problem is String) {
          done?.completeError(StateError(problem));
        } else {
          done?.complete();
        }
      }
    } catch (_) {
      /* Ignore what is not from Otakase. */
    }
  }

  void _lost(Object? e) {
    for (final done in _acks.values) {
      done.completeError(StateError('Lost the TV. Reconnecting…'));
    }
    _acks.clear();
    if (_closed) return;
    _fails++;
    final message = e is StateError ? e.message : null;
    _set(LinkState.lost, message);
    // The TV may have a new address after a router restart.
    if (_fails >= 2) unawaited(_findAgain());
    _retry?.cancel();
    _retry = Timer(
      Duration(seconds: 1 << (_fails - 1).clamp(0, 3)),
      () => unawaited(_connect()),
    );
  }

  Future<void> _findAgain() async {
    if (_finding || _closed) return;
    _finding = true;
    try {
      for (final found in await findTVs()) {
        remote.found(found);
        if (found.id == tv.id &&
            (found.host != tv.host || found.port != tv.port)) {
          tv = found;
          _retry?.cancel();
          unawaited(_connect());
        }
      }
    } catch (_) {
      /* Try again on the next miss. */
    } finally {
      _finding = false;
    }
  }

  /// Sends one control and completes when the TV took it.
  Future<void> send(String action, {Json args = const {}}) async {
    final socket = _socket;
    if (socket == null) {
      if (state == LinkState.polling) {
        await remote.send(tv, action: action, args: args);
        unawaited(_refresh());
        return;
      }
      throw StateError('Reconnecting to ${tv.name}…');
    }
    final seq = ++_seq, done = Completer<void>();
    _acks[seq] = done;
    socket.add(jsonEncode({'seq': seq, 'action': action, 'args': args}));
    return done.future.timeout(
      const Duration(seconds: 5),
      onTimeout: () {
        _acks.remove(seq);
        throw StateError('${tv.name} did not answer. Try again.');
      },
    );
  }

  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    _retry?.cancel();
    _poll?.cancel();
    for (final done in _acks.values) {
      done.completeError(StateError('Closed.'));
    }
    _acks.clear();
    await _socket?.close();
    _socket = null;
    super.dispose();
  }
}
