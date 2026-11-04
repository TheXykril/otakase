import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/episode_completion.dart';
import 'package:otakase/data/external_subtitles.dart';
import 'package:otakase/data/models.dart';

Duration seconds(int value) => Duration(seconds: value);

void main() {
  test('EOF must be within the final seconds of the known duration', () {
    expect(reachedEpisodeEnd(seconds(600), seconds(1440)), isFalse);
    expect(reachedEpisodeEnd(seconds(1410), seconds(1440)), isFalse);
    expect(reachedEpisodeEnd(seconds(1437), seconds(1440)), isTrue);
    expect(reachedEpisodeEnd(seconds(20), Duration.zero), isFalse);
    expect(reachedEpisodeEnd(seconds(6), seconds(10)), isFalse);
  });

  test('terminal credits count, credits followed by a scene do not', () {
    const credits = [SkipSpan('ed', 1290, 1440)];
    expect(watchedEpisode(seconds(1290), seconds(1440), 95, credits), isTrue);
    expect(watchedEpisode(seconds(1289), seconds(1440), 95, credits), isFalse);
    expect(watchedEpisode(seconds(1290), seconds(1440), 100, credits), isFalse);
    expect(
      watchedEpisode(seconds(1290), seconds(1440), 95, [
        const SkipSpan('ed', 1290, 1400),
      ]),
      isFalse,
    );
    expect(watchedEpisode(seconds(1224), seconds(1440), 85, []), isTrue);
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
