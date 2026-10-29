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

class TVDevice {
  const TVDevice({
    required this.id,
    required this.name,
    required this.host,
    required this.port,
  });
  final String id, name, host;
  final int port;
  Json toJson() => {'id': id, 'name': name, 'host': host, 'port': port};
  factory TVDevice.fromJson(Json j) => TVDevice(
    id: j['id'] as String,
    name: j['name'] as String? ?? 'Otakase TV',
    host: j['host'] as String,
    port: number(j['port']),
  );
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
  }) async {
    if (!allowed(tv.host) || tv.port < 1 || tv.port > 65535) {
      throw StateError('Choose a TV on your local network.');
    }
    // LAN requests use a direct socket, with neither proxies nor redirects.
    final socket = await Socket.connect(
      tv.host,
      tv.port,
      timeout: const Duration(seconds: 3),
    );
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
}

TVDevice? parseTVAdvertisement(String raw, String host) {
  final a = InternetAddress.tryParse(host);
  if (a == null || !privateAddress(a) || raw.length > 1024) return null;
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
    );
  } catch (_) {
    return null;
  }
}

Future<List<TVDevice>> discoverTVs({
  Duration wait = const Duration(seconds: 2),
  int port = tvDiscoveryPort,
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
    socket.send(
      utf8.encode('{"otakase":1,"kind":"discover"}'),
      InternetAddress('255.255.255.255'),
      port,
    );
    await Future<void>.delayed(wait);
  } finally {
    await subscription.cancel();
    socket.close();
  }
  return found.values.toList();
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
  int _wrong = 0, _pendingCommands = 0;
  DateTime? _waitUntil;
  final _connected = <String, DateTime>{};
  Future<void> Function(String, Json)? command;
  Json Function()? status;
  final _events = StreamController<String>.broadcast();
  Stream<String> get events => _events.stream;
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

  Future<void> startTV({int discoveryPort = tvDiscoveryPort}) async {
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
      _server = await HttpServer.bind(InternetAddress.anyIPv4, 0);
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
        r.method != 'POST' ||
        r.headers.value('origin') != null) {
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
        final token = randomSecret(), key = randomSecret();
        final clients = Map<String, dynamic>.from(
          _phones['clients'] as Map? ?? {},
        );
        clients[key] = {
          'hash': sha256.convert(utf8.encode(token)).toString(),
          'name': remoteName(body['name']?.toString() ?? 'Phone'),
        };
        if (clients.length > 12) clients.remove(clients.keys.first);
        _phones = {..._phones, 'clients': clients};
        await _persist('lan.tv', _phones);
        code = null;
        await _reply(r, 200, {
          'id': id,
          'client': key,
          'token': token,
          'name': name,
        });
        notifyListeners();
        return;
      }
      final key = body['client'] as String? ?? '',
          clients = _phones['clients'] as Map? ?? {};
      final saved = clients[key] as Map?,
          auth = r.headers.value('authorization') ?? '';
      if (saved == null ||
          !auth.startsWith('Bearer ') ||
          sha256.convert(utf8.encode(auth.substring(7))).toString() !=
              saved['hash']) {
        await _reply(r, 401, {'error': 'Pair this phone with the TV again.'});
        return;
      }
      if (r.uri.path != '/status' && r.uri.path != '/command') {
        await _reply(r, 404, {'error': 'Unknown control.'});
        return;
      }
      final seen = _connected[key];
      if (seen == null ||
          now().difference(seen) > const Duration(seconds: 30)) {
        _events.add('${saved['name']} connected');
      }
      _connected[key] = now();
      if (r.uri.path == '/command') {
        final action = body['action'] as String? ?? '';
        if (!const [
          'play',
          'pause',
          'toggle',
          'seek',
          'next',
          'episode',
          'volume',
        ].contains(action)) {
          await _reply(r, 400, {'error': 'Unknown control.'});
          return;
        }
        if (command == null || status?.call()['has_player'] != true) {
          await _reply(r, 409, {'error': 'Open an episode on the TV first.'});
          return;
        }
        final args = (body['args'] as Map? ?? {}).cast<String, dynamic>();
        if (action == 'seek' && number(args['seconds']).abs() != 10) {
          await _reply(r, 400, {'error': 'Seek by 10 seconds.'});
          return;
        }
        if (_pendingCommands >= 10) {
          await _reply(r, 429, {
            'error': 'Wait for the TV to finish this action.',
          });
          return;
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
        await _reply(r, 202, {'id': id, 'name': name, ...?status?.call()});
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
    await _persist('lan.tv', _phones);
    notifyListeners();
  }

  Future<void> close() async {
    if (closed) return;
    closed = true;
    code = null;
    _discovery?.close();
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
