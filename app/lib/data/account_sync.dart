import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'account.dart';
import 'models.dart';
import 'profiles.dart';

// Settings, profiles, per-show choices and resume spots follow the account.
//
// The device stays the source: everything is saved here first and copied to
// the account in the background. For each profile and kind the device keeps
// the copy it last exchanged with the server; a field that differs from it
// changed here and goes up stamped with the time it was noticed. The server
// keeps the newer change per field, so two devices editing different
// settings never undo each other.

/// Parts of the sync a device can switch off.
enum SyncPart { profiles, settings, shows, logins }

extension SyncPartText on SyncPart {
  String get label => switch (this) {
    SyncPart.profiles => 'Profiles and locks',
    SyncPart.settings => 'Settings',
    SyncPart.shows => 'Show choices and resume spots',
    SyncPart.logins => 'AniList, MAL and Trakt logins',
  };
}

/// Settings that belong to this device only.
const deviceOnlySettings = {
  'KeepScreenOn',
  'EpisodeAlerts',
  'TorrentOnMobileData',
  'CheckUpdates',
  'DevBuilds',
  'ContentAsked',
  'DownloadSpots',
  'DiscordInbox',
  'DiscordNews',
  'DiscordClub',
  'Milestone',
  'AniSkipSubmitter',
  'SkipVotes',
};

/// Caches and bookkeeping, by prefix: never synced.
const _cachePrefixes = ['ListCache.', 'PublicListCache.', 'ListSync'];

/// Per-show choices: sources that worked, the local list, scores, hidden
/// Continue watching items and remembered tracks.
const _showKeys = {
  'WorkingSources',
  'WorkingMovieSources',
  'LocalList',
  'HiddenContinue',
  'ShowTracks',
};
const _showPrefixes = [
  'LocalStatus.',
  'LocalScore.',
  'TitleScore.',
  'ListCategory.',
];

/// Which synced kind a setting key goes in, or null for none.
String? settingKind(String key) {
  if (deviceOnlySettings.contains(key) ||
      _cachePrefixes.any(key.startsWith) ||
      key == 'AdultContent' ||
      key == 'ContinueWatchingAdult') {
    return null;
  }
  if (_showKeys.contains(key) || _showPrefixes.any(key.startsWith)) {
    return 'shows';
  }
  return 'settings';
}

/// How many resume spots travel: the most recent shows only.
const resumeShows = 30;

/// Device switches for each part, per account.
class SyncSwitches {
  SyncSwitches(this.preferences, this.account);
  final SharedPreferences preferences;
  final String account;
  bool on(SyncPart part) =>
      preferences.getBool('account.$account.sync.${part.name}') ?? true;
  Future<void> set(SyncPart part, bool value) =>
      preferences.setBool('account.$account.sync.${part.name}', value);
  Set<String> get kinds => {
    if (on(SyncPart.profiles)) 'profile',
    if (on(SyncPart.settings)) 'settings',
    if (on(SyncPart.shows)) ...['shows', 'resume'],
  };
}

/// Keeps one account's profiles on this device in step with the server.
class AccountSync extends ChangeNotifier {
  AccountSync({
    required this.session,
    required this.profiles,
    required this.preferences,
    this.pushDelay = const Duration(seconds: 10),
    this.resumeEvery = const Duration(minutes: 1),
    DateTime Function()? now,
  }) : now = now ?? DateTime.now,
       switches = SyncSwitches(preferences, session.id);
  final AccountSession session;
  final ProfileManager profiles;
  final SharedPreferences preferences;
  final SyncSwitches switches;
  final Duration pushDelay, resumeEvery;
  final DateTime Function() now;
  Timer? _timer;
  Future<void>? _running;
  bool _again = false, _pushResume = false, closed = false;
  DateTime _resumeSent = DateTime.fromMillisecondsSinceEpoch(0);
  String? lastError;
  DateTime? lastSynced;

  String get _p => 'account.${session.id}';
  int get version => preferences.getInt('$_p.sync.version') ?? 0;

  @override
  void notifyListeners() {
    if (!closed) super.notifyListeners();
  }

  // ---- What the device has now ------------------------------------------

  Json _settings(String profile) {
    final store = profiles.storeOf(profile);
    if (store != null) return store.syncedValues();
    try {
      return (jsonDecode(
        preferences.getString('profile.$profile.settings.v1') ?? '{}',
      ) as Map).cast<String, dynamic>();
    } catch (_) {
      return {};
    }
  }

  List<WatchEntry> _history(String profile) {
    final store = profiles.storeOf(profile);
    if (store != null) return store.allHistory;
    try {
      return [
        for (final e in jsonDecode(
          preferences.getString('profile.$profile.history.v1') ?? '[]',
        ) as List)
          WatchEntry.fromJson((e as Map).cast<String, dynamic>()),
      ];
    } catch (_) {
      return [];
    }
  }

  /// The fields of one kind for a profile, as they are on this device.
  Json current(String profile, String kind) {
    switch (kind) {
      case 'profile':
        final list = profiles.profiles;
        final i = list.indexWhere((p) => p.id == profile);
        if (i < 0) return {'_deleted': true};
        final p = list[i];
        return {
          'name': p.name,
          'color': p.color,
          'lock': p.lockHash,
          'salt': p.lockSalt,
          'order': i,
        };
      case 'settings':
      case 'shows':
        return {
          for (final MapEntry(:key, :value) in _settings(profile).entries)
            if (settingKind(key) == kind) key: value,
        };
      case 'resume':
        final list = _history(profile)
          ..sort((a, b) => b.updated.compareTo(a.updated));
        return {
          for (final e in list.take(resumeShows))
            e.anime.key: {
              ...e.toJson(),
              // Spots of other episodes stay on the device that watched.
              'viewing': const {},
            },
        };
    }
    return {};
  }

  // ---- What was last exchanged -----------------------------------------

  Json _sent(String profile, String kind) {
    try {
      return (jsonDecode(
        preferences.getString('$_p.sent.$profile.$kind') ?? '{}',
      ) as Map).cast<String, dynamic>();
    } catch (_) {
      return {};
    }
  }

  Json _times(String profile, String kind) {
    try {
      return (jsonDecode(
        preferences.getString('$_p.times.$profile.$kind') ?? '{}',
      ) as Map).cast<String, dynamic>();
    } catch (_) {
      return {};
    }
  }

  Future<void> _remember(String profile, String kind, Json sent, Json times) =>
      Future.wait([
        preferences.setString('$_p.sent.$profile.$kind', jsonEncode(sent)),
        preferences.setString('$_p.times.$profile.$kind', jsonEncode(times)),
      ]);

  /// Fields that changed here since the last exchange.
  Map<String, (Object?, int)> _changes(String profile, String kind, int at) {
    final now_ = current(profile, kind);
    final sent = _sent(profile, kind);
    final out = <String, (Object?, int)>{};
    for (final MapEntry(:key, :value) in now_.entries) {
      if (!sent.containsKey(key) ||
          jsonEncode(sent[key]) != jsonEncode(value)) {
        out[key] = (value, at);
      }
    }
    for (final key in sent.keys) {
      if (!now_.containsKey(key) && sent[key] != null) out[key] = (null, at);
    }
    return out;
  }

  // ---- Running ----------------------------------------------------------

  /// Something changed on this device: send it after a short wait, so a
  /// burst of changes goes up in one call.
  void changed({bool resume = false}) {
    if (closed) return;
    if (resume) _pushResume = true;
    _timer?.cancel();
    _timer = Timer(pushDelay, () => unawaited(sync()));
  }

  /// Sends what waits now, as the app goes to the background.
  Future<void> flush() {
    _timer?.cancel();
    _pushResume = true;
    _resumeSent = DateTime.fromMillisecondsSinceEpoch(0);
    return sync();
  }

  /// Pulls changes, then pushes this device's. One run at a time.
  Future<void> sync() {
    if (closed || session.signedOut) return Future.value();
    if (_running != null) {
      _again = true;
      return _running!;
    }
    return _running = () async {
      try {
        do {
          _again = false;
          await _pull();
          await _push();
        } while (_again && !closed);
        lastError = null;
        lastSynced = now();
      } catch (e) {
        lastError = e.toString();
      } finally {
        _running = null;
        notifyListeners();
      }
    }();
  }

  Future<void> _pull() async {
    final r = await session.request('GET', '/sync?since=$version');
    final docs = (r['docs'] as List? ?? []).whereType<Map>().toList();
    final kinds = switches.kinds;
    // Profiles first, so new ones exist before their settings arrive.
    docs.sort(
      (a, b) =>
          (a['kind'] == 'profile' ? 0 : 1) - (b['kind'] == 'profile' ? 0 : 1),
    );
    for (final raw in docs) {
      final doc = raw.cast<String, dynamic>();
      final kind = doc['kind'] as String? ?? '';
      final profile = doc['profile'] as String? ?? '';
      if (!kinds.contains(kind)) continue;
      await _apply(
        profile,
        kind,
        (doc['fields'] as Map? ?? {}).cast<String, dynamic>(),
        (doc['times'] as Map? ?? {}).cast<String, dynamic>(),
      );
    }
    await preferences.setInt('$_p.sync.version', number(r['version']));
  }

  /// Takes the fields from the server that are newer than this device's.
  Future<void> _apply(
    String profile,
    String kind,
    Json fields,
    Json times,
  ) async {
    final bound = profiles.profiles.any(
      (p) => p.id == profile && p.account == session.id,
    );
    if (kind != 'profile' && !bound) return;
    final known = _times(profile, kind);
    final sent = _sent(profile, kind);
    final local = _changes(profile, kind, now().millisecondsSinceEpoch);
    final take = <String, dynamic>{};
    for (final MapEntry(:key, :value) in fields.entries) {
      final t = number(times[key]);
      if (t <= number(known[key])) continue;
      // A change made here and not sent yet wins only when it is newer.
      if (local.containsKey(key) && local[key]!.$2 > t && kind != 'profile') {
        continue;
      }
      take[key] = value;
      known[key] = t;
      sent[key] = value;
    }
    if (take.isEmpty) return;
    switch (kind) {
      case 'profile':
        if (fields['_deleted'] == true) {
          if (bound) await profiles.removeSynced(profile);
          await _remember(profile, kind, sent, known);
          return;
        }
        await profiles.upsertSynced(session.id, profile, fields);
      case 'settings':
      case 'shows':
        await profiles.applySettings(profile, take);
      case 'resume':
        await profiles.applyResume(profile, [
          for (final v in take.values)
            if (v is Map) WatchEntry.fromJson(v.cast<String, dynamic>()),
        ]);
    }
    await _remember(profile, kind, sent, known);
  }

  Future<void> _push() async {
    final at = now().millisecondsSinceEpoch;
    final kinds = switches.kinds;
    final sendResume =
        _pushResume && now().difference(_resumeSent) >= resumeEvery;
    final docs = <Json>[];
    final pending = <(String, String, Json, Json)>[];
    final ids = {
      for (final p in profiles.profiles)
        if (p.account == session.id) p.id,
      ...profiles.deletedSynced(session.id),
    };
    for (final profile in ids) {
      for (final kind in kinds) {
        if (kind == 'resume' && !sendResume) continue;
        final changes = _changes(profile, kind, at);
        if (changes.isEmpty) continue;
        final sent = _sent(profile, kind), times = _times(profile, kind);
        for (final MapEntry(:key, :value) in changes.entries) {
          sent[key] = value.$1;
          times[key] = value.$2;
        }
        docs.add({
          'profile': profile,
          'kind': kind,
          'fields': {for (final e in changes.entries) e.key: e.value.$1},
          'times': {for (final e in changes.entries) e.key: e.value.$2},
        });
        pending.add((profile, kind, sent, times));
      }
    }
    if (sendResume) {
      _pushResume = false;
      _resumeSent = now();
    }
    if (docs.isEmpty) return;
    // The server takes up to 32 documents a call.
    for (var i = 0; i < docs.length; i += 32) {
      final r = await session.request(
        'POST',
        '/sync',
        body: {
          'docs': docs.sublist(i, i + 32 > docs.length ? docs.length : i + 32),
        },
      );
      final next = number(r['version']);
      if (next == version + 1 && number(r['changed']) > 0) {
        // Only our own push moved the version on: nothing new to pull.
        await preferences.setInt('$_p.sync.version', next);
      } else if (next != version) {
        _again = true;
      }
    }
    for (final (profile, kind, sent, times) in pending) {
      await _remember(profile, kind, sent, times);
    }
    await profiles.forgetDeleted(session.id);
  }

  /// First sign-in on a device: either this device's profiles join the
  /// account (their values win), or the account's profiles come here.
  Future<void> start({required bool upload}) async {
    if (upload) {
      for (final p in profiles.profiles.where((p) => p.account.isEmpty)) {
        await profiles.bind(p.id, session.id);
      }
    }
    // The first sync carries the resume spots as well.
    _pushResume = true;
    _resumeSent = DateTime.fromMillisecondsSinceEpoch(0);
    await sync();
  }

  @override
  void dispose() {
    closed = true;
    _timer?.cancel();
    super.dispose();
  }
}
