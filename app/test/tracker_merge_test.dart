import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/tracking.dart';

const ani = Anime(id: 42, malId: 24, title: 'AniList title');
const mal = Anime(id: 0, malId: 24, title: 'Different MAL title');

void main() {
  test(
    'matches idMal, keeps AniList metadata and uses higher MAL progress',
    () {
      final rows = mergeTrackerLists(
        [const ListEntry(ani, 'PLANNING', 3, score: 90, notes: 'keep')],
        [const ListEntry(mal, 'on_hold', 8, score: 70)],
      );
      expect(rows, hasLength(1));
      final entry = rows.single;
      expect(entry.anime, same(ani));
      expect(entry.progress, 8);
      expect(entry.status, 'PAUSED');
      expect(entry.score, 90);
      expect(entry.notes, 'keep');
    },
  );

  test('AniList supplies status when ahead or tied on progress', () {
    for (final progress in [7, 8]) {
      final entry = mergeTrackerLists(
        [const ListEntry(ani, 'DROPPED', 8)],
        [ListEntry(mal, 'watching', progress)],
      ).single;
      expect(entry.progress, 8);
      expect(entry.status, 'DROPPED');
    }
  });

  test('score prefers a set AniList score independently of progress', () {
    for (final scores in [
      (0.0, 80.0, 80.0),
      (0.0, 0.0, 0.0),
      (50.0, 0.0, 50.0),
    ]) {
      final entry = mergeTrackerLists(
        [ListEntry(ani, 'CURRENT', 8, score: scores.$1)],
        [ListEntry(mal, 'completed', 12, score: scores.$2)],
      ).single;
      expect(entry.score, scores.$3);
    }
  });

  test('unmatched entries survive without matching by title or AniList id', () {
    final rows = mergeTrackerLists(
      [
        const ListEntry(ani, 'CURRENT', 2),
        const ListEntry(Anime(id: 99, title: 'Same title'), 'PLANNING', 0),
      ],
      [
        const ListEntry(mal, 'watching', 4),
        const ListEntry(
          Anime(id: 0, malId: 99, title: 'Same title'),
          'plan_to_watch',
          0,
        ),
        const ListEntry(
          Anime(id: 0, malId: 42, title: 'AniList title'),
          'dropped',
          3,
        ),
      ],
    );
    expect(rows.map((e) => e.anime.key), [
      'al:42',
      'al:99',
      'mal:99',
      'mal:42',
    ]);
    expect(rows.map((e) => e.status), [
      'CURRENT',
      'PLANNING',
      'PLANNING',
      'DROPPED',
    ]);
  });

  test('MAL-only categories use AniList names including rewatching', () {
    const statuses = {
      'watching': 'CURRENT',
      'plan_to_watch': 'PLANNING',
      'completed': 'COMPLETED',
      'on_hold': 'PAUSED',
      'dropped': 'DROPPED',
    };
    for (final s in statuses.entries) {
      final entry = mergeTrackerLists([], [ListEntry(mal, s.key, 3)]).single;
      expect(entry.status, s.value);
    }
    final entry = mergeTrackerLists(
      [const ListEntry(ani, 'COMPLETED', 12)],
      [const ListEntry(mal, 'watching', 13, isRepeating: true)],
    ).single;
    expect(entry.status, 'REPEATING');
    expect(entry.progress, 13);
    expect(
      mergeTrackerLists([], [
        const ListEntry(mal, 'watching', 2, isRepeating: true),
      ]).single.status,
      'REPEATING',
    );
  });
}
