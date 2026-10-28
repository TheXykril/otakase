import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/profiles.dart';
import 'package:otakase/data/tv_remote.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_test.dart' show MemoryTokens;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
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
      await tv.startTV(discoveryPort: 0);
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
    await tv.startTV(discoveryPort: 0);
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
      await tv.startTV(discoveryPort: 0);
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
