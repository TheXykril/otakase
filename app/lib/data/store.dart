import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'models.dart';

const sourceNames = [
  'anikoto',
  'megavid',
  'animex',
  'kickassanime',
  'anizone',
  'reanime',
  'nyaa',
];
// Evens out loudness while playing so dialogue stays clear next to loud
// effects and music. In order: fold surround tracks down to stereo with the
// centre (dialogue) channel forward and the LFE left out, pull back wide
// stereo music a little, trim deep bass, lift the speech band, squash loud
// peaks, then level the result over about a second. A plain loudness leveller
// lets a single bang or a loud score set the gain, which drops nearby voices
// far below the rest of the film. Uses the signal at each boundary so gain
// stays level after seeks. Keep in step with volumeLevellerFilter in Go.
const volumeLeveller =
    'lavfi=[aresample=ochl=stereo:clev=1.414:slev=0.5:lfe_mix_level=0,stereotools=slev=0.5,lowshelf=f=150:g=-8,equalizer=f=2500:t=o:w=2:g=4,acompressor=threshold=-30dB:ratio=6:attack=3:release=200:knee=6:link=maximum,dynaudnorm=f=150:g=7:p=0.95:m=8:b=1]';

/// The mpv `af` value for the viewer's NormalizeVolume setting.
String audioFilter(AppStore store) =>
    store.flag('NormalizeVolume') ? volumeLeveller : '';

const settingDefaults = <String, dynamic>{
  'AniListUsername': '',
  'MyAnimeListUsername': '',
  'SubOrDub': 'sub',
  'SubsLanguage': 'english',
  'AudioLanguages': 'japanese,chinese,english',
  'AutoAudioFallback': true,
  'SubStyle': 'soft',
  'SubtitleFont': 'Noto Sans JP',
  'SubtitleSize': 44,
  'SubtitleColour': '#FFFFFF',
  'SubtitleBackground': '#00000000',
  'SubtitleOutline': 2,
  'SubtitlePosition': 100,
  'HideEpisodeSpoilers': false,
  'NextEpisodePrompt': false,
  'SkipOp': true,
  'SkipEd': true,
  'SkipRecap': true,
  'NormalizeVolume': true,
  'SkipFiller': true,
  'SkipDelay': 0,
  'IntroDBSkipTimes': true,
  'AnimeSkipTimes': true,
  'ScoreOnCompletion': true,
  'DiscordPresence': true,
  'Quality': 'best',
  'ResumePlayback': true,
  'PercentageToMarkComplete': 85,
  'Provider': 'stacked',
  'ProviderOrder': sourceNames,
  'DisabledProviders': <String>[],
  'TorrentSources': true,
  'TorrentOnMobileData': false,
  'CheckUpdates': true,
  'KeepScreenOn': false,
  // Phones on the same Discord account control TVs through the bot when
  // the local network can't reach them.
  'RemoteAnywhere': true,
  'SyncAniListMAL': true,
  'DiscordShareWatching': false,
  'DiscordShareRatings': 'ask',
  // What the profile shows: anime, movies (films and series) or both.
  // Anime until the viewer picks, so an existing profile looks the same.
  'Content': 'anime',
  'MovieLanguages': ['english', 'lithuanian'],
  'DiscordWatchWithMe': false,
};

class AppStore extends ChangeNotifier {
  AppStore(this.preferences, {this.namespace = ''}) {
    try {
      _values.addAll(
        (jsonDecode(
          preferences.getString('${namespace}settings.v1') ?? '{}',
        ) as Map).cast<String, dynamic>(),
      );
    } catch (_) {
      /* A damaged preference falls back to defaults. */
    }
    _values.remove('AdultContent');
    _values.remove('ContinueWatchingAdult');
    // Own-mpv playback and an automatic fallback switch nothing read.
    _values.remove('ExternalMpv');
    _values.remove('MpvPath');
    _values.remove('AutoFallback');
    // 'ask' opened a track menu at every episode start; it is gone.
    if (_values['SubStyle'] == 'ask') _values['SubStyle'] = 'soft';
    try {
      for (final e in (jsonDecode(
        preferences.getString('${namespace}history.v1') ?? '[]',
      ) as List)) {
        final entry = WatchEntry.fromJson((e as Map).cast<String, dynamic>());
        _history[entry.anime.key] = entry;
      }
    } catch (_) {
      /* Keep settings usable when old history is invalid. */
    }
  }
  final SharedPreferences preferences;
  final String namespace;
  final Json _values = {};
  final Map<String, WatchEntry> _history = {};
  final Map<String, String> providerHealth = {};
  String avatar = '';
  dynamic value(String key) => _values[key] ?? settingDefaults[key];

  /// Whether this profile picked a value itself instead of the default.
  bool isSet(String key) => _values.containsKey(key);
  bool _adultAllowed = false;
  bool get adultAllowed => _adultAllowed;
  void setAdultAllowed(bool allowed) {
    if (_adultAllowed == allowed) return;
    _adultAllowed = allowed;
    notifyListeners();
  }

  bool flag(String key) =>
      key == 'AdultContent' ? _adultAllowed : value(key) == true;
  String text(String key) => value(key)?.toString() ?? '';
  int integer(String key) => number(value(key));
  List<String> strings(String key) =>
      (value(key) as List? ?? []).whereType<String>().toList();
  String? preferredSource(Anime anime) {
    final choices = value('WorkingSources');
    return choices is Map ? choices[anime.key] as String? : null;
  }

  Future<void> rememberSource(Anime anime, String source) async {
    if (preferredSource(anime) == source) return;
    final choices = Map<String, dynamic>.from(
      value('WorkingSources') as Map? ?? {},
    );
    choices[anime.key] = source;
    await set('WorkingSources', choices);
  }

  bool visible(Anime a) => flag('AdultContent') || !a.isAdult;
  List<WatchEntry> get history =>
      _history.values.where((h) => visible(h.anime)).toList()
        ..sort((a, b) => b.updated.compareTo(a.updated));
  List<WatchEntry> get continuing => history
      .where(
        (h) =>
            !h.watched.contains(h.episode) &&
            !continueHidden(h.anime.key, h.episode, h.updated),
      )
      .toList();

  /// Whether the viewer took this off Continue watching. It comes back once
  /// another episode plays or the spot moves on.
  bool continueHidden(String key, int episode, int updated) {
    final hidden = value('HiddenContinue');
    if (hidden is! Map || hidden[key] is! Map) return false;
    final h = hidden[key] as Map;
    return number(h['episode']) == episode && updated <= number(h['updated']);
  }

  /// Takes a show, film or series off Continue watching until it moves on.
  Future<void> hideContinue(String key, int episode, int updated) {
    final hidden = Map<String, dynamic>.from(
      value('HiddenContinue') as Map? ?? {},
    );
    hidden[key] = {'episode': episode, 'updated': updated};
    // Only the latest few matter; old ones have moved on long ago.
    while (hidden.length > 200) {
      hidden.remove(hidden.keys.first);
    }
    return set('HiddenContinue', hidden);
  }

  /// Shows a profile without AniList or MAL put on its own list, newest
  /// first, as status plus the show itself so Home can draw them.
  Future<void> setLocalStatus(Anime anime, String status) {
    final list = Map<String, dynamic>.from(value('LocalList') as Map? ?? {});
    list.remove(anime.key);
    list[anime.key] = {'status': status, 'anime': anime.toJson()};
    while (list.length > 500) {
      list.remove(list.keys.first);
    }
    return Future.wait([
      set('LocalList', list),
      set('LocalStatus.${anime.key}', status),
    ]);
  }

  List<Anime> localList(String status) {
    final list = value('LocalList');
    if (list is! Map) return const [];
    final shows = <Anime>[];
    for (final v in list.values.toList().reversed) {
      if (v is! Map || v['status'] != status || v['anime'] is! Map) continue;
      try {
        shows.add(Anime.fromJson((v['anime'] as Map).cast<String, dynamic>()));
      } catch (_) {
        /* A damaged entry is skipped. */
      }
    }
    return shows;
  }

  WatchEntry? entry(Anime anime) =>
      _history[anime.key] ??
      _history.values
          .where(
            (h) =>
                anime.id > 0 && h.anime.id == anime.id ||
                anime.malId > 0 && h.anime.malId == anime.malId,
          )
          .firstOrNull;
  Future<void> setWatchedProgress(Anime anime, int progress) async {
    final old = entry(anime);
    _history.remove(old?.anime.key);
    _history[anime.key] = WatchEntry(
      anime: anime,
      episode: progress + 1,
      position: 0,
      watched: List.generate(progress, (i) => i + 1),
      updated: DateTime.now().millisecondsSinceEpoch,
    );
    notifyListeners();
    await _persist();
  }

  Future<void> set(String key, dynamic val) async {
    _values[key] = val;
    notifyListeners();
    await preferences.setString('${namespace}settings.v1', jsonEncode(_values));
  }

  void health(String name, String status) {
    providerHealth[name] = status;
    notifyListeners();
  }

  Future<void> progress(
    Anime anime,
    int episode,
    int position,
    int duration, {
    bool completed = false,
    String? viewingKey,
    List<int>? viewingCheckpoint,
  }) async {
    final old = entry(anime);
    _history.remove(old?.anime.key);
    final watched = {...old?.watched ?? <int>[]};
    if (completed) watched.add(episode);
    final viewing = {...?old?.viewing};
    final resumes = {...?old?.resumes};
    if (old != null && old.position > 0 && !old.watched.contains(old.episode)) {
      resumes.putIfAbsent('${old.episode}', () => old.position);
    }
    if (watched.contains(episode)) {
      viewing.removeWhere((key, _) => key.startsWith('$episode:'));
      resumes.remove('$episode');
    } else {
      resumes['$episode'] = position;
      if (viewingKey != null && viewingCheckpoint != null) {
        final prior = viewing[viewingKey];
        if (prior == null ||
            prior[0] != viewingCheckpoint[0] ||
            prior[1] < viewingCheckpoint[1]) {
          viewing[viewingKey] = viewingCheckpoint;
        }
      }
    }
    _history[anime.key] = WatchEntry(
      anime: anime,
      episode: episode,
      position: completed ? 0 : position,
      duration: duration,
      watched: watched.toList()..sort(),
      viewing: viewing,
      resumes: resumes,
      updated: DateTime.now().millisecondsSinceEpoch,
    );
    notifyListeners();
    await _persist();
  }

  Future<void> markWatched(Anime anime, int episode, bool watched) async {
    final old = entry(anime);
    final list = {...old?.watched ?? <int>[]};
    if (watched) {
      list.add(episode);
    } else {
      list.remove(episode);
    }
    _history[anime.key] = WatchEntry(
      anime: anime,
      episode: old?.episode ?? episode,
      position: old?.position ?? 0,
      duration: old?.duration ?? 0,
      watched: list.toList()..sort(),
      viewing: old?.viewing ?? const {},
      resumes: old?.resumes ?? const {},
      updated: DateTime.now().millisecondsSinceEpoch,
    );
    notifyListeners();
    await _persist();
  }

  Future<void> _persist() => preferences
      .setString(
        '${namespace}history.v1',
        jsonEncode(_history.values.map((e) => e.toJson()).toList()),
      )
      .then((_) {});
}
