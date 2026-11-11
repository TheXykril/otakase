import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/lan_page.dart';
import 'package:otakase/data/profiles.dart';
import 'package:otakase/data/tv_remote.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_test.dart' show MemoryTokens;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  lookupTests();
  test(
    'QR and discovery accept only local literal IPv4 with valid code and port',
    () {
      const tv = TVDevice(
        id: 'tv-1',
        name: 'TV',
        host: '192.168.1.8',
        port: 12345,
      );
      final code = RemotePairCode(tv, '012345', DateTime.now());
      expect(RemotePairCode.fromUri(code.qr).device.host, tv.host);
      for (final q in [
        {'host': '8.8.8.8'},
        {'host': 'local.example'},
        {'port': '65536'},
        {'code': 'abc'},
      ]) {
        expect(
          () => RemotePairCode.fromUri(
            code.qr.replace(
              queryParameters: {...code.qr.queryParameters, ...q},
            ),
          ),
          throwsFormatException,
        );
      }
      final raw = jsonEncode({
        'otakase': 1,
        'kind': 'tv',
        'id': 'tv-1',
        'name': 'TV',
        'port': 12345,
      });
      expect(parseTVAdvertisement(raw, '192.168.1.8')!.id, 'tv-1');
      expect(parseTVAdvertisement(raw, '8.8.8.8'), null);
    },
  );
  test(
    'a TV that never answers explains the firewall, not the socket',
    () async {
      final closed = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
      final port = closed.port;
      await closed.close();
      final remote = TVRemote(
        storage: MemoryTokens(),
        transport: const LanTransport(allowLoopback: true),
      );
      await expectLater(
        remote.pair(
          TVDevice(id: 'tv-1', name: 'TV', host: '127.0.0.1', port: port),
          '012345',
        ),
        throwsA(
          isA<StateError>().having(
            (e) => e.message,
            'message',
            allOf(
              contains('Could not reach 127.0.0.1'),
              contains('Allow phone remote'),
              isNot(contains('SocketException')),
            ),
          ),
        ),
      );
      await remote.close();
    },
  );
  test(
    'pairing stores hashed TV token and sends authenticated commands',
    () async {
      final tvTokens = MemoryTokens(), phoneTokens = MemoryTokens();
      final tv = TVRemote(
        storage: tvTokens,
        name: 'Living room',
        findAddress: () async => '127.0.0.1',
        transport: const LanTransport(allowLoopback: true),
      );
      final phone = TVRemote(
        storage: phoneTokens,
        transport: const LanTransport(allowLoopback: true),
      );
      addTearDown(tv.close);
      addTearDown(phone.close);
      await tv.startTV(discoveryPort: 0, serverPort: 0);
      final code = await tv.beginPair(),
          actions = <String>[],
          events = <String>[];
      tv.status = () => {
        'has_player': true,
        'title': 'A story',
        'episode': 2,
        'episodes': 12,
        'playing': true,
        'volume': 50,
      };
      tv.command = (action, args) async {
        actions.add('$action:${jsonEncode(args)}');
      };
      final sub = tv.events.listen(events.add);
      addTearDown(sub.cancel);
      await phone.pair(code.device, code.code, phoneName: 'My phone');
      final saved = jsonDecode(phoneTokens.values['lan.remote']!)[tv.id] as Map;
      expect(tvTokens.values['lan.tv'], isNot(contains(saved['token'])));
      expect(tv.code, null);
      expect(phone.paired.single.id, tv.id);
      expect((await phone.send(code.device))['title'], 'A story');
      expect(events, ['My phone connected']);
      for (final entry in {
        'play': <String, dynamic>{},
        'pause': <String, dynamic>{},
        'seek': {'seconds': 10},
        'next': <String, dynamic>{},
        'episode': {'episode': 4},
        'volume': {'volume': 60},
      }.entries) {
        await phone.send(code.device, action: entry.key, args: entry.value);
      }
      await Future<void>.delayed(Duration.zero);
      expect(actions.length, 6);
      await expectLater(
        phone.send(code.device, action: 'seek', args: {'seconds': 85}),
        throwsStateError,
      );
      await expectLater(
        phone.send(code.device, action: 'source'),
        throwsStateError,
      );
      await tv.forgetPhones();
      await expectLater(phone.send(code.device), throwsStateError);
      await phone.forget(tv.id);
      expect(phone.paired, isEmpty);
    },
  );
  test('codes expire and five wrong attempts wait 30 seconds', () async {
    var clock = DateTime(2027, 1, 1);
    final tv = TVRemote(
          storage: MemoryTokens(),
          findAddress: () async => '127.0.0.1',
          transport: const LanTransport(allowLoopback: true),
          now: () => clock,
        ),
        phone = TVRemote(
          storage: MemoryTokens(),
          transport: const LanTransport(allowLoopback: true),
        );
    addTearDown(tv.close);
    addTearDown(phone.close);
    await tv.startTV(discoveryPort: 0, serverPort: 0);
    final code = await tv.beginPair();
    final wrong = code.code == '000000' ? '111111' : '000000';
    for (var i = 0; i < 5; i++) {
      await expectLater(phone.pair(code.device, wrong), throwsStateError);
    }
    await expectLater(
      phone.pair(code.device, code.code),
      throwsA(
        isA<StateError>().having(
          (e) => e.message,
          'message',
          contains('30 seconds'),
        ),
      ),
    );
    clock = clock.add(const Duration(seconds: 31));
    await phone.pair(code.device, code.code);
    final expired = await tv.beginPair();
    clock = clock.add(const Duration(minutes: 11));
    await expectLater(
      phone.pair(expired.device, expired.code),
      throwsA(
        isA<StateError>().having(
          (e) => e.message,
          'message',
          contains('new pairing code'),
        ),
      ),
    );
  });
  test(
    'episode commands return before loading completes and stay ordered',
    () async {
      final tv = TVRemote(
            storage: MemoryTokens(),
            findAddress: () async => '127.0.0.1',
            transport: const LanTransport(allowLoopback: true),
          ),
          phone = TVRemote(
            storage: MemoryTokens(),
            transport: const LanTransport(allowLoopback: true),
          );
      addTearDown(tv.close);
      addTearDown(phone.close);
      await tv.startTV(discoveryPort: 0, serverPort: 0);
      final code = await tv.beginPair();
      await phone.pair(code.device, code.code);
      final opening = Completer<void>(), actions = <String>[];
      tv.status = () => {'has_player': true};
      tv.command = (action, args) async {
        actions.add(action);
        if (action == 'next') await opening.future;
      };
      await phone
          .send(code.device, action: 'next')
          .timeout(const Duration(seconds: 1));
      await phone
          .send(code.device, action: 'pause')
          .timeout(const Duration(seconds: 1));
      expect(actions, ['next']);
      opening.complete();
      await Future<void>.delayed(Duration.zero);
      expect(actions, ['next', 'pause']);
    },
  );
  test(
    'production transport rejects public addresses before connecting',
    () async {
      await expectLater(
        const LanTransport().post(
          const TVDevice(id: 'bad', name: 'Bad', host: '8.8.8.8', port: 80),
          '/pair',
          {},
        ),
        throwsStateError,
      );
      expect(const LanTransport().allowed('127.0.0.1'), false);
      expect(const LanTransport().allowed('172.16.0.1'), true);
      expect(const LanTransport().allowed('172.32.0.1'), false);
    },
  );
  test('remote pairings and TV identity stay inside their profile', () async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance(), base = MemoryTokens();
    final one = ProfileTokens(base, prefs, '1'),
        two = ProfileTokens(base, prefs, '2');
    await one.write(
      'lan.remote',
      jsonEncode({
        'tv-1': {
          'id': 'tv-1',
          'name': 'Saved TV',
          'host': '192.168.1.8',
          'port': 12345,
          'token': 'secret',
          'client': 'phone',
        },
      }),
    );
    await one.write(
      'lan.tv',
      jsonEncode({'id': 'profile-one-tv', 'clients': {}}),
    );
    final first = TVRemote(storage: one), second = TVRemote(storage: two);
    await first.initialize();
    await second.initialize();
    expect(first.paired.single.id, 'tv-1');
    expect(second.paired, isEmpty);
    expect(first.id, isNot(second.id));
    await first.close();
    await one.deactivate();
    await expectLater(second.send(first.paired.single), throwsStateError);
    await second.close();
    await two.deactivate();
  });
}

void lookupTests() {
  test('home network addresses skip container bridges and mobile data', () {
    expect(lanInterfaceRank('wlan0'), lessThan(lanInterfaceRank('eth0')));
    expect(lanInterfaceRank('wlp3s0'), 0);
    expect(lanInterfaceRank('enp0s31f6'), 1);
    for (final name in [
      'docker0',
      'br-1a2b',
      'virbr0',
      'podman0',
      'tailscale0',
      'wg0',
      'rmnet_data0',
    ]) {
      expect(lanInterfaceRank(name), 9, reason: name);
    }
    expect(directedBroadcasts(['192.168.1.20', '8.8.8.8', '10.0.4.7']), [
      '192.168.1.255',
      '10.0.4.255',
    ]);
    final hosts = neighbourHosts(['192.168.1.20']);
    expect(hosts.length, 253);
    expect(hosts, isNot(contains('192.168.1.20')));
    expect(hosts, containsAll(['192.168.1.1', '192.168.1.254']));
    expect(parseTVAddress(' 192.168.1.20 '), ('192.168.1.20', tvDiscoveryPort));
    expect(parseTVAddress('192.168.1.20:9000'), ('192.168.1.20', 9000));
    expect(parseTVAddress('8.8.8.8'), null);
    expect(parseTVAddress('192.168.1'), null);
    expect(parseTVAddress('192.168.1.20:70000'), null);
  });
  test('phones pair from the code alone, without a discovered TV', () async {
    final tv = TVRemote(
          storage: MemoryTokens(),
          name: 'Laptop',
          findAddress: () async => '127.0.0.1',
          transport: const LanTransport(allowLoopback: true),
        ),
        other = TVRemote(
          storage: MemoryTokens(),
          name: 'Other TV',
          findAddress: () async => '127.0.0.1',
          transport: const LanTransport(allowLoopback: true),
        ),
        phone = TVRemote(
          storage: MemoryTokens(),
          transport: const LanTransport(allowLoopback: true),
        );
    addTearDown(tv.close);
    addTearDown(other.close);
    addTearDown(phone.close);
    await tv.startTV(discoveryPort: 0, serverPort: 0);
    await other.startTV(discoveryPort: 0, serverPort: 0);
    final code = await tv.beginPair();
    await other.beginPair();
    final hello = await helloTV(
      '127.0.0.1',
      port: tv.port,
      transport: const LanTransport(allowLoopback: true),
    );
    expect(hello?.id, tv.id);
    expect(hello?.name, 'Laptop');
    expect(hello?.port, tv.port);
    // Several TVs nearby: the one whose code matches is the one paired.
    final paired = await phone.pairByCode(
      code.code,
      findTVs: () async => [
        (await helloTV(
          '127.0.0.1',
          port: other.port,
          transport: const LanTransport(allowLoopback: true),
        ))!,
        hello!,
      ],
    );
    expect(paired.id, tv.id);
    expect(phone.paired.single.id, tv.id);
    await expectLater(
      phone.pairByCode('123456', findTVs: () async => []),
      throwsA(
        isA<StateError>().having(
          (e) => e.message,
          'message',
          contains('address shown under the code'),
        ),
      ),
    );
    await expectLater(
      phone.pairByCode('123456', address: 'living room'),
      throwsStateError,
    );
  });

  Future<(TVRemote, TVRemote, TVDevice)> tvAndPhone() async {
    final tv = TVRemote(
          storage: MemoryTokens(),
          name: 'Living room',
          findAddress: () async => '127.0.0.1',
          transport: const LanTransport(allowLoopback: true),
        ),
        phone = TVRemote(
          storage: MemoryTokens(),
          transport: const LanTransport(allowLoopback: true),
        );
    addTearDown(tv.close);
    addTearDown(phone.close);
    await tv.startTV(discoveryPort: 0, serverPort: 0);
    final found = await helloTV(
      '127.0.0.1',
      port: tv.port,
      transport: const LanTransport(allowLoopback: true),
    );
    return (tv, phone, found!);
  }

  test('a phone asks and the TV allows it without typing a code', () async {
    final (tv, phone, device) = await tvAndPhone();
    expect(device.version, remoteVersion);
    String? shown;
    final asking = phone.requestControl(
      device,
      phoneName: 'Pixel',
      digits: (d) => shown = d,
      every: const Duration(milliseconds: 20),
    );
    while (tv.ask == null) {
      await Future<void>.delayed(const Duration(milliseconds: 5));
    }
    expect(tv.ask!.phone, 'Pixel');
    expect(tv.ask!.digits, matches(RegExp(r'^\d\d$')));
    // A second phone cannot cover the question already on screen.
    final other = TVRemote(
      storage: MemoryTokens(),
      transport: const LanTransport(allowLoopback: true),
    );
    addTearDown(other.close);
    await expectLater(other.requestControl(device), throwsStateError);
    await tv.allowAsk();
    await asking;
    expect(shown, isNotEmpty);
    expect(tv.ask, null);
    expect(phone.paired.single.id, tv.id);
    tv.status = () => {'has_player': false};
    expect((await phone.send(device))['name'], 'Living room');
  });

  test('Not now refuses, and three refusals make phones wait', () async {
    final (tv, phone, device) = await tvAndPhone();
    for (var i = 0; i < 3; i++) {
      final asking = phone.requestControl(
        device,
        every: const Duration(milliseconds: 20),
      );
      while (tv.ask == null) {
        await Future<void>.delayed(const Duration(milliseconds: 5));
      }
      tv.denyAsk();
      await expectLater(
        asking,
        throwsA(
          isA<StateError>().having(
            (e) => e.message,
            'message',
            'The TV said not now.',
          ),
        ),
      );
    }
    await expectLater(
      phone.requestControl(device),
      throwsA(
        isA<StateError>().having(
          (e) => e.message,
          'message',
          contains('Wait 30'),
        ),
      ),
    );
    expect(phone.paired, isEmpty);
  });

  test('the live connection pushes status and carries controls', () async {
    HttpOverrides.global = _RealHttp();
    addTearDown(() => HttpOverrides.global = null);
    final (tv, phone, device) = await tvAndPhone();
    final code = await tv.beginPair();
    await phone.pair(device, code.code);
    var playing = true;
    final actions = <String>[];
    tv.status = () => {
      'has_player': true,
      'title': 'A story',
      'playing': playing,
    };
    tv.command = (action, args) async {
      actions.add(action);
      if (action == 'pause') playing = false;
    };
    final link = RemoteLink(phone, device, findTVs: () async => []);
    addTearDown(link.close);
    link.start();
    Future<void> until(bool Function() ok) async {
      for (var i = 0; i < 200 && !ok(); i++) {
        await Future<void>.delayed(const Duration(milliseconds: 10));
      }
      expect(ok(), isTrue);
    }

    await until(() => link.status['title'] == 'A story');
    expect(link.state, LinkState.live);
    await link.send('pause');
    await until(() => link.status['playing'] == false);
    expect(actions, ['pause']);
    await expectLater(link.send('source'), throwsStateError);
    for (final (action, args) in [
      ('seek_to', {'position': 300}),
      ('subtitle', {'id': '2'}),
      ('audio', {'id': '1'}),
      ('quality', {'value': '720'}),
      ('previous', <String, dynamic>{}),
      ('party_skip', <String, dynamic>{}),
    ]) {
      await link.send(action, args: args);
    }
    for (final (action, args) in [
      ('seek_to', {'position': -5}),
      ('subtitle', {'id': 2}),
      ('quality', {'value': '../x'}),
    ]) {
      await expectLater(link.send(action, args: args), throwsStateError);
    }
    await until(() => actions.length == 7);
    expect(actions.skip(1), [
      'seek_to',
      'subtitle',
      'audio',
      'quality',
      'previous',
      'party_skip',
    ]);
    // Forgotten phones are cut off and told to pair again.
    await tv.forgetPhones();
    await until(() => link.state == LinkState.lost);
    expect(link.error, contains('Pair this phone'));
  });

  test('a phone starts an episode on the TV with nothing playing', () async {
    final (tv, phone, device) = await tvAndPhone();
    final code = await tv.beginPair();
    await phone.pair(device, code.code);
    tv.status = () => {'has_player': false};
    await expectLater(
      phone.send(
        device,
        action: 'start',
        args: {'anilist_id': 1, 'episode': 2},
      ),
      throwsA(
        isA<StateError>().having(
          (e) => e.message,
          'message',
          contains('Open Otakase'),
        ),
      ),
    );
    final started = <String>[];
    tv.start = (id, episode, position, mode) async {
      started.add('$id:$episode:$position:$mode');
    };
    await phone.send(
      device,
      action: 'start',
      args: {
        'anilist_id': 154587,
        'episode': 8,
        'position': 492,
        'mode': 'dub',
      },
    );
    await phone.send(
      device,
      action: 'start',
      args: {'anilist_id': 5, 'episode': 1},
    );
    for (final bad in [
      {'anilist_id': 0, 'episode': 1},
      {'anilist_id': 5, 'episode': 0},
      {'anilist_id': 5, 'episode': 1, 'position': -1},
      {'anilist_id': 5, 'episode': 1, 'mode': 'raw'},
      {'anilist_id': '5', 'episode': 1},
    ]) {
      await expectLater(
        phone.send(device, action: 'start', args: bad),
        throwsStateError,
      );
    }
    await Future<void>.delayed(const Duration(milliseconds: 20));
    expect(started, ['154587:8:492:dub', '5:1:0:sub']);
  });

  test('the live connection finds the TV again at a new address', () async {
    HttpOverrides.global = _RealHttp();
    addTearDown(() => HttpOverrides.global = null);
    final (tv, phone, device) = await tvAndPhone();
    final code = await tv.beginPair();
    await phone.pair(device, code.code);
    tv.status = () => {'has_player': false, 'title': 'Moved'};
    // The saved address no longer answers, as after a router restart.
    final gone = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
    final stale = TVDevice(
      id: device.id,
      name: device.name,
      host: device.host,
      port: gone.port,
      version: 2,
    );
    await gone.close();
    final link = RemoteLink(phone, stale, findTVs: () async => [device]);
    addTearDown(link.close);
    link.start();
    for (var i = 0; i < 600 && link.status['title'] != 'Moved'; i++) {
      await Future<void>.delayed(const Duration(milliseconds: 10));
    }
    expect(link.status['title'], 'Moved');
    expect(link.tv.port, device.port);
    expect(phone.paired.single.port, device.port);
  });
}

/// The test binding answers every HttpClient with 400; the live remote
/// needs real loopback sockets.
class _RealHttp extends HttpOverrides {}
