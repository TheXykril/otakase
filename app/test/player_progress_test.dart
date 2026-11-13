import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/play_request.dart';
import 'package:otakase/player/progress.dart';

import 'widget_test.dart' as fixture;

const show = Anime(id: 7, title: 'Show', episodes: 12, duration: 24);
const length = Duration(minutes: 24);

EpisodeSpot spot(int episode, int seconds, {bool inCredits = false}) =>
    EpisodeSpot(
      anime: show,
      episode: episode,
      at: Duration(seconds: seconds),
      length: length,
      inCredits: inCredits,
    );

PlayRequest film({List<String>? scrobbles, List<(int, int, bool)>? saves}) =>
    PlayRequest(
      title: 'Film',
      subtitle: '',
      open: (_, _, _) => throw UnimplementedError(),
      onScrobble: (action, percent) => scrobbles?.add(action),
      onProgress: (at, total, done) async => saves?.add((at, total, done)),
    );

void main() {
  test('an episode spot lands in the history, not before it moved', () async {
    final services = await fixture.services();
    final progress = PlayerProgress(services);
    await progress.saveEpisode(spot(3, 0));
    expect(services.store.entry(show), isNull);
    await progress.saveEpisode(spot(3, 300), quiet: true);
    final entry = services.store.entry(show)!;
    expect(entry.episode, 3);
    expect(entry.position, 300);
    expect(entry.watched, isEmpty);
  });

  test('credits on leaving and a completed save mark the episode', () async {
    final services = await fixture.services();
    final progress = PlayerProgress(services);
    await progress.saveEpisode(spot(2, 1400, inCredits: true));
    expect(services.store.entry(show)!.watched, [2]);
    await progress.saveEpisode(spot(3, 0), completed: true);
    expect(services.store.entry(show)!.watched, containsAll([2, 3]));
    expect(progress.completed, {2, 3});
    // Seeking back in a watched episode keeps it watched.
    await progress.saveEpisode(spot(3, 60));
    expect(services.store.entry(show)!.watched, contains(3));
    progress.reset();
    expect(progress.completed, isEmpty);
  });

  test('a title saves through its request and keeps an unmoved spot', () async {
    final services = await fixture.services();
    final progress = PlayerProgress(services);
    final saves = <(int, int, bool)>[];
    final request = film(saves: saves);
    await progress.saveTitle(request, Duration.zero, length);
    expect(saves, isEmpty);
    await progress.saveTitle(request, const Duration(seconds: 90), length);
    progress.markedWatched = true;
    await progress.saveTitle(request, Duration.zero, length);
    expect(saves, [(90, 1440, false), (0, 1440, true)]);
  });

  test('Trakt hears start and pause, and stop only once', () async {
    final services = await fixture.services();
    final progress = PlayerProgress(services);
    final scrobbles = <String>[];
    final request = film(scrobbles: scrobbles);
    progress.scrobble(request, 'pause', Duration.zero, 0);
    progress.scrobble(request, 'start', Duration.zero, 0);
    progress.scrobble(request, 'pause', const Duration(seconds: 30), 2);
    progress.scrobble(request, 'stop', const Duration(seconds: 30), 2);
    progress.sendStop(request);
    progress.scrobble(request, 'start', const Duration(seconds: 30), 2);
    expect(scrobbles, ['start', 'pause', 'stop']);
    progress.reset();
    progress.markedWatched = true;
    progress.scrobble(request, 'start', const Duration(seconds: 30), 2);
    expect(scrobbles, ['start', 'pause', 'stop']);
  });
}
