import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
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
}
