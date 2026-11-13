import 'dart:async';

import '../data/models.dart';
import '../data/play_request.dart';
import '../data/synced_resume.dart';
import '../services.dart';
import '../ui/rate_prompt.dart';

/// Where an anime episode stands when its spot is saved.
class EpisodeSpot {
  const EpisodeSpot({
    required this.anime,
    required this.episode,
    required this.at,
    required this.length,
    this.inCredits = false,
    this.viewingKey,
    this.viewingCheckpoint,
  });

  final Anime anime;
  final int episode;
  final Duration at, length;

  /// The viewer is leaving during the credits of a copy that played
  /// properly, which counts the episode as watched.
  final bool inCredits;
  final String? viewingKey;
  final List<int>? viewingCheckpoint;
}

/// Keeps the player's watch progress: the spot in the history, the
/// watched episodes, the synced resume point on the trackers and Trakt's
/// scrobbles for films and series.
///
/// The player tells it where playback is; nothing here rebuilds the
/// player screen.
class PlayerProgress {
  PlayerProgress(this.services) : pusher = services.tracking.pusher();

  final AppServices services;

  /// Sends resume points and finished episodes to the signed-in trackers.
  final ResumePusher pusher;

  /// Episodes marked watched while this player was open.
  final completed = <int>{};

  /// A film or series episode was marked watched.
  bool markedWatched = false;

  /// Trakt was told the title is done.
  bool stopSent = false;

  Timer? _timer;

  /// Calls [tick] every [interval] until [cancel].
  void every(Duration interval, void Function() tick) {
    _timer?.cancel();
    _timer = Timer.periodic(interval, (_) => tick());
  }

  void cancel() {
    _timer?.cancel();
    _timer = null;
  }

  /// A new title starts: nothing of the last one's state carries over.
  void reset() {
    markedWatched = false;
    stopSent = false;
    completed.clear();
  }

  /// Saves a film or series episode through its request. Leaving before
  /// playback moved must not wipe the saved spot.
  Future<void> saveTitle(
    PlayRequest request,
    Duration at,
    Duration length, {
    bool completed = false,
  }) async {
    final done = completed || markedWatched;
    if (!done && at == Duration.zero) return;
    await request.onProgress?.call(at.inSeconds, length.inSeconds, done);
  }

  /// Saves an anime episode's spot. A quiet save keeps it in memory only.
  Future<void> saveEpisode(
    EpisodeSpot spot, {
    bool completed = false,
    bool quiet = false,
  }) async {
    if (!completed && spot.at == Duration.zero) return;
    final watched =
        completed || this.completed.contains(spot.episode) || spot.inCredits;
    final newlyWatched = watched && this.completed.add(spot.episode);
    final persist = services.store.progress(
      spot.anime,
      spot.episode,
      spot.at.inSeconds,
      spot.length.inSeconds,
      completed: watched,
      viewingKey: spot.viewingKey,
      viewingCheckpoint: spot.viewingCheckpoint,
      quiet: quiet && !watched,
    );
    if (newlyWatched) {
      if (finishesShow(services, spot.anime, spot.episode)) {
        services.ratingDue = spot.anime;
      }
      await pusher.episodeDone(
        spot.anime.key,
        spot.episode,
        spot.anime.episodes,
      );
    } else if (!watched) {
      pusher.note(spot.anime.key, spot.episode, spot.at.inSeconds);
    }
    await persist;
  }

  /// Writes the history to disk and tells the app.
  Future<void> flushHistory() => services.store.flushHistory();

  /// Sends pending resume points to the trackers.
  Future<void> flush() => pusher.flush();

  /// Tells Trakt a title started, paused or stopped.
  void scrobble(
    PlayRequest? request,
    String action,
    Duration at,
    double percent,
  ) {
    if (request == null) return;
    if (action == 'stop') return sendStop(request);
    if (stopSent || markedWatched) return;
    // Before the first frame the spot is not known yet; 0% would tell
    // Trakt the title was restarted.
    if (action == 'pause' && at == Duration.zero) return;
    request.onScrobble?.call(action, percent);
  }

  /// Tells Trakt the title is done, once.
  void sendStop(PlayRequest? request) {
    if (stopSent) return;
    stopSent = true;
    request?.onScrobble?.call('stop', 100);
  }
}
