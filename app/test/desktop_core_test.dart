import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/desktop/desktop_core.dart';

/// A stand-in for the otakase-core helper. Messages the app writes arrive in
/// [sent]; [reply] writes a line back.
class FakeHelper implements Process {
  FakeHelper() {
    _input.stream
        .cast<List<int>>()
        .transform(utf8.decoder)
        .transform(const LineSplitter())
        .listen((line) => sent.add(jsonDecode(line) as Map<String, dynamic>));
  }

  final _input = StreamController<List<int>>();
  final _output = StreamController<List<int>>();
  final _exit = Completer<int>();
  final sent = <Map<String, dynamic>>[];
  late final IOSink _stdin = IOSink(_input.sink);

  void reply(Map<String, Object?> message) =>
      _output.add(utf8.encode('${jsonEncode(message)}\n'));

  void crash() {
    _output.close();
    if (!_exit.isCompleted) _exit.complete(2);
  }

  @override
  IOSink get stdin => _stdin;
  @override
  Stream<List<int>> get stdout => _output.stream;
  @override
  Stream<List<int>> get stderr => const Stream.empty();
  @override
  Future<int> get exitCode => _exit.future;
  @override
  int get pid => 1;
  @override
  bool kill([ProcessSignal signal = ProcessSignal.sigterm]) {
    crash();
    return true;
  }
}

Future<void> settle() => Future<void>.delayed(Duration.zero);

void main() {
  late List<FakeHelper> helpers;
  late DesktopCore core;

  setUp(() {
    helpers = [];
    core = DesktopCore(
      executable: 'otakase-core',
      launcher: (_) async {
        final helper = FakeHelper();
        helpers.add(helper);
        return helper;
      },
    );
  });

  test('sends requests and returns results by id', () async {
    final first = await core.start('{"version":1,"operation":"providers"}');
    final second = await core.start('{"version":1,"operation":"stop"}');
    await settle();
    expect(helpers, hasLength(1));
    final helper = helpers.single;
    expect(helper.sent[0], {
      'id': first,
      'op': 'request',
      'input': {'version': 1, 'operation': 'providers'},
    });
    // Answers come back in completion order.
    helper.reply({
      'id': second,
      'result': {
        'version': 1,
        'data': {'stopped': true},
      },
    });
    helper.reply({
      'id': first,
      'result': {
        'version': 1,
        'data': {
          'providers': ['anikoto'],
        },
      },
    });
    expect(jsonDecode(await core.result(first))['data'], {
      'providers': ['anikoto'],
    });
    expect(jsonDecode(await core.result(second))['data'], {'stopped': true});
  });

  test('a result is collected once', () async {
    final id = await core.start('{"version":1}');
    await settle();
    helpers.single.reply({
      'id': id,
      'result': {'version': 1, 'data': {}},
    });
    await core.result(id);
    final again = jsonDecode(await core.result(id));
    expect(again['error']['code'], 'invalid_handle');
  });

  test('cancel reaches the helper while the result is awaited', () async {
    final id = await core.start('{"version":1}');
    final pending = core.result(id);
    await core.cancel(id);
    await settle();
    expect(helpers.single.sent.last, {'id': id, 'op': 'cancel'});
    helpers.single.reply({
      'id': id,
      'result': {
        'version': 1,
        'error': {'code': 'cancelled', 'message': 'x', 'retryable': false},
      },
    });
    expect(jsonDecode(await pending)['error']['code'], 'cancelled');
    // A finished request is not cancelled again.
    final count = helpers.single.sent.length;
    await core.cancel(id);
    await settle();
    expect(helpers.single.sent, hasLength(count));
  });

  test('a crashed helper fails its requests and is started again', () async {
    final id = await core.start('{"version":1}');
    final pending = core.result(id);
    helpers.single.crash();
    final failed = jsonDecode(await pending);
    expect(failed['error']['code'], 'core_exited');
    expect(failed['error']['retryable'], true);

    final next = await core.start('{"version":1}');
    await settle();
    expect(helpers, hasLength(2));
    helpers.last.reply({
      'id': next,
      'result': {'version': 1, 'data': {}},
    });
    expect(jsonDecode(await core.result(next))['data'], <String, Object?>{});
  });

  test('ignores lines that are not replies', () async {
    final id = await core.start('{"version":1}');
    await settle();
    final helper = helpers.single;
    helper._output.add(utf8.encode('not json\n'));
    helper.reply({'event': 'ready', 'version': 1});
    helper.reply({
      'event': 'error',
      'result': {'version': 1},
    });
    helper.reply({
      'id': id,
      'result': {
        'version': 1,
        'data': {'ok': 1},
      },
    });
    expect(jsonDecode(await core.result(id))['data'], {'ok': 1});
  });

  test('a missing helper reports core_missing', () async {
    final missing = DesktopCore(
      executable: '/nonexistent/otakase-core',
      launcher: (path) => throw ProcessException(path, const [], 'not found'),
    );
    await expectLater(
      missing.start('{"version":1}'),
      throwsA(
        isA<Exception>().having(
          (e) => e.toString(),
          'message',
          contains('core_missing'),
        ),
      ),
    );
  });
}
