import 'dart:async';

import '../core.dart';
import '../desktop/desktop_core.dart';
import 'models.dart';
import 'store.dart';

/// What is playing, in the viewer's Discord status: "Watching" the show,
/// the episode, a cover and time left, as the command-line app showed. Only
/// on desktop, where the Discord app is on the same computer. 18+ titles are
/// never shown.
class DiscordPresence {
  DiscordPresence(this.core, this.store, {bool? desktop})
    : desktop = desktop ?? isDesktop;
  final CoreClient core;
  final AppStore store;
  final bool desktop;

  String _shown = '';
  DateTime _sentAt = DateTime.fromMillisecondsSinceEpoch(0);
  double _expected = 0;
  bool _visible = false, _sending = false;

  /// Sent again this often unchanged, so Discord keeps it.
  static const refresh = Duration(minutes: 2);

  bool get enabled => desktop && store.flag('DiscordPresence');

  /// Shows an episode. Called every few seconds while it plays; only a
  /// change, a jump or the refresh interval reaches Discord.
  Future<void> episode(
    Anime anime,
    int episode, {
    required bool paused,
    required double position,
    required double duration,
    String joinUrl = '',
  }) {
    if (anime.isAdult) return clear();
    return _show(
      title: anime.title,
      state: 'Episode $episode',
      cover: anime.coverLarge.isNotEmpty ? anime.coverLarge : anime.cover,
      paused: paused,
      position: position,
      duration: duration,
      buttons: [
        if (joinUrl.startsWith('https://'))
          {'label': 'Watch with me', 'url': joinUrl},
        if (anime.id > 0)
          {
            'label': 'View on AniList',
            'url': 'https://anilist.co/anime/${anime.id}',
          },
        if (anime.malId > 0 && !joinUrl.startsWith('https://'))
          {
            'label': 'View on MAL',
            'url': 'https://myanimelist.net/anime/${anime.malId}',
          },
      ],
    );
  }

  /// Shows a film or a series episode.
  Future<void> movie(
    String title,
    String subtitle, {
    String cover = '',
    String imdb = '',
    required bool paused,
    required double position,
    required double duration,
  }) => _show(
    title: title,
    state: subtitle.isEmpty ? 'Film' : subtitle,
    cover: cover,
    paused: paused,
    position: position,
    duration: duration,
    buttons: [
      if (RegExp(r'^tt\d+$').hasMatch(imdb))
        {'label': 'View on IMDb', 'url': 'https://www.imdb.com/title/$imdb/'},
    ],
  );

  Future<void> _show({
    required String title,
    required String state,
    required String cover,
    required bool paused,
    required double position,
    required double duration,
    required List<Map<String, String>> buttons,
  }) async {
    if (!enabled) return clear();
    if (title.trim().isEmpty || _sending) return;
    final now = DateTime.now();
    final key = '$title|$state|$paused';
    final elapsed = now.difference(_sentAt).inMilliseconds / 1000;
    final jumped = (position - (_expected + (paused ? 0 : elapsed))).abs() > 5;
    if (key == _shown && !jumped && now.difference(_sentAt) < refresh) return;
    _sending = true;
    try {
      await core.request({
        'operation': 'presence_set',
        'presence': {
          'title': title.length > 120 ? '${title.substring(0, 117)}...' : title,
          'state': state,
          'cover': cover,
          'paused': paused,
          'position': position,
          'duration': duration,
          'buttons': buttons.take(2).toList(),
        },
      }, timeout: const Duration(seconds: 10));
      _visible = true;
    } catch (_) {
      // Discord is not running: tried again at the next refresh.
    } finally {
      _shown = key;
      _sentAt = now;
      _expected = position;
      _sending = false;
    }
  }

  /// Takes the status down when playback ends.
  Future<void> clear() async {
    _shown = '';
    _sentAt = DateTime.fromMillisecondsSinceEpoch(0);
    if (!_visible) return;
    _visible = false;
    try {
      await core.request({
        'operation': 'presence_clear',
      }, timeout: const Duration(seconds: 5));
    } catch (_) {
      /* Nothing is shown once the app closes anyway. */
    }
  }
}
