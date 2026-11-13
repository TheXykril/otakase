import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/cast/cast.dart';
import 'package:otakase/core.dart';
import 'package:otakase/player/cast.dart';

ResolvedStream stream({
  String subtitle = '',
  List<Map<String, dynamic>> subtitles = const [],
}) => ResolvedStream.fromJson({
  'url': 'https://example.invalid/video.m3u8',
  'subtitle': subtitle,
  'subtitles': subtitles,
});

const english = {
  'url': 'https://example.invalid/en.vtt',
  'language': 'en',
  'label': 'English',
};
const lithuanian = {
  'url': 'https://example.invalid/lt.vtt',
  'language': 'lt',
  'label': 'Lietuvių',
};
const localFile = {
  'url': '/data/subs/en.ass',
  'language': 'en',
  'label': 'English',
};

void main() {
  group('cast subtitle', () {
    test('first preferred language wins', () {
      final s = stream(subtitles: [english, lithuanian]);
      expect(
        castSubtitleFor(s, isTitle: false, mode: 'sub', languages: 'lt,en'),
        lithuanian['url'],
      );
      expect(
        castSubtitleFor(s, isTitle: false, mode: 'sub', languages: 'off'),
        '',
      );
    });

    test('a dub and skipped subtitles send nothing', () {
      final s = stream(subtitles: [english]);
      expect(
        castSubtitleFor(s, isTitle: false, mode: 'dub', languages: 'en'),
        '',
      );
      expect(
        castSubtitleFor(
          s,
          isTitle: false,
          mode: 'sub',
          languages: 'en',
          skipEpisodeSubtitles: true,
        ),
        '',
      );
    });

    test('an episode falls back to any track, a title does not', () {
      final s = stream(subtitles: [english]);
      expect(
        castSubtitleFor(s, isTitle: false, mode: 'sub', languages: 'ja'),
        english['url'],
      );
      expect(
        castSubtitleFor(s, isTitle: true, mode: 'sub', languages: 'ja'),
        '',
      );
      expect(
        castSubtitleFor(
          stream(subtitle: 'https://example.invalid/one.vtt'),
          isTitle: false,
          mode: 'sub',
          languages: 'ja',
        ),
        'https://example.invalid/one.vtt',
      );
    });

    test('a file on this device goes along only beside a download', () {
      expect(
        castSubtitleFor(
          stream(),
          isTitle: true,
          mode: 'sub',
          languages: 'en',
          externalTrack: localFile,
        ),
        '',
      );
      expect(
        castSubtitleFor(
          stream(),
          isTitle: true,
          mode: 'sub',
          languages: 'en',
          externalTrack: localFile,
          castsFile: true,
        ),
        localFile['url'],
      );
      // A streamed title only sends web subtitle files.
      expect(
        castSubtitleFor(
          stream(subtitles: [localFile]),
          isTitle: true,
          mode: 'sub',
          languages: 'en',
        ),
        '',
      );
    });
  });

  test('a device takes over and each ended load counts once', () async {
    final cast = PlayerCast(CoreClient());
    expect(cast.active, isFalse);
    cast.begin(CastDevice.fromJson({'id': 'tv', 'name': 'TV'}));
    expect(cast.active, isTrue);
    expect(cast.paused, isFalse);
    final ended = CastStatus.fromJson({'state': 'ended', 'load': 2});
    expect(cast.endedNow(ended), isTrue);
    expect(cast.endedNow(ended), isFalse);
    expect(
      cast.endedNow(CastStatus.fromJson({'state': 'ended', 'load': 3})),
      isTrue,
    );
    expect(
      cast.endedNow(CastStatus.fromJson({'state': 'playing', 'load': 4})),
      isFalse,
    );
    await cast.stop(remote: true);
    expect(cast.active, isFalse);
    expect(cast.status, isNull);
  });
}
