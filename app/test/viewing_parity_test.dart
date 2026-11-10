import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/downloads.dart';
import 'package:otakase/data/episode_completion.dart';

void main() {
  test('a title followed from its start is watched up to the playhead', () {
    final viewing = EpisodeViewing()
      ..start(const Duration(minutes: 100), [100 * 60000, 0]);
    for (var s = 0; s <= 20; s++) {
      viewing.sample(Duration(seconds: s), playing: true);
    }
    expect(viewing.watchedUpTo(const Duration(seconds: 20)), isTrue);
  });

  test('a jump to the end is not watched', () {
    final viewing = EpisodeViewing()
      ..start(const Duration(minutes: 100), [100 * 60000, 0]);
    viewing.sample(Duration.zero, playing: true);
    viewing.seek(Duration.zero, const Duration(minutes: 95));
    viewing.sample(const Duration(minutes: 95), playing: true);
    expect(viewing.watchedUpTo(const Duration(minutes: 95)), isFalse);
    expect(
      viewing.ended(
        const Duration(minutes: 100),
        const Duration(minutes: 100),
        const [],
      ),
      isFalse,
    );
  });

  test('a resumed spot counts as watched up to it', () {
    final viewing = EpisodeViewing()
      ..start(const Duration(minutes: 100), [100 * 60000, 60 * 60000]);
    expect(viewing.watchedUpTo(const Duration(minutes: 60)), isTrue);
    expect(viewing.watchedUpTo(const Duration(minutes: 90)), isFalse);
  });

  test('a placeholder clip is far shorter than the episode', () {
    expect(copyLengthPlausible(const Duration(minutes: 1), 16 * 60), isFalse);
    expect(copyLengthPlausible(const Duration(minutes: 11), 16 * 60), isTrue);
    expect(copyLengthPlausible(const Duration(minutes: 1), 0), isTrue);
  });

  test('a download keeps what it is', () {
    final item = DownloadItem(
      id: 'anime:al:21:3:sub',
      kind: 'anime',
      title: 'One Piece',
      label: 'Episode 3',
      base: '/tmp/x',
      meta: {
        'anime': {'id': 21},
        'episode': 3,
      },
    );
    final back = DownloadItem.fromJson(item.toJson());
    expect(back.meta['episode'], 3);
    expect((back.meta['anime'] as Map)['id'], 21);
    final old = DownloadItem.fromJson({'id': 'x', 'kind': 'movie'});
    expect(old.meta, isEmpty);
  });
}
