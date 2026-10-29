import 'dart:math';

import 'models.dart';
import 'synced_resume.dart';
import 'tracker_merge.dart';

class UpcomingEpisode {
  const UpcomingEpisode(this.anime, this.episode, this.airingAt);
  final Anime anime;
  final int episode, airingAt;
  String caption(DateTime now) {
    final seconds = max<int>(0, airingAt - now.millisecondsSinceEpoch ~/ 1000);
    final hours = seconds ~/ 3600;
    final time = hours >= 24
        ? '${hours ~/ 24} d ${hours % 24} h'
        : hours > 0
        ? '$hours h ${seconds % 3600 ~/ 60} min'
        : '${max<int>(1, (seconds / 60).ceil())} min';
    return 'Ep $episode · in $time';
  }
}

class Continuation {
  const Continuation(this.continueWatching, this.upNext);
  final List<WatchEntry> continueWatching;
  final List<UpcomingEpisode> upNext;
}

Continuation buildContinuation(
  List<ListEntry> list,
  List<WatchEntry> history, {
  required bool adult,
  required DateTime now,
}) {
  final remaining = {for (final h in history) h.anime.key: h};
  final rows = <WatchEntry>[], upNext = <UpcomingEpisode>[];
  void add(
    Anime anime,
    int progress,
    String notes,
    int updated,
    WatchEntry? local,
  ) {
    if (!adult && anime.isAdult ||
        anime.format == 'MUSIC' ||
        anime.status == 'NOT_YET_RELEASED') {
      return;
    }
    final next = progress + 1;
    final aired = anime.nextEpisode > 1
        ? anime.nextEpisode - 1
        : anime.episodes;
    if (aired > 0 && next > aired) {
      if (anime.status == 'RELEASING' &&
          anime.nextEpisode > 1 &&
          anime.nextAiringAt > now.millisecondsSinceEpoch ~/ 1000) {
        upNext.add(
          UpcomingEpisode(anime, anime.nextEpisode, anime.nextAiringAt),
        );
      }
      return;
    }
    final tag = parseSyncedResumeTag(notes);
    final remote = tag?.episode == next && (tag?.seconds ?? 0) >= 10
        ? tag!.seconds
        : 0;
    final seconds = max<int>(
      remote,
      local?.episode == next ? local!.position : 0,
    );
    rows.add(
      WatchEntry(
        anime: anime,
        episode: next,
        position: seconds,
        duration: local?.episode == next ? local!.duration : 0,
        watched: local?.watched ?? const [],
        updated: updated,
      ),
    );
  }

  for (final raw in list) {
    final e = normalizeTrackerEntry(raw), a = e.anime;
    final local = history
        .where(
          (h) =>
              h.anime.key == a.key || a.malId > 0 && h.anime.malId == a.malId,
        )
        .firstOrNull;
    if (local != null) remaining.remove(local.anime.key);
    if (e.status != 'CURRENT' && e.status != 'REPEATING') continue;
    final localProgress = local?.watched.fold<int>(0, max) ?? 0;
    final remoteAt = e.updatedAt?.millisecondsSinceEpoch ?? 0;
    final progress = local != null && local.updated > remoteAt
        ? max<int>(e.progress, localProgress)
        : e.progress;
    add(
      mergeAnimeMetadata(a, local?.anime ?? a),
      progress,
      e.notes,
      max<int>(remoteAt, local?.updated ?? 0),
      local,
    );
  }
  for (final h in remaining.values) {
    if (h.position <= 0 && h.watched.isEmpty) continue;
    final progress = h.watched.fold<int>(0, max);
    final next = h.position > 0 && !h.watched.contains(h.episode)
        ? h.episode
        : progress + 1;
    add(h.anime, next - 1, '', h.updated, h);
  }
  rows.sort((a, b) => b.updated.compareTo(a.updated));
  upNext.sort((a, b) => a.airingAt.compareTo(b.airingAt));
  return Continuation(rows, upNext);
}
