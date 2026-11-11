import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/remote_relay.dart';
import 'package:otakase/data/tv_remote.dart';

import 'auth_test.dart' show MemoryTokens;

/// A stand-in for the bot's relay (bot/remoterelay.go): one TV, phones
/// numbered c1, c2…
class FakeBot {
  late final HttpServer server;
  WebSocket? host;
  final phones = <String, WebSocket>{};
  final tvs = <Map<String, String>>[];
  int count = 0;

  String get base => 'http://127.0.0.1:${server.port}';

  Future<void> start() async {
    server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    server.listen((r) async {
      if (r.headers.value('authorization') != 'Bearer link') {
        r.response.statusCode = 401;
        await r.response.close();
        return;
      }
      if (r.uri.path == '/v1/remote/host') {
        final ws = await WebSocketTransformer.upgrade(r);
        host = ws;
        tvs.add({
          'id': r.uri.queryParameters['id']!,
          'name': r.uri.queryParameters['name']!,
        });
        ws.listen((raw) {
          final j = jsonDecode(raw as String) as Map;
          for (final e in phones.entries) {
            if (j['to'] == '' || j['to'] == e.key) {
              e.value.add(jsonEncode(j['data']));
            }
          }
        });
        return;
      }
      if (r.uri.path == '/v1/remote/control' && host != null) {
        final ws = await WebSocketTransformer.upgrade(r);
        final id = 'c${++count}';
        phones[id] = ws;
        host!.add(
          jsonEncode({
            'type': 'join',
            'from': id,
            'name': r.uri.queryParameters['name'],
          }),
        );
        ws.listen(
          (raw) => host?.add(
            jsonEncode({'from': id, 'data': jsonDecode(raw as String)}),
          ),
          onDone: () => phones.remove(id),
        );
        return;
      }
      r.response.statusCode = 404;
      await r.response.close();
    });
  }

  Future<void> close() async {
    for (final p in phones.values) {
      await p.close();
    }
    await host?.close();
    await server.close(force: true);
  }
}

class _RealHttp extends HttpOverrides {}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('a phone on the same account controls the TV through the bot, '
      'then pairs and moves to the local network', () async {
    HttpOverrides.global = _RealHttp();
    addTearDown(() => HttpOverrides.global = null);
    final bot = FakeBot();
    await bot.start();
    addTearDown(bot.close);
    final tv = TVRemote(
      storage: MemoryTokens(),
      name: 'Living room',
      findAddress: () async => '127.0.0.1',
      transport: const LanTransport(allowLoopback: true),
    );
    final phone = TVRemote(
      storage: MemoryTokens(),
      transport: const LanTransport(allowLoopback: true),
    );
    addTearDown(tv.close);
    addTearDown(phone.close);
    await tv.startTV(discoveryPort: 0, serverPort: 0);
    final actions = <String>[];
    var playing = true;
    tv.status = () => {
      'has_player': true,
      'title': 'Far away',
      'playing': playing,
    };
    tv.command = (action, args) async {
      actions.add(action);
      if (action == 'pause') playing = false;
    };
    final host = RelayHost(remote: tv, base: bot.base, token: () => 'link');
    addTearDown(host.close);
    host.start();
    final relay = RelayClient(
      base: bot.base,
      token: () => 'link',
      list: () async => {'tvs': bot.tvs},
    );
    Future<void> until(bool Function() ok) async {
      for (var i = 0; i < 300 && !ok(); i++) {
        await Future<void>.delayed(const Duration(milliseconds: 10));
      }
      expect(ok(), isTrue);
    }

    await until(() => bot.tvs.isNotEmpty);
    final listed = await relay.tvs();
    expect(listed.single.id, tv.id);
    expect(listed.single.name, 'Living room');
    expect(phone.isPaired(tv.id), isFalse);

    final link = RemoteLink(
      phone,
      listed.single,
      findTVs: () async => [],
      relay: relay,
      phoneName: 'Pixel',
      homeEvery: const Duration(milliseconds: 100),
    );
    addTearDown(link.close);
    link.start();
    await until(() => link.status['title'] == 'Far away');
    expect(link.away, isTrue);
    await link.send('pause');
    await until(() => link.status['playing'] == false);
    expect(actions, ['pause']);
    await expectLater(link.send('source'), throwsStateError);
    // The TV trusted the phone: it got a local pairing without Allow, and
    // the link moves to the local network.
    await until(() => phone.isPaired(tv.id));
    expect(tv.hasPhones, isTrue);
    await until(() => link.state == LinkState.live);
    expect(link.tv.port, tv.port);
    await link.send('play');
    await until(() => actions.length == 2);
  });

  test('without a link the relay stays off', () async {
    final relay = RelayClient(
      base: 'https://bot.example',
      token: () => '',
      list: () async => throw StateError('not called'),
    );
    expect(relay.available, isFalse);
    expect(await relay.tvs(), isEmpty);
    expect(
      relayUrl('https://bot.example/', '/v1/remote/host', {
        'id': 'a b',
      }).toString(),
      'wss://bot.example/v1/remote/host?id=a+b',
    );
  });
}
