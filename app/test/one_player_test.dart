import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/play_request.dart';

void main() {
  test('a found copy becomes a source the player handles like anime', () {
    final source = mediaSource(
      const PlayableMedia(
        url: 'https://cdn.example/film.m3u8',
        headers: {'Referer': 'https://site.example/'},
        copy: 'vidsrc:server 1',
        subtitles: [
          {'url': 'https://cdn.example/en.vtt', 'language': 'en'},
          {'path': '/downloads/film.lt.srt', 'language': 'lt'},
        ],
      ),
    );
    expect(source.provider, 'vidsrc:server 1');
    expect(source.streams, hasLength(1));
    final stream = source.streams.single;
    expect(stream.hls, isTrue);
    expect(stream.headers['Referer'], 'https://site.example/');
    expect(stream.subtitles.map((t) => t['url']), [
      'https://cdn.example/en.vtt',
      '/downloads/film.lt.srt',
    ]);
  });

  test('a download keeps its plain file path', () {
    final source = mediaSource(const PlayableMedia(url: '/downloads/film.mp4'));
    expect(source.provider, isEmpty);
    expect(source.streams.single.url, '/downloads/film.mp4');
    expect(source.streams.single.hls, isFalse);
  });

  test('track languages match by name, code or label', () {
    expect(languageMatches('en-US', null, 'english'), isTrue);
    expect(languageMatches('lit', null, 'lithuanian'), isTrue);
    expect(languageMatches(null, 'Lietuvių', 'lithuanian'), isTrue);
    expect(languageMatches('zh_Hans', null, 'chinese'), isTrue);
    expect(languageMatches('ja', null, 'english'), isFalse);
  });
}
