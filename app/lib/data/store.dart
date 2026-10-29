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
];
// Evens out loudness while playing: quiet dialogue comes up and loud effects
// come down. Uses ffmpeg's dynaudnorm (about a 3 s window, at most 10x gain).
const volumeLeveller = 'lavfi=[dynaudnorm=f=200:g=15:p=0.95:m=10]';

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
  'NextEpisodePrompt': false,
  'SkipOp': true,
  'SkipEd': true,
  'SkipRecap': true,
  'NormalizeVolume': true,
  'SkipFiller': true,
  'SkipDelay': 0,
  'Quality': 'best',
  'ResumePlayback': true,
  'PercentageToMarkComplete': 85,
  'Provider': 'stacked',
  'ProviderOrder': sourceNames,
  'DisabledProviders': <String>[],
  'AutoFallback': true,
  'CheckUpdates': true,
  'SyncAniListMAL': true,
  'DiscordShareWatching': false,
  'DiscordShareRatings': 'ask',
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
  List<WatchEntry> get continuing =>
      history.where((h) => !h.watched.contains(h.episode)).toList();
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
  }) async {
    final old = entry(anime);
    _history.remove(old?.anime.key);
    final watched = {...old?.watched ?? <int>[]};
    if (completed) watched.add(episode);
    _history[anime.key] = WatchEntry(
      anime: anime,
      episode: episode,
      position: completed ? 0 : position,
      duration: duration,
      watched: watched.toList()..sort(),
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
