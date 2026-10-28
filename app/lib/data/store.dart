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
  'SkipFiller': true,
  'SkipDelay': 0,
  'Quality': 'best',
  'ResumePlayback': true,
  'PercentageToMarkComplete': 85,
  'Provider': 'stacked',
  'ProviderOrder': sourceNames,
  'DisabledProviders': <String>[],
  'AutoFallback': true,
  'AdultContent': false,
  'ExperimentalMovies': false,
  'CheckUpdates': true,
};

class AppStore extends ChangeNotifier {
  AppStore(this.preferences) {
    try {
      _values.addAll(
        (jsonDecode(preferences.getString('settings.v1') ?? '{}') as Map)
            .cast<String, dynamic>(),
      );
    } catch (_) {
      /* A damaged preference falls back to defaults. */
    }
    // 'ask' opened a track menu at every episode start; it is gone.
    if (_values['SubStyle'] == 'ask') _values['SubStyle'] = 'soft';
    try {
      for (final e in (jsonDecode(
        preferences.getString('history.v1') ?? '[]',
      ) as List)) {
        final entry = WatchEntry.fromJson((e as Map).cast<String, dynamic>());
        _history[entry.anime.key] = entry;
      }
    } catch (_) {
      /* Keep settings usable when old history is invalid. */
    }
  }
  final SharedPreferences preferences;
  final Json _values = {};
  final Map<String, WatchEntry> _history = {};
  final Map<String, String> providerHealth = {};
  String avatar = '';
  dynamic value(String key) => _values[key] ?? settingDefaults[key];
  bool flag(String key) => value(key) == true;
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
      .where((h) => h.position > 0 || !h.watched.contains(h.episode))
      .toList();
  WatchEntry? entry(Anime anime) => _history[anime.key];
  Future<void> set(String key, dynamic val) async {
    _values[key] = val;
    notifyListeners();
    await preferences.setString('settings.v1', jsonEncode(_values));
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
        'history.v1',
        jsonEncode(_history.values.map((e) => e.toJson()).toList()),
      )
      .then((_) {});
}
