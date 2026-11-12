import 'dart:async';
import 'dart:convert';

import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../host.dart';
import 'continuation.dart';

// New episode alerts for shows being watched. Android checks AniList's
// public airing times in the background, with no sign-in and no push
// service. Computers say so in the app while it is open.

class EpisodeAlerts {
  EpisodeAlerts(this.preferences, {DateTime Function()? now})
    : now = now ?? DateTime.now;
  final SharedPreferences preferences;
  final DateTime Function() now;

  static const _sentKey = 'EpisodeAlerts.sent';
  static const _channel = MethodChannel('otakase/device');

  /// Shows a short line in the app, set by the app shell.
  void Function(String text)? show;

  Timer? _timer;
  List<UpcomingEpisode> _shows = const [];
  String? _handed;

  /// Follows the shows being watched that have an episode coming.
  void watch(List<UpcomingEpisode> upNext, {required bool on}) {
    final shows = [
      for (final u in upNext)
        if (u.anime.id > 0) u,
    ];
    if (Host.android) {
      final text = jsonEncode({
        'on': on,
        'shows': [
          for (final u in shows)
            {'id': u.anime.id, 'title': u.anime.title, 'episode': u.episode},
        ],
      });
      if (text == _handed) return;
      _handed = text;
      unawaited(
        _channel
            .invokeMethod<void>('episodeAlerts', jsonDecode(text))
            .catchError((Object _) {}),
      );
      return;
    }
    _shows = on ? shows : const [];
    _timer?.cancel();
    _timer = null;
    if (_shows.isEmpty) return;
    check();
    _timer = Timer.periodic(const Duration(minutes: 5), (_) => check());
  }

  /// Says which followed episodes came out since they were last looked at.
  void check() {
    final at = now().millisecondsSinceEpoch ~/ 1000;
    final sent = (preferences.getStringList(_sentKey) ?? const []).toSet();
    var changed = false;
    for (final u in _shows) {
      final key = '${u.anime.id}:${u.episode}';
      if (u.airingAt > at || sent.contains(key)) continue;
      sent.add(key);
      changed = true;
      show?.call('Episode ${u.episode} of ${u.anime.title} is out');
    }
    if (changed) {
      // Only recent ones matter.
      final keep = sent.toList();
      unawaited(
        preferences.setStringList(
          _sentKey,
          keep.sublist(keep.length > 200 ? keep.length - 200 : 0),
        ),
      );
    }
  }

  void dispose() {
    _timer?.cancel();
    _timer = null;
  }
}
