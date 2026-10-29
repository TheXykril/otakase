import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/ui/home_page.dart';

WatchEntry entry({
  int episode = 8,
  int position = 600,
  int duration = 1440,
  List<int> watched = const [],
}) => WatchEntry(
  anime: const Anime(id: 1, title: 'Show'),
  episode: episode,
  position: position,
  duration: duration,
  watched: watched,
  updated: 0,
);

void main() {
  test('line shows how far into the episode in progress the viewer is', () {
    expect(episodeProgress(entry(), watched: 7), closeTo(600 / 1440, 1e-9));
  });
  test('no line without a half watched episode', () {
    expect(episodeProgress(null), isNull);
    expect(episodeProgress(entry(position: 0)), isNull);
    expect(episodeProgress(entry(duration: 0)), isNull);
    expect(episodeProgress(entry(watched: [8])), isNull);
    expect(episodeProgress(entry(), watched: 8), isNull);
  });
}
