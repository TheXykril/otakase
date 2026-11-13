import '../core.dart';
import '../data/models.dart';
import '../data/play_request.dart';
import '../data/skip_times.dart';
import '../services.dart';

/// What the player does about the skip span playback is in.
enum SkipCue {
  /// Nothing to do.
  none,

  /// The viewer turned automatic skipping on and the delay passed: jump.
  skip,

  /// Playback just entered a span that isn't skipped by itself: offer the
  /// skip button.
  offer,
}

/// The skip times of the playing episode or title: openings, endings and
/// recaps, the span playback is in now, and which spans were skipped or
/// watched on purpose so they are not skipped again.
///
/// The player tells it where playback is and acts on the cue it returns.
class PlayerSkips {
  PlayerSkips(this.services);

  final AppServices services;

  List<SkipSpan> spans = [];

  /// The span playback is in, or null.
  SkipSpan? active;

  final _kept = <String>{};

  static String _key(SkipSpan s) => '${s.type}:${s.start}';

  /// A new episode or copy starts.
  void reset() {
    spans = [];
    active = null;
    _kept.clear();
  }

  /// [span] was skipped or is being watched on purpose: automatic
  /// skipping leaves it alone from now on.
  void keep(SkipSpan span) => _kept.add(_key(span));

  /// Every span is left alone, as while the viewer marks a new one.
  void keepAll() => spans.forEach(keep);

  bool kept(SkipSpan span) => _kept.contains(_key(span));

  /// The setting that turns automatic skipping on for [type].
  static String setting(String type) => switch (type) {
    'op' => 'SkipOp',
    'ed' => 'SkipEd',
    _ => 'SkipRecap',
  };

  /// Playback is at [seconds]. Returns whether [active] changed and what
  /// to do about it.
  ({bool changed, SkipCue cue}) at(double seconds) {
    final current = spans.where((s) => s.contains(seconds)).firstOrNull;
    final entered =
        active?.type != current?.type || active?.start != current?.start;
    final changed = !identical(active, current);
    active = current;
    if (current == null) return (changed: changed, cue: SkipCue.none);
    final auto = services.store.flag(setting(current.type));
    if (auto &&
        !kept(current) &&
        seconds >= current.start + services.store.integer('SkipDelay')) {
      keep(current);
      return (changed: changed, cue: SkipCue.skip);
    }
    return (
      changed: changed,
      cue: entered && !auto ? SkipCue.offer : SkipCue.none,
    );
  }

  /// Series episodes get their spans from the request (TheIntroDB); films
  /// have none. Null when there is nothing to look up.
  Future<List<SkipSpan>?> forTitle(
    PlayRequest request,
    double length,
    RequestScope scope,
  ) async {
    final find = request.skips;
    if (find == null || length <= 0) return null;
    return selectSkipSpans(const [], await find(length, scope), length);
  }

  /// Anime spans come from the source first; the skip time services fill
  /// in when the source lacks an opening and ending pair. [length] is read
  /// after the source answers, since the copy's length may arrive late.
  /// [stillWanted] says whether the episode is still the one playing.
  Future<List<SkipSpan>?> forEpisode(
    PlaybackSource source,
    Anime anime,
    int episode,
    RequestScope scope, {
    required double Function() length,
    required bool Function() stillWanted,
  }) async {
    final result = await services.resolver.skips(source, episode, scope);
    if (!stillWanted()) return null;
    final seconds = length();
    if (seconds <= 0) return null;
    final provider = selectSkipSpans(result, const [], seconds);
    if (provider.any((s) => s.type == 'op') &&
        provider.any((s) => s.type == 'ed')) {
      return provider;
    }
    final extra = await services.skipTimes.lookup(
      anime,
      episode,
      seconds,
      scope: scope,
    );
    return selectSkipSpans(result, extra, seconds);
  }

  /// Sends a marked span to AniSkip and shows it at once. Returns words
  /// for the viewer when it failed, null when it was sent.
  Future<String?> submit(
    Anime anime,
    int episode,
    String type,
    double start,
    double end,
    double length,
  ) async {
    try {
      final id = await services.skipTimes.submit(
        anime,
        episode,
        type,
        start,
        end,
        length,
      );
      spans = [
        ...spans.where((s) => s.type != type),
        SkipSpan(type, start, end, source: 'aniskip', id: id),
      ]..sort((a, b) => a.start.compareTo(b.start));
      return null;
    } on CoreFailure catch (e) {
      return e.message;
    } catch (_) {
      return 'AniSkip could not be reached. Try again later.';
    }
  }

  /// Votes on the span playing now, else the episode's first AniSkip
  /// time. Returns words for the viewer.
  Future<String> vote({required bool up}) async {
    final span =
        (active != null && active!.id.isNotEmpty ? active : null) ??
        spans.where((s) => s.id.isNotEmpty).firstOrNull;
    if (span == null) return 'No AniSkip time to vote on';
    if (services.skipTimes.voted(span)) return 'You already voted on this one';
    try {
      await services.skipTimes.vote(span, up: up);
      return 'Thanks, your vote is in';
    } catch (_) {
      return 'AniSkip could not be reached';
    }
  }
}
