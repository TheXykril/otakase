import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/continuation.dart';
import 'package:otakase/data/episode_alerts.dart';
import 'package:otakase/data/models.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  test('computers say once that a followed episode is out', () async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    var now = DateTime.utc(2026, 11, 12, 12);
    final alerts = EpisodeAlerts(prefs, now: () => now);
    addTearDown(alerts.dispose);
    final said = <String>[];
    alerts.show = said.add;
    final at = now.millisecondsSinceEpoch ~/ 1000 + 3600;
    final upNext = [
      UpcomingEpisode(Anime(id: 5, title: 'Show'), 4, at),
      // Only AniList shows are followed.
      UpcomingEpisode(Anime(id: 0, malId: 9, title: 'Other'), 2, at),
    ];
    alerts.watch(upNext, on: true);
    expect(said, isEmpty);
    now = now.add(const Duration(hours: 2));
    alerts.check();
    expect(said, ['Episode 4 of Show is out']);
    alerts.check();
    // A restart remembers it was said.
    final again = EpisodeAlerts(prefs, now: () => now)..show = said.add;
    addTearDown(again.dispose);
    again.watch(upNext, on: true);
    expect(said, hasLength(1));
    // Off stops it.
    alerts.watch([UpcomingEpisode(Anime(id: 6, title: 'New'), 1, at)], on: false);
    alerts.check();
    expect(said, hasLength(1));
  });
}
