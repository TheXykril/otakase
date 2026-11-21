import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/cast/car_screen.dart';
import 'package:otakase/cast/cast.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/models.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final messenger =
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  const car = MethodChannel('otakase/car');
  final calls = <MethodCall>[];
  var open = true;

  setUp(() {
    CarScreen.debugPhone = true;
    calls.clear();
    open = true;
    messenger.setMockMethodCallHandler(car, (call) async {
      calls.add(call);
      return switch (call.method) {
        'connected' => open,
        'status' => {
          'active': true,
          'device': 'Car screen',
          'kind': 'car',
          'state': 'paused',
          'position': 42.0,
          'duration': 1400.0,
          'paused': true,
          'load': 1,
          'note': 'Video plays only while the car is parked.',
        },
        'control' when (call.arguments as Map)['action'] == 'play' =>
          throw PlatformException(
            code: 'refused',
            message: 'Video plays only while the car is parked.',
          ),
        _ => null,
      };
    });
    // No TVs on the network in these tests.
    messenger.setMockMethodCallHandler(
      const MethodChannel('otakase/core'),
      (call) async => throw PlatformException(code: 'closed'),
    );
    messenger.setMockMethodCallHandler(
      const MethodChannel('otakase/device'),
      (call) async => null,
    );
  });
  tearDown(() {
    CarScreen.debugPhone = null;
    messenger.setMockMethodCallHandler(car, null);
    messenger.setMockMethodCallHandler(
      const MethodChannel('otakase/core'),
      null,
    );
    messenger.setMockMethodCallHandler(
      const MethodChannel('otakase/device'),
      null,
    );
  });

  test('the car screen is listed only while Android Auto shows it', () async {
    final client = CastClient(CoreClient());
    final devices = await client.devices(seconds: 0);
    expect(devices.map((d) => d.name), ['Car screen']);
    expect(devices.single.description, contains('parked'));
    open = false;
    expect(await CarScreen.instance.connected(), isFalse);
  });

  test('casting to the car goes to the car player, not the core', () async {
    final client = CastClient(CoreClient());
    await client.start(
      device: CarScreen.device,
      stream: ResolvedStream.fromJson({
        'url': 'https://cdn.example/master.m3u8',
        'headers': {'User-Agent': 'x'},
        'referrer': 'https://site.example/',
      }),
      title: 'Frieren · Episode 3',
      subtitle: 'https://cdn.example/en.vtt',
      start: 90,
    );
    final start = calls.singleWhere((c) => c.method == 'start');
    final args = start.arguments as Map;
    expect(args['hls'], isTrue);
    expect(args['headers'], {
      'User-Agent': 'x',
      'Referer': 'https://site.example/',
    });
    expect(args['start'], 90);
    final status = await client.status();
    expect(status.label, 'Paused on Car screen');
    expect(status.position, 42);
    expect(
      () => client.control('play'),
      throwsA(
        isA<CoreFailure>().having(
          (e) => e.message,
          'message',
          contains('parked'),
        ),
      ),
    );
  });

  test('Continue watching reaches the car once per change', () async {
    final anime = Anime(id: 7, title: 'Frieren');
    final entry = WatchEntry(anime: anime, episode: 4, position: 754);
    CarScreen.instance.offer([entry]);
    CarScreen.instance.offer([entry]);
    await Future<void>.delayed(Duration.zero);
    final shelves = calls.where((c) => c.method == 'shelf').toList();
    expect(shelves, hasLength(1));
    final item = (shelves.single.arguments as List).single as Map;
    expect(item['title'], 'Frieren');
    expect(item['detail'], 'Episode 4 · from 12:34');

    WatchEntry? played;
    CarScreen.instance.play = (e) => played = e;
    await messenger.handlePlatformMessage(
      'otakase/car',
      const StandardMethodCodec().encodeMethodCall(
        MethodCall('pick', 'al:7#4'),
      ),
      (_) {},
    );
    expect(played?.episode, 4);
    CarScreen.instance.play = null;
  });
}
