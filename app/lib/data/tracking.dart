import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';

import 'auth.dart';
import 'catalog.dart';
import 'models.dart';
import 'synced_resume.dart';
import 'tracker_merge.dart';
import 'store.dart';

export 'tracker_merge.dart';

const anilistStatuses = {
  'ALL': 'All titles',
  'CURRENT': 'Watching',
  'PLANNING': 'Planning',
  'COMPLETED': 'Completed',
  'PAUSED': 'Paused',
  'DROPPED': 'Dropped',
  'REPEATING': 'Repeating',
};
const malStatuses = {
  'ALL': 'All titles',
  'watching': 'Watching',
  'plan_to_watch': 'Planning',
  'completed': 'Completed',
  'on_hold': 'On hold',
  'dropped': 'Dropped',
};
String malStatus(String status) =>
    const {
      'CURRENT': 'watching',
      'PLANNING': 'plan_to_watch',
      'COMPLETED': 'completed',
      'PAUSED': 'on_hold',
      'DROPPED': 'dropped',
      'REPEATING': 'watching',
    }[status] ??
    status;

class CombinedAccountPage {
  const CombinedAccountPage(
    this.entries, {
    this.notLoaded = const [],
    this.anilist = const [],
    this.mal = const [],
  });
  final List<ListEntry> entries;
  final List<String> notLoaded;
  final List<ListEntry> anilist, mal;
}

class TrackingService extends ChangeNotifier {
  void Function()? onProgressWritten;
  TrackingService(this.auth, {DateTime Function()? now, this.store})
    : now = now ?? DateTime.now;
  final DateTime Function() now;
  final AppStore? store;
  final AuthService auth;
  String? lastError;
  bool closed = false;
  @override
  void notifyListeners() {
    if (!closed) super.notifyListeners();
  }

  @override
  void dispose() {
    closed = true;
    super.dispose();
  }

  int revision = 0;
  final Map<String, Future<void>> _writes = {};
  final Map<String, Anime> _shows = {};
  final Map<String, ListEntry?> _entries = {};
  String _entryKey(String service, Anime anime) =>
      service == 'MAL' ? 'MAL:mal:${anime.malId}' : 'AniList:${anime.key}';
  ListEntry? cached(String service, Anime anime) =>
      _entries[_entryKey(service, anime)];

  Future<CombinedAccountPage> combinedList({required bool adult}) async {
    final pages = <String, AccountPage>{};
    await Future.wait(
      ['AniList', 'MAL'].map((service) async {
        try {
          pages[service] = await list(
            service,
            adult: adult,
            filterAdult: false,
          );
        } catch (_) {
          // Each tracker can still supply the list when the other is down.
        }
      }),
    );
    return CombinedAccountPage(
      mergeTrackerLists(
        pages['AniList']?.entries ?? [],
        pages['MAL']?.entries ?? [],
      ).where((e) => adult || !e.anime.isAdult).toList(),
      anilist: pages['AniList']?.entries ?? [],
      mal: pages['MAL']?.entries ?? [],
      notLoaded: [
        for (final service in ['AniList', 'MAL'])
          if (!pages.containsKey(service) || pages[service]!.fromCache) service,
      ],
    );
  }

  Future<AccountPage> list(
    String service, {
    required bool adult,
    bool filterAdult = true,
  }) async {
    try {
      return await _list(service, adult: adult, filterAdult: filterAdult);
    } catch (_) {
      final cached = cachedList(
        service,
        adult: adult,
        filterAdult: filterAdult,
      );
      if (cached != null) return cached;
      rethrow;
    }
  }

  String _identity(String service) =>
      '${auth.account(service)?.userId}:${auth.account(service)?.username}';
  AccountPage? cachedList(
    String service, {
    required bool adult,
    bool filterAdult = true,
  }) {
    final data = store?.value('ListCache.$service');
    if (!auth.signedIn(service) ||
        data is! Map ||
        data['identity'] != _identity(service)) {
      return null;
    }
    try {
      final rows = (data['entries'] as List)
          .map((e) => ListEntry.fromJson((e as Map).cast<String, dynamic>()))
          .where((e) => !filterAdult || adult || !e.anime.isAdult)
          .toList();
      for (final e in rows) {
        _entries[_entryKey(service, e.anime)] = e;
      }
      return AccountPage(
        rows,
        avatar: auth.account(service)?.avatar ?? '',
        fromCache: true,
      );
    } catch (_) {
      return null;
    }
  }

  Future<AccountPage> _list(
    String service, {
    required bool adult,
    required bool filterAdult,
  }) async {
    final owner = _identity(service);
    if (!auth.signedIn(service) || closed) {
      throw const LoginFailure('Account signed out.');
    }
    final rows = <String, ListEntry>{};
    if (service == 'AniList') {
      final account = auth.account(service)!;
      final d = await auth.graph(
        '''query(\$id:Int){MediaListCollection(userId:\$id,type:ANIME){lists{entries{status progress notes updatedAt private repeat startedAt{year month day} completedAt{year month day} score(format:POINT_100) media{$mediaFields}}}}}''',
        {'id': account.userId},
      );
      for (final group
          in (d['MediaListCollection'] as Map?)?['lists'] as List? ?? []) {
        for (final e in (group as Map)['entries'] as List? ?? []) {
          final a = Anime.fromJson((e['media'] as Map).cast<String, dynamic>());
          if (filterAdult && !adult && a.isAdult) continue;
          final entry = ListEntry(
            a,
            e['status'] as String,
            number(e['progress']),
            notes: e['notes'] as String? ?? '',
            score: (e['score'] as num? ?? 0).toDouble(),
            updatedAt: number(e['updatedAt']) > 0
                ? DateTime.fromMillisecondsSinceEpoch(
                    number(e['updatedAt']) * 1000,
                    isUtc: true,
                  )
                : null,
            isPrivate: e['private'] == true,
            repeat: number(e['repeat']),
            startedAt: FuzzyDate.fromJson(e['startedAt']),
            completedAt: FuzzyDate.fromJson(e['completedAt']),
          );
          rows[a.key] = entry;
        }
      }
    } else {
      for (var offset = 0; ; offset += 100) {
        final d = await auth.mal('GET', '/users/@me/animelist', {
          'limit': '100',
          'offset': '$offset',
          'sort': 'list_updated_at',
          'nsfw': '$adult',
          'fields': 'list_status{comments,score,is_rewatching,updated_at,num_times_rewatched,start_date,finish_date},num_episodes,media_type,mean,average_episode_duration,start_season,nsfw,rating,alternative_titles,synopsis,status,genres',
        });
        for (final e in d['data'] as List? ?? []) {
          final a = Anime.fromMal((e['node'] as Map).cast<String, dynamic>());
          if (filterAdult && !adult && a.isAdult) continue;
          final s = (e['list_status'] as Map?) ?? {};
          final entry = ListEntry(
            a,
            s['status'] as String? ?? 'watching',
            number(s['num_episodes_watched']),
            notes: s['comments'] as String? ?? '',
            score: number(s['score']) * 10.0,
            isRepeating: s['is_rewatching'] == true,
            updatedAt: DateTime.tryParse(s['updated_at'] as String? ?? ''),
            repeat: number(s['num_times_rewatched']),
            startedAt: FuzzyDate.parse(s['start_date'] as String?),
            completedAt: FuzzyDate.parse(s['finish_date'] as String?),
          );
          rows[a.key] = entry;
        }
        if ((d['paging'] as Map?)?['next'] == null) break;
      }
    }
    if (closed || owner != _identity(service)) {
      throw const LoginFailure('Account changed.');
    }
    for (final e in rows.values) {
      _entries[_entryKey(service, e.anime)] = e;
    }
    await store?.set('ListCache.$service', {
      'identity': owner,
      'entries': rows.values.map((e) => e.toJson()).toList(),
    });
    return AccountPage(
      rows.values.toList(),
      avatar: auth.account(service)!.avatar,
    );
  }

  Future<ListEntry?> entry(String service, Anime anime) async {
    ListEntry? result;
    if (service == 'AniList' && anime.id > 0) {
      final d = await auth.graph(
        '''query(\$id:Int){Media(id:\$id,type:ANIME){mediaListEntry{status progress notes updatedAt private repeat startedAt{year month day} completedAt{year month day} score(format:POINT_100)}}}''',
        {'id': anime.id},
      );
      final e = (d['Media'] as Map?)?['mediaListEntry'] as Map?;
      if (e != null) {
        result = ListEntry(
          anime,
          e['status'] as String,
          number(e['progress']),
          notes: e['notes'] as String? ?? '',
          score: (e['score'] as num? ?? 0).toDouble(),
          updatedAt: number(e['updatedAt']) > 0
              ? DateTime.fromMillisecondsSinceEpoch(
                  number(e['updatedAt']) * 1000,
                  isUtc: true,
                )
              : null,
          isPrivate: e['private'] == true,
          repeat: number(e['repeat']),
          startedAt: FuzzyDate.fromJson(e['startedAt']),
          completedAt: FuzzyDate.fromJson(e['completedAt']),
        );
      }
    } else if (service == 'MAL' && anime.malId > 0) {
      final d = await auth.mal('GET', '/anime/${anime.malId}', {
        'fields': 'my_list_status{comments,score,is_rewatching,updated_at,num_times_rewatched,start_date,finish_date}',
      });
      final e = d['my_list_status'] as Map?;
      if (e != null) {
        result = ListEntry(
          anime,
          e['status'] as String? ?? 'watching',
          number(e['num_episodes_watched']),
          notes: e['comments'] as String? ?? '',
          score: number(e['score']) * 10.0,
          isRepeating: e['is_rewatching'] == true,
          updatedAt: DateTime.tryParse(e['updated_at'] as String? ?? ''),
          repeat: number(e['num_times_rewatched']),
          startedAt: FuzzyDate.parse(e['start_date'] as String?),
          completedAt: FuzzyDate.parse(e['finish_date'] as String?),
        );
      }
    }
    _entries[_entryKey(service, anime)] = result;
    return result;
  }

  Iterable<String> services(Anime anime) => auth.accounts.keys
      .where((s) => s == 'AniList' ? anime.id > 0 : anime.malId > 0)
      .toList();
  Future<void> _each(Anime anime, Future<void> Function(String) action) async {
    if (!auth.hasAccounts) return;
    final supported = services(anime).toList();
    final failures = auth.accounts.keys
        .where((s) => !supported.contains(s))
        .toList();
    await Future.wait(
      services(anime).map((service) async {
        try {
          await action(service);
        } catch (_) {
          failures.add(service);
        }
      }),
    );
    revision++;
    lastError = failures.isEmpty
        ? null
        : '${failures.join(' and ')} sync failed. Check Accounts and retry.';
    notifyListeners();
    if (lastError != null) throw LoginFailure(lastError!);
  }

  Future<void> _serial(Anime anime, Future<void> Function() action) {
    final previous = _writes[anime.key] ?? Future.value();
    final next = previous.catchError((Object _) {}).then((_) => action());
    _writes[anime.key] = next;
    return next;
  }

  Future<void> _save(
    String service,
    Anime anime, {
    String? status,
    int? progress,
    double? score,
    String? notes,
    int? repeat,
    FuzzyDate? startedAt,
    FuzzyDate? completedAt,
    bool omitRewatching = false,
  }) async {
    if (service == 'AniList') {
      await auth.graph(
        '''mutation(\$id:Int,\$status:MediaListStatus,\$progress:Int,\$score:Int,\$notes:String,\$repeat:Int,\$started:FuzzyDateInput,\$completed:FuzzyDateInput){SaveMediaListEntry(mediaId:\$id,status:\$status,progress:\$progress,scoreRaw:\$score,notes:\$notes,repeat:\$repeat,startedAt:\$started,completedAt:\$completed){id}}''',
        {
          'id': anime.id,
          'status': ?status,
          'progress': ?progress,
          if (score != null) 'score': score.round(),
          'notes': ?notes,
          'repeat': ?repeat,
          if (startedAt != null) 'started': startedAt.toJson(),
          if (completedAt != null) 'completed': completedAt.toJson(),
        },
      );
    } else {
      await auth.mal('PATCH', '/anime/${anime.malId}/my_list_status', {
        if (status != null) 'status': malStatus(status),
        if (!omitRewatching && status == 'REPEATING') 'is_rewatching': 'true',
        if (!omitRewatching && status != null && status != 'REPEATING')
          'is_rewatching': 'false',
        if (progress != null) 'num_watched_episodes': '$progress',
        if (score != null) 'score': '${(score / 10).round().clamp(0, 10)}',
        'comments': ?notes,
        if (repeat != null) 'num_times_rewatched': '$repeat',
        if (startedAt != null) 'start_date': startedAt.mal,
        if (completedAt != null) 'finish_date': completedAt.mal,
      });
    }
    final previous = cached(service, anime);
    _entries[_entryKey(service, anime)] = ListEntry(
      anime,
      status == null
          ? previous?.status ?? (service == 'MAL' ? 'watching' : 'CURRENT')
          : service == 'MAL'
          ? malStatus(status)
          : status,
      progress ?? previous?.progress ?? 0,
      notes: notes ?? previous?.notes ?? '',
      score: score ?? previous?.score ?? 0,
      updatedAt: now(),
      isPrivate: previous?.isPrivate ?? false,
      repeat: repeat ?? previous?.repeat ?? 0,
      startedAt: startedAt ?? previous?.startedAt ?? const FuzzyDate(),
      completedAt: completedAt ?? previous?.completedAt ?? const FuzzyDate(),
      isRepeating: status == null
          ? previous?.isRepeating ?? false
          : status == 'REPEATING',
    );
  }

  Future<bool> saveReconciled(
    String service,
    ListEntry desired, {
    SyncWritePart part = SyncWritePart.state,
  }) async {
    var written = false;
    await _serial(desired.anime, () async {
      final current = cached(service, desired.anime);
      if (part == SyncWritePart.repeat) {
        if (current == null ||
            normalizeTrackerEntry(current).status != desired.status ||
            current.progress != desired.progress ||
            current.score != desired.score ||
            current.repeat > desired.repeat) {
          return;
        }
        await auth.mal(
          'PATCH',
          '/anime/${desired.anime.malId}/my_list_status',
          {'num_times_rewatched': '${desired.repeat}'},
        );
        _entries[_entryKey(service, desired.anime)] = current.copyWith(
          repeat: desired.repeat,
          updatedAt: now(),
        );
        written = true;
        return;
      }
      if (current != null && incomingIsNewer(desired, current)) return;
      await _save(
        service,
        desired.anime,
        status: desired.status,
        progress: desired.progress,
        score: desired.score,
        repeat: part == SyncWritePart.completion ? null : desired.repeat,
        startedAt: desired.startedAt,
        completedAt: desired.completedAt,
        omitRewatching: part == SyncWritePart.completion,
      );
      written = true;
    });
    return written;
  }

  Future<Anime> identify(Anime anime, {required bool adult}) async {
    if (anime.id > 0) return anime;
    final d = await auth.graph(
      'query(\$mal:Int,\$adult:Boolean){Media(idMal:\$mal,type:ANIME,isAdult:\$adult){$mediaFields}}',
      {'mal': anime.malId, 'adult': adult ? null : false},
    );
    if (d['Media'] is! Map) return anime;
    return mergeAnimeMetadata(
      Anime.fromJson((d['Media'] as Map).cast<String, dynamic>()),
      anime,
    );
  }

  Future<void> update(
    Anime anime, {
    String? status,
    int? progress,
    double? score,
  }) => _serial(
    anime,
    () => _each(anime, (s) async {
      await _save(s, anime, status: status, progress: progress, score: score);
      if (progress != null && !closed) onProgressWritten?.call();
    }),
  );

  Future<SyncedResume> resume(Anime anime) async {
    var best = noResume;
    await Future.wait(
      services(anime).map((s) async {
        try {
          final e = await entry(s, anime).timeout(const Duration(seconds: 4));
          final p = parseSyncedResumeTag(e?.notes ?? '');
          if (p != null) best = laterSyncedResume(best, p);
        } catch (_) {
          /* A tracker lookup never blocks playback. */
        }
      }),
    );
    return best;
  }

  ListEntry? mergedCached(Anime anime) {
    ListEntry? result;
    for (final service in services(anime)) {
      final entry = cached(service, anime);
      if (entry != null) {
        result = result == null
            ? normalizeTrackerEntry(entry)
            : mergeAnimeEntries(result, entry);
      }
    }
    return result;
  }

  int progress(Anime anime) => mergedCached(anime)?.progress ?? 0;

  Future<void> pushResume(Anime anime, SyncedResume p) => _serial(
    anime,
    () => _each(anime, (s) async {
      final e = await entry(s, anime);
      if (e == null) return;
      final notes = withSyncedResumeTag(e.notes, p);
      if (notes != e.notes) await _save(s, anime, notes: notes);
    }),
  );
  Future<void> finish(Anime anime, int episode, int total) => _serial(
    anime,
    () => _each(anime, (s) async {
      final e = await entry(s, anime);
      final progress = max(episode, e?.progress ?? 0);
      final complete = total > 0 && progress >= total;
      await _save(
        s,
        anime,
        progress: progress,
        status: complete
            ? 'COMPLETED'
            : (s == 'MAL'
                  ? const {
                          'plan_to_watch': 'CURRENT',
                          'watching': 'CURRENT',
                          'on_hold': 'CURRENT',
                          'dropped': 'CURRENT',
                          'completed': 'COMPLETED',
                        }[e?.status] ??
                        'CURRENT'
                  : e?.status == 'REPEATING'
                  ? 'REPEATING'
                  : e?.status == 'COMPLETED'
                  ? 'COMPLETED'
                  : 'CURRENT'),
        notes: syncedResumeAfterEpisode(e?.notes ?? '', episode, total),
      );
      if (!closed) onProgressWritten?.call();
    }),
  );
  ResumePusher pusher() => ResumePusher(
    push: (key, p) => pushResume(_shows[key]!, p),
    finish: (key, ep, total) => finish(_shows[key]!, ep, total),
    onError: (e) {
      lastError = e.toString();
      notifyListeners();
    },
  );
  void remember(Anime anime) => _shows[anime.key] = anime;
}
