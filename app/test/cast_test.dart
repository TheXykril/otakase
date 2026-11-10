import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/cast/cast.dart';
import 'package:otakase/core.dart';

/// Answers the core channel the way the Go core does, and records requests.
class FakeCore {
  final requests = <Map<String, dynamic>>[];
  final pending = <int, String>{};
  int next = 0;

  Map<String, dynamic> answer(Map<String, dynamic> request) =>
      switch (request['operation']) {
        'cast_devices' => {
          'devices': [
            {'id': 'browser:browser', 'name': 'Browser', 'kind': 'BROWSER'},
            {
              'id': 'chromecast:abc',
              'name': 'Living Room TV',
              'kind': 'Chromecast',
              'address': '192.168.1.20:8009',
            },
          ],
        },
        'cast_status' => {
          'active': true,
          'device': 'Browser',
          'kind': 'BROWSER',
          'state': 'playing',
          'position': 61.5,
          'duration': 1420,
          'load': 2,
          'link': 'http://192.168.1.5:8012/abcde',
          'viewers': 0,
        },
        _ => {'ok': true},
      };

  void install() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('otakase/core'), (
          call,
        ) async {
          switch (call.method) {
            case 'start':
              final request = (jsonDecode(call.arguments as String) as Map)
                  .cast<String, dynamic>();
              requests.add(request);
              pending[++next] = jsonEncode({
                'version': 1,
                'data': answer(request),
              });
              return next;
            case 'result':
              return pending.remove(call.arguments as int);
          }
          return null;
        });
  }
}

void main() {
  late FakeCore core;
  setUp(() {
    core = FakeCore()..install();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          const MethodChannel('otakase/device'),
          (call) async => null,
        );
  });
  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('otakase/core'), null);
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('otakase/device'), null);
  });

  test('start sends the stream with its headers and subtitles', () async {
    final client = CastClient(CoreClient());
    final devices = await client.devices();
    expect(devices.map((d) => d.name), ['Browser', 'Living Room TV']);
    expect(devices.first.isBrowser, isTrue);
    await client.start(
      device: devices.last,
      stream: ResolvedStream.fromJson({
        'url': 'https://cdn.example/master.m3u8',
        'headers': {'User-Agent': 'x'},
        'referrer': 'https://site.example/',
      }),
      title: 'Frieren · Episode 3',
      subtitle: 'https://cdn.example/en.vtt',
      start: 90,
    );
    final cast = core.requests.last['cast'] as Map;
    expect(core.requests.last['operation'], 'cast_start');
    expect(cast['device'], 'chromecast:abc');
    expect(cast['hls'], isTrue);
    expect(cast['headers'], {
      'User-Agent': 'x',
      'Referer': 'https://site.example/',
    });
    expect(cast['subtitle'], 'https://cdn.example/en.vtt');
    expect(cast['start'], 90);
    final status = await client.status();
    expect(status.playing, isTrue);
    expect(status.position, 61.5);
    expect(status.label, 'Playing on Browser');
  });

  testWidgets('picker lists TVs before the browser and returns the pick', (
    tester,
  ) async {
    CastDevice? picked;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            onPressed: () async => picked = await pickCastDevice(
              context,
              CastClient(CoreClient()),
            ),
            child: const Text('open'),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    for (
      var i = 0;
      i < 20 && find.text('Living Room TV').evaluate().isEmpty;
      i++
    ) {
      await tester.pump(const Duration(milliseconds: 50));
    }
    await tester.pumpAndSettle();
    expect(find.text('Living Room TV'), findsOneWidget);
    expect(find.text('Chromecast · 192.168.1.20:8009'), findsOneWidget);
    final tv = tester.getTopLeft(find.text('Living Room TV'));
    final browser = tester.getTopLeft(find.text('Browser'));
    expect(tv.dy, lessThan(browser.dy));
    await tester.tap(find.text('Living Room TV'));
    await tester.pumpAndSettle();
    expect(picked?.id, 'chromecast:abc');
  });

  testWidgets('panel shows the browser link and stops', (tester) async {
    final status = await CastClient(CoreClient()).status();
    var stopped = false;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: CastPanel(
            status: status,
            deviceName: 'Browser',
            onStop: () => stopped = true,
          ),
        ),
      ),
    );
    expect(find.text('http://192.168.1.5:8012/abcde'), findsOneWidget);
    await tester.tap(find.byKey(const Key('cast-stop')));
    expect(stopped, isTrue);
  });

  testWidgets('panel offers to open the firewall when the TV was blocked', (
    tester,
  ) async {
    final status = CastStatus.fromJson({
      'active': true,
      'device': 'Office TV',
      'state': 'error',
      'error': 'The device could not reach this computer.',
      'blocked': true,
      'can_allow': true,
    });
    var allowed = false;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: CastPanel(
            status: status,
            deviceName: 'Office TV',
            onStop: () {},
            onAllow: () => allowed = true,
          ),
        ),
      ),
    );
    expect(
      find.text('The device could not reach this computer.'),
      findsOneWidget,
    );
    await tester.tap(find.byKey(const Key('cast-allow')));
    expect(allowed, isTrue);
  });

  testWidgets('panel hides Allow casting without a firewall to open', (
    tester,
  ) async {
    final status = CastStatus.fromJson({
      'active': true,
      'device': 'Office TV',
      'state': 'error',
      'blocked': true,
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: CastPanel(
            status: status,
            deviceName: 'Office TV',
            onStop: () {},
            onAllow: () {},
          ),
        ),
      ),
    );
    expect(find.byKey(const Key('cast-allow')), findsNothing);
  });
}
