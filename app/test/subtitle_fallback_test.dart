import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/external_subtitles.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/subtitle_fallback.dart';

const blocked = SubtitleFailure(
  'The subtitle certificate chain is not trusted.',
  host: 'blocked.example',
);

PlaybackSource source(String name, {int copies = 1, String mode = 'sub'}) =>
    PlaybackSource(
      provider: name,
      showId: name,
      mode: mode,
      streams: [
        for (var i = 0; i < copies; i++)
          ResolvedStream.fromJson({
            'url': 'https://$name.example/$i/video.m3u8',
            'subtitle': 'https://$name.example/$i/english.vtt',
          }),
      ],
    );

void main() {
  test(
    'tries the next mirror then resolves a different matched video and track',
    () async {
      final initial = source('first', copies: 2);
      final alternate = source('second');
      final opened = <String>[];
      final result = await selectSubtitleCopy(
        initial: initial,
        initialCopy: 0,
        enabled: true,
        scope: RequestScope(),
        openCopy: (candidate, index) async {
          final stream = candidate.streams[index];
          opened.add('${stream.url} + ${stream.subtitle}');
          return (
            selected: candidate == alternate,
            failure: candidate == initial && index == 0 ? blocked : null,
          );
        },
        nextSource: (tried) async {
          expect(tried, {'first'});
          return alternate;
        },
      );
      expect(result, isNull);
      expect(opened, [
        'https://first.example/0/video.m3u8 + https://first.example/0/english.vtt',
        'https://first.example/1/video.m3u8 + https://first.example/1/english.vtt',
        'https://second.example/0/video.m3u8 + https://second.example/0/english.vtt',
      ]);
    },
  );

  test(
    'stops on the first working mirror before looking up providers',
    () async {
      final result = await selectSubtitleCopy(
        initial: source('first', copies: 2),
        initialCopy: 0,
        enabled: true,
        scope: RequestScope(),
        openCopy: (_, index) async =>
            (selected: index == 1, failure: index == 0 ? blocked : null),
        nextSource: (_) async => fail('the second mirror had subtitles'),
      );
      expect(result, isNull);
    },
  );

  test(
    'leaves dub, subtitles-off and missing or non-network tracks alone',
    () async {
      for (final (mode, enabled, selected, failure) in [
        ('dub', true, false, blocked),
        ('sub', false, false, blocked),
        ('sub', true, true, null),
        ('sub', true, false, null),
        ('sub', true, false, const SubtitleFailure('Invalid file')),
      ]) {
        expect(
          await selectSubtitleCopy(
            initial: source('first', mode: mode),
            initialCopy: 0,
            enabled: enabled,
            scope: RequestScope(),
            openCopy: (_, _) async => (selected: selected, failure: failure),
            nextSource: (_) async => fail('no source change expected'),
          ),
          isNull,
        );
      }
    },
  );

  test('keeps the original hostname when all alternatives fail', () async {
    final result = await selectSubtitleCopy(
      initial: source('first'),
      initialCopy: 0,
      enabled: true,
      scope: RequestScope(),
      openCopy: (candidate, _) async {
        if (candidate.provider == 'second') {
          throw const CoreFailure('playback', 'Cannot open video');
        }
        return (selected: false, failure: blocked);
      },
      nextSource: (tried) async =>
          tried.contains('second') ? null : source('second'),
    );
    expect(result?.host, 'blocked.example');
  });

  test(
    'cancelling playback prevents another mirror or provider from opening',
    () async {
      final scope = RequestScope();
      await expectLater(
        selectSubtitleCopy(
          initial: source('first', copies: 2),
          initialCopy: 0,
          enabled: true,
          scope: scope,
          openCopy: (_, index) async {
            expect(index, 0);
            scope.cancel();
            return (selected: false, failure: blocked);
          },
          nextSource: (_) async => fail('cancelled playback'),
        ),
        throwsA(isA<CoreFailure>().having((e) => e.code, 'code', 'cancelled')),
      );
    },
  );

  test('native TLS errors record just the hostname for fallback', () async {
    final stream = source('first').streams.single;
    await expectLater(
      downloadSubtitle(
        MockClient((_) async => fail('must use native core')),
        stream,
        'https://blocked.example/english.vtt?token=private',
        nativeCore: _BlockedCore(),
      ),
      throwsA(
        isA<SubtitleFailure>().having((e) => e.host, 'host', 'blocked.example'),
      ),
    );
  });
}

class _BlockedCore extends CoreClient {
  @override
  Future<Json> request(
    Json input, {
    void Function(int)? onHandle,
    RequestScope? scope,
    Duration timeout = const Duration(seconds: 40),
  }) async {
    throw const CoreFailure(
      'subtitle_tls_chain',
      'Untrusted certificate chain',
    );
  }
}
