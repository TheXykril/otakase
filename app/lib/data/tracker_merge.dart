import 'dart:math';

import 'models.dart';

String anilistStatus(String status) =>
    const {
      'watching': 'CURRENT',
      'plan_to_watch': 'PLANNING',
      'completed': 'COMPLETED',
      'on_hold': 'PAUSED',
      'dropped': 'DROPPED',
    }[status] ??
    status;
ListEntry normalizeTrackerEntry(ListEntry e) =>
    e.copyWith(status: e.isRepeating ? 'REPEATING' : anilistStatus(e.status));

int entryFreshnessScore(ListEntry raw) {
  final e = normalizeTrackerEntry(raw);
  return 100 * e.progress +
      (const {
            'REPEATING': 60,
            'CURRENT': 50,
            'COMPLETED': 40,
            'PAUSED': 30,
            'PLANNING': 20,
            'DROPPED': 10,
          }[e.status] ??
          0) +
      max(e.score, 0).truncate() +
      max<int>(e.repeat, 0) +
      (e.anime.cover.isNotEmpty ? 1 : 0);
}

bool incomingIsNewer(ListEntry existing, ListEntry incoming) {
  final before = existing.updatedAt, after = incoming.updatedAt;
  if (before == null || after == null) {
    if ((before == null) != (after == null)) return before == null;
  } else if (!before.isAtSameMomentAs(after)) {
    return after.isAfter(before);
  }
  return entryFreshnessScore(incoming) > entryFreshnessScore(existing);
}

Anime mergeAnimeMetadata(Anime a, Anime b) => Anime(
  id: a.id == 0 ? b.id : a.id,
  malId: a.malId == 0 ? b.malId : a.malId,
  title: a.title.isEmpty || a.title == 'Untitled' ? b.title : a.title,
  nativeTitle: a.nativeTitle.isEmpty ? b.nativeTitle : a.nativeTitle,
  titles: a.titles.isEmpty ? b.titles : a.titles,
  cover: a.cover.isEmpty ? b.cover : a.cover,
  coverLarge: a.coverLarge.isEmpty ? b.coverLarge : a.coverLarge,
  banner: a.banner.isEmpty ? b.banner : a.banner,
  description: a.description.isEmpty ? b.description : a.description,
  format: a.format.isEmpty ? b.format : a.format,
  status: a.status.isEmpty ? b.status : a.status,
  year: a.year == 0 ? b.year : a.year,
  episodes: a.episodes == 0 ? b.episodes : a.episodes,
  duration: a.duration == 0 ? b.duration : a.duration,
  score: a.score == 0 ? b.score : a.score,
  isAdult: a.isAdult || b.isAdult,
  nextEpisode: a.nextEpisode == 0 ? b.nextEpisode : a.nextEpisode,
  nextAiringAt: a.nextEpisode == 0 ? b.nextAiringAt : a.nextAiringAt,
  genres: a.genres.isEmpty ? b.genres : a.genres,
  studio: a.studio.isEmpty ? b.studio : a.studio,
  episodeArt: a.episodeArt.isEmpty ? b.episodeArt : a.episodeArt,
  related: a.related.isEmpty ? b.related : a.related,
  recommended: a.recommended.isEmpty ? b.recommended : a.recommended,
);

ListEntry mergeEntryMetadata(ListEntry preferred, ListEntry fallback) =>
    preferred.copyWith(
      anime: mergeAnimeMetadata(preferred.anime, fallback.anime),
      repeat: preferred.repeat == 0
          ? max(fallback.repeat, 0)
          : preferred.repeat,
    );
ListEntry mergeAnimeEntries(ListEntry existing, ListEntry incoming) {
  final a = normalizeTrackerEntry(existing),
      b = normalizeTrackerEntry(incoming);
  return incomingIsNewer(a, b)
      ? mergeEntryMetadata(b, a)
      : mergeEntryMetadata(a, b);
}

List<ListEntry> mergeTrackerLists(
  List<ListEntry> anilist,
  List<ListEntry> mal,
) {
  final remaining = {for (final e in mal) e.anime.malId: e};
  return [
    for (final a in anilist)
      if (a.anime.malId > 0 && remaining.containsKey(a.anime.malId))
        mergeAnimeEntries(
          a,
          remaining.remove(a.anime.malId)!,
        ).copyWith(isPrivate: a.isPrivate)
      else
        normalizeTrackerEntry(a),
    ...remaining.values.map(normalizeTrackerEntry),
  ];
}

bool entriesEquivalentForSync(ListEntry current, ListEntry desired) {
  final have = mergeEntryMetadata(
    normalizeTrackerEntry(current),
    normalizeTrackerEntry(desired),
  );
  final want = mergeEntryMetadata(normalizeTrackerEntry(desired), have);
  return have.anime.id == want.anime.id &&
      have.anime.malId == want.anime.malId &&
      have.progress == want.progress &&
      have.repeat == want.repeat &&
      have.score == want.score &&
      have.status == want.status &&
      have.startedAt == want.startedAt &&
      have.completedAt == want.completedAt;
}

class TrackerSyncWrite {
  const TrackerSyncWrite(
    this.service,
    this.entry, {
    this.part = SyncWritePart.state,
  });
  final String service;
  final ListEntry entry;
  final SyncWritePart part;
  Json toJson() => {
    'service': service,
    'entry': entry.toJson(),
    'part': part.name,
  };
  factory TrackerSyncWrite.fromJson(Json j) => TrackerSyncWrite(
    j['service'] as String,
    ListEntry.fromJson((j['entry'] as Map).cast<String, dynamic>()),
    part: SyncWritePart.values.byName(j['part'] as String),
  );
}

enum SyncWritePart { state, completion, repeat }

List<TrackerSyncWrite> planTrackerSync(
  List<ListEntry> anilist,
  List<ListEntry> mal, {
  required bool adult,
}) {
  final aByMal = {
    for (final a in anilist)
      if (a.anime.malId > 0) a.anime.malId: a,
  };
  final mByMal = {
    for (final m in mal)
      if (m.anime.malId > 0) m.anime.malId: m,
  };
  final keys = {...aByMal.keys, ...mByMal.keys}.toList()..sort();
  final writes = <TrackerSyncWrite>[];
  void queue(String service, ListEntry entry) {
    if (service == 'MAL' && entry.status == 'COMPLETED' && entry.repeat > 0) {
      writes.add(
        TrackerSyncWrite(service, entry, part: SyncWritePart.completion),
      );
      writes.add(TrackerSyncWrite(service, entry, part: SyncWritePart.repeat));
    } else {
      writes.add(TrackerSyncWrite(service, entry));
    }
  }

  for (final key in keys) {
    final a = aByMal[key], m = mByMal[key];
    final winner = a == null
        ? normalizeTrackerEntry(m!)
        : m == null
        ? normalizeTrackerEntry(a)
        : mergeAnimeEntries(a, m).copyWith(isPrivate: a.isPrivate);
    if (!adult && winner.anime.isAdult) continue;
    if ((a == null || !entriesEquivalentForSync(a, winner)) &&
        winner.anime.id > 0) {
      queue('AniList', winner);
    }
    if (!(a?.isPrivate ?? false) &&
        (m == null || !entriesEquivalentForSync(m, winner))) {
      queue('MAL', winner);
    }
  }
  return [
    ...writes.where((w) => w.service == 'AniList'),
    ...writes.where((w) => w.service == 'MAL'),
  ];
}
