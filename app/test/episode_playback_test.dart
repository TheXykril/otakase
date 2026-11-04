import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/episode_completion.dart';
import 'package:otakase/data/external_subtitles.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/skip_times.dart';

Duration seconds(int value) => Duration(seconds: value);

void main() {
  test('EOF must be within the final seconds of the known duration', () {
    expect(reachedEpisodeEnd(seconds(600), seconds(1440)), isFalse);
    expect(reachedEpisodeEnd(seconds(1410), seconds(1440)), isFalse);
    expect(reachedEpisodeEnd(seconds(1437), seconds(1440)), isTrue);
    expect(reachedEpisodeEnd(seconds(20), Duration.zero), isFalse);
    expect(reachedEpisodeEnd(seconds(6), seconds(10)), isFalse);
  });

  final runtime = seconds(1440);
  EpisodeViewing watchedThrough(int time) =>
      EpisodeViewing()..start(runtime, [1440000, time * 1000]);

  test('terminal credits keep the short-tail tolerance', () {
    const credits = [SkipSpan('ed', 1290, 1437)];
    final viewing = watchedThrough(1290);
    expect(viewing.credits(seconds(1290), runtime, credits), isTrue);
    expect(viewing.credits(seconds(1289), runtime, credits), isFalse);
    expect(
      watchedThrough(600).credits(seconds(1290), runtime, credits),
      isFalse,
    );
    expect(
      viewing.credits(seconds(1290), runtime, [
        const SkipSpan('ed', 1290, 1400),
      ]),
      isFalse,
    );
  });

  test('leaving at 85 percent or seeking from the middle does not finish', () {
    final viewing = watchedThrough(600)
      ..seek(seconds(600), seconds(1438))
      ..sample(seconds(1438), playing: true)
      ..sample(seconds(1439), playing: true);
    expect(viewing.ended(seconds(1439), runtime, []), isFalse);
    expect(watchedThrough(1224).credits(seconds(1224), runtime, []), isFalse);
  });

  test('normal end and an unmarked outro skip can finish without markers', () {
    expect(watchedThrough(1438).ended(seconds(1439), runtime, []), isTrue);
    final viewing = watchedThrough(1340)
      ..seek(seconds(1340), seconds(1437))
      ..sample(seconds(1437), playing: true);
    expect(viewing.ended(seconds(1437), runtime, []), isFalse);
    viewing.sample(seconds(1438), playing: true);
    expect(viewing.ended(seconds(1438), runtime, []), isTrue);
  });

  test('marker skip lands before post-credit story and waits for playback', () {
    const ed = SkipSpan('ed', 1290, 1400);
    final viewing = watchedThrough(1290)
      ..seek(seconds(1290), seconds(1400), span: ed)
      ..sample(seconds(1290), playing: true);
    expect(viewing.checkpoint, [1440000, 1290000]);
    viewing.sample(seconds(1400), playing: true);
    expect(viewing.checkpoint, [1440000, 1400000]);
    expect(viewing.credits(seconds(1400), runtime, [ed]), isFalse);
    expect(viewing.ended(seconds(1400), runtime, [ed]), isFalse);
  });

  test(
    'ordinary seek steps preserve viewing without crediting a long jump',
    () {
      final viewing = watchedThrough(100)
        ..seek(seconds(100), seconds(110))
        ..sample(seconds(110), playing: true);
      expect(viewing.checkpoint, [1440000, 110000, 10000]);
      viewing.seek(seconds(110), seconds(160));
      viewing.sample(seconds(160), playing: true);
      expect(viewing.checkpoint, [1440000, 110000, 10000]);
    },
  );

  test('terminal auto-skip counts when EOF arrives before the position', () {
    const ed = SkipSpan('ed', 1290, 1440);
    final viewing = watchedThrough(1290)
      ..seek(seconds(1290), runtime, span: ed);
    expect(viewing.ended(seconds(1290), runtime, [ed]), isTrue);
  });

  test('viewing evidence is isolated from a differently timed cut', () {
    final viewing = EpisodeViewing()..start(runtime, [1500000, 1439000]);
    expect(viewing.ended(seconds(1439), runtime, []), isFalse);
  });

  test('lookup combines a pair or falls back to the best partial source', () {
    const op = SkipSpan('op', 30, 90);
    const ed = SkipSpan('ed', 1290, 1437, source: 'theintrodb');
    const alternateOp = SkipSpan('op', 35, 95, source: 'aniskip');
    const recap = SkipSpan('recap', 100, 110, source: 'aniskip');
    expect(selectSkipSpans([op], [alternateOp, recap, ed], 1440), [
      op,
      recap,
      ed,
    ]);
    expect(selectSkipSpans([op], [alternateOp, recap], 1440), [
      alternateOp,
      recap,
    ]);
    expect(selectSkipSpans([], [ed], 1440), [ed]);
    expect(selectSkipSpans([], [], 1440), isEmpty);
  });

  testWidgets('stall waits for a full interval without progress', (
    tester,
  ) async {
    final watchdog = PlaybackStallWatchdog();
    addTearDown(watchdog.cancel);
    var stalls = 0;
    void onStall() => stalls++;

    watchdog.arm(onStall);
    await tester.pump(seconds(24));
    watchdog.arm(onStall);
    await tester.pump(seconds(24));
    expect(stalls, 0);
    await tester.pump(seconds(1));
    expect(stalls, 1);
    await tester.pump(seconds(25));
    expect(stalls, 1);

    watchdog.arm(onStall);
    watchdog.cancel();
    await tester.pump(seconds(25));
    expect(stalls, 1);
  });

  test(
    'a single provider sidecar appears without duplicating listed tracks',
    () {
      const url = 'https://subtitles.example/track';
      ResolvedStream stream(List<Json> subtitles) => ResolvedStream.fromJson({
        'url': 'https://video.example/index.m3u8',
        'subtitle': url,
        'subtitles': subtitles,
      });
      expect(externalSubtitleTracks(stream([])).single['url'], url);
      expect(
        externalSubtitleTracks(
          stream([
            {'url': url, 'language': 'en'},
          ]),
        ).single['language'],
        'en',
      );
    },
  );

  test(
    'subtitle bytes get a format hint and an HTML error page is rejected',
    () {
      final url = Uri.parse('https://subtitles.example/track?token=123');
      expect(subtitleExtension(utf8.encode('\uFEFFWEBVTT\n'), url), 'vtt');
      expect(subtitleExtension(utf8.encode('[Script Info]\n'), url), 'ass');
      expect(
        subtitleExtension(
          utf8.encode('1\n00:01:03,500 --> 00:01:04,999\nLine'),
          url,
        ),
        'srt',
      );
      expect(
        () => subtitleExtension(utf8.encode('<!DOCTYPE html>'), url),
        throwsFormatException,
      );
    },
  );
  final stream = ResolvedStream.fromJson({
    'url': 'http://127.0.0.1:17323/video.m3u8',
    'referrer': 'https://provider.example/watch/',
    'headers': {
      'User-Agent': 'Provider player',
      'Origin': 'https://provider.example',
    },
  });

  test(
    'protected sidecar preserves bytes and resolves against the provider',
    () async {
      final bytes = utf8.encode('WEBVTT\n\n00:00.000 --> 00:02.000\n日本語');
      final client = MockClient((request) async {
        expect(request.url, Uri.parse('https://provider.example/watch/track'));
        expect(request.headers['referer'], 'https://provider.example/watch/');
        expect(request.headers['user-agent'], 'Provider player');
        expect(request.headers['origin'], 'https://provider.example');
        return http.Response.bytes(bytes, 200);
      });
      final subtitle = await downloadSubtitle(client, stream, 'track');
      expect(subtitle.bytes, bytes);
      expect(subtitle.extension, 'vtt');
    },
  );

  test(
    'manual subtitle errors distinguish HTTP failures from invalid files',
    () async {
      final forbidden = MockClient(
        (_) async => http.Response('Forbidden', 403),
      );
      final html = MockClient(
        (_) async => http.Response('<html>Blocked</html>', 200),
      );
      final unreachable = MockClient(
        (_) async => throw http.ClientException('Offline'),
      );
      const link = '//subtitles.example/track';
      for (final (client, message) in [
        (forbidden, 'Subtitle download failed (HTTP 403).'),
        (html, 'The subtitle server returned an invalid file.'),
        (unreachable, 'The subtitle server could not be reached.'),
      ]) {
        await expectLater(
          downloadSubtitle(client, stream, link),
          throwsA(
            isA<SubtitleFailure>().having((e) => e.message, 'message', message),
          ),
        );
      }
    },
  );

  test('Android sidecar requests bypass the Dart HTTP client', () async {
    final bytes = utf8.encode('WEBVTT\n\n00:00.000 --> 00:02.000\n日本語');
    final core = _SubtitleCore(bytes);
    final client = MockClient((_) async => fail('Android used Dart HTTP'));
    final subtitle = await downloadSubtitle(
      client,
      stream,
      'https://subtitles.example/track',
      nativeCore: core,
    );
    expect(subtitle.bytes, bytes);
    expect(subtitle.extension, 'vtt');
    expect(core.input, {
      'operation': 'subtitle_download',
      'subtitle': {
        'url': 'https://subtitles.example/track',
        'headers': {
          'User-Agent': 'Provider player',
          'Origin': 'https://provider.example',
          'Referer': 'https://provider.example/watch/',
        },
      },
    });
  });

  test(
    'checks the active file instead of requiring the subtitle ID to change',
    () {
      final file = File('/cache/otakase-subtitle-x/track.ass');
      expect(isActiveSubtitleFile(file, file.path), isTrue);
      expect(isActiveSubtitleFile(file, file.uri.toString()), isTrue);
      expect(isActiveSubtitleFile(file, 'auto'), isFalse);
      expect(
        isActiveSubtitleFile(file, '/cache/otakase-subtitle-y/track.ass'),
        isFalse,
      );
    },
  );
}

class _SubtitleCore extends CoreClient {
  _SubtitleCore(this.bytes);

  final List<int> bytes;
  Json? input;

  @override
  Future<Json> request(
    Json input, {
    void Function(int)? onHandle,
    RequestScope? scope,
    Duration timeout = const Duration(seconds: 40),
  }) async {
    this.input = input;
    return {'body': base64Encode(bytes)};
  }
}
