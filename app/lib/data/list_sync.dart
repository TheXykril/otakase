import 'dart:async';

import 'package:flutter/foundation.dart';

import 'auth.dart';
import 'models.dart';
import 'store.dart';
import 'tracking.dart';

class ListSync extends ChangeNotifier {
  ListSync({
    required this.tracking,
    required this.store,
    DateTime Function()? now,
    Future<void> Function(Duration)? delay,
  }) : now = now ?? DateTime.now,
       delay = delay ?? Future<void>.delayed {
    identity = _identity;
    tracking.auth.addListener(_accountsChanged);
  }
  final TrackingService tracking;
  final AppStore store;
  final DateTime Function() now;
  final Future<void> Function(Duration) delay;
  bool running = false, closed = false;
  int epoch = 0;
  String identity = '';
  Future<void>? pending;
  String get _identity => ['AniList', 'MAL']
      .map(
        (s) =>
            '${tracking.auth.account(s)?.userId}:${tracking.auth.account(s)?.username}',
      )
      .join('|');
  bool get enabled => store.flag('SyncAniListMAL');
  bool get ready =>
      tracking.auth.signedIn('AniList') && tracking.auth.signedIn('MAL');
  void _accountsChanged() {
    if (identity == _identity) return;
    identity = _identity;
    epoch++;
    _scope();
  }

  void _scope() {
    if (store.text('ListSyncIdentity') == identity) return;
    for (final key in [
      'ListSyncStarted',
      'ListSyncCompleted',
      'ListSyncChanges',
      'ListSyncError',
      'ListSyncRetry',
    ]) {
      unawaited(store.set(key, null));
    }
    unawaited(store.set('ListSyncIdentity', identity));
  }

  String get status {
    if (!enabled) return 'Sync is off';
    if (running) return 'Syncing your lists';
    final error = store.text('ListSyncError');
    if (error.isNotEmpty) return error;
    final at = store.integer('ListSyncCompleted');
    if (at == 0) return 'Lists sync in the background';
    final minutes = now()
        .difference(DateTime.fromMillisecondsSinceEpoch(at))
        .inMinutes;
    final ago = minutes < 1
        ? 'just now'
        : minutes < 60
        ? '$minutes min ago'
        : '${minutes ~/ 60} h ago';
    return 'Synced ${store.integer('ListSyncChanges')} changes · $ago';
  }

  Future<void> refresh() async {
    if (!enabled || !ready || running || closed) return;
    try {
      final page = await tracking.combinedList(adult: store.adultAllowed);
      afterLoad(page, force: true);
    } catch (e) {
      await store.set('ListSyncError', e.toString());
      if (!closed) notifyListeners();
    }
  }

  void afterLoad(CombinedAccountPage page, {bool force = false}) {
    if (!enabled || !ready || running || closed || page.notLoaded.isNotEmpty) {
      return;
    }
    _scope();
    final last = store.integer('ListSyncStarted');
    if (!force &&
        last > 0 &&
        now().difference(DateTime.fromMillisecondsSinceEpoch(last)) <
            const Duration(minutes: 30)) {
      return;
    }
    running = true;
    final generation = epoch;
    pending = Future<void>(() => _run(page, generation));
    notifyListeners();
  }

  bool _active(int generation) =>
      !closed && generation == epoch && enabled && ready;
  Future<void> _run(CombinedAccountPage page, int generation) async {
    var changed = 0;
    try {
      if (!_active(generation)) return;
      await store.set('ListSyncStarted', now().millisecondsSinceEpoch);
      await store.set('ListSyncError', '');
      final adult = store.adultAllowed;
      final known = page.anilist.map((e) => e.anime.malId).toSet();
      final mal = <ListEntry>[];
      var mapped = 0;
      for (final entry in page.mal) {
        if (!_active(generation)) return;
        if ((!adult && entry.anime.isAdult) ||
            known.contains(entry.anime.malId) ||
            entry.anime.id > 0 ||
            mapped >= 100) {
          mal.add(entry);
          continue;
        }
        if (mapped > 0) await delay(const Duration(milliseconds: 500));
        mapped++;
        try {
          final anime = await tracking.identify(entry.anime, adult: adult);
          mal.add(entry.copyWith(anime: anime));
        } on LoginFailure catch (e) {
          if (e.statusCode == 429) rethrow;
          mal.add(entry);
        }
      }
      final writes = <TrackerSyncWrite>[];
      final retry = store.value('ListSyncRetry');
      if (retry is Map) {
        try {
          writes.add(TrackerSyncWrite.fromJson(retry.cast<String, dynamic>()));
        } catch (_) {
          await store.set('ListSyncRetry', null);
        }
      }
      writes.addAll(planTrackerSync(page.anilist, mal, adult: adult));
      var attempted = 0;
      final failures = <String>{};
      for (final write in writes.take(100)) {
        if (write.part == SyncWritePart.completion && attempted == 99) break;
        if (!_active(generation)) return;
        if (attempted > 0) await delay(const Duration(milliseconds: 500));
        if (!_active(generation)) return;
        if (!store.adultAllowed && write.entry.anime.isAdult) continue;
        attempted++;
        try {
          if (await tracking.saveReconciled(
            write.service,
            write.entry,
            part: write.part,
          )) {
            changed++;
          }
          if (write.part == SyncWritePart.repeat) {
            await store.set('ListSyncRetry', null);
          }
        } on LoginFailure catch (e) {
          if (write.part == SyncWritePart.repeat) {
            await store.set('ListSyncRetry', write.toJson());
          }
          if (e.statusCode == 429) rethrow;
          failures.add(write.service);
        } catch (_) {
          if (write.part == SyncWritePart.repeat) {
            await store.set('ListSyncRetry', write.toJson());
          }
          failures.add(write.service);
        }
      }
      if (!_active(generation)) return;
      await store.set('ListSyncChanges', changed);
      await store.set('ListSyncCompleted', now().millisecondsSinceEpoch);
      if (failures.isNotEmpty) {
        await store.set(
          'ListSyncError',
          '${failures.join(' and ')} sync failed. Refresh to retry.',
        );
      }
    } catch (e) {
      if (_active(generation)) {
        await store.set('ListSyncChanges', changed);
        await store.set(
          'ListSyncError',
          e is LoginFailure && e.statusCode == 429
              ? 'Tracker rate limit reached. Retry on the next refresh.'
              : 'List sync failed. Refresh to retry.',
        );
      }
    } finally {
      running = false;
      if (!closed) notifyListeners();
    }
  }

  @override
  void dispose() {
    closed = true;
    epoch++;
    tracking.auth.removeListener(_accountsChanged);
    super.dispose();
  }
}
