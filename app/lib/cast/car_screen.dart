import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import '../core.dart';
import '../data/models.dart';
import '../host.dart';
import 'cast.dart';

/// The Android Auto screen, as one more place to cast to.
///
/// Otakase shows its own screen in Android Auto while the car is parked.
/// Casting there works like casting to a TV: the phone sends the stream it
/// found and follows playback once a second. The car screen also lists
/// Continue watching; a pick there opens the episode here and casts it.
class CarScreen {
  CarScreen._();
  static final instance = CarScreen._();

  static const _channel = MethodChannel('otakase/car');

  /// Tests stand in for an Android phone.
  @visibleForTesting
  static bool? debugPhone;
  static bool get _phone => debugPhone ?? (Host.android && !Host.tv);

  /// What the cast menu lists while the car screen is open.
  static final device = CastDevice.fromJson(const {
    'id': 'car',
    'name': 'Car screen',
    'kind': 'car',
  });

  /// Opens a Continue watching entry the car asked for, casting it there.
  /// Set by the app shell on phones.
  void Function(WatchEntry entry)? play;

  final _offered = <String, WatchEntry>{};
  String _sent = '';
  bool _listening = false;

  void _listen() {
    if (_listening) return;
    _listening = true;
    _channel.setMethodCallHandler((call) async {
      if (call.method != 'pick') return null;
      final entry = _offered[call.arguments];
      if (entry != null) play?.call(entry);
      return null;
    });
  }

  /// Whether Android Auto has the Otakase screen open right now.
  Future<bool> connected() async {
    if (!_phone) return false;
    try {
      return await _channel.invokeMethod<bool>('connected') ?? false;
    } on MissingPluginException {
      return false;
    } on PlatformException {
      return false;
    }
  }

  /// Gives the car screen the phone's Continue watching row.
  void offer(Iterable<WatchEntry> entries) {
    if (!_phone) return;
    _listen();
    final list = entries.take(20).toList();
    final items = [
      for (final e in list)
        {
          'key': '${e.anime.key}#${e.episode}',
          'title': e.anime.title,
          'detail': shelfDetail(e),
        },
    ];
    final sent = items.map((i) => '${i['key']}|${i['detail']}').join(',');
    if (sent == _sent) return;
    _sent = sent;
    _offered
      ..clear()
      ..addAll({for (final e in list) '${e.anime.key}#${e.episode}': e});
    unawaited(
      _channel.invokeMethod<void>('shelf', items).catchError((Object _) {}),
    );
  }

  Future<void> start({
    required ResolvedStream stream,
    required String title,
    String subtitle = '',
    double start = 0,
    double duration = 0,
  }) async {
    final headers = Map<String, String>.of(stream.headers);
    if (stream.referrer.isNotEmpty) headers['Referer'] = stream.referrer;
    try {
      await _channel.invokeMethod<void>('start', {
        'url': stream.url,
        'headers': headers,
        'hls': stream.hls || stream.url.contains('.m3u8'),
        'subtitle': subtitle,
        'start': start,
        'duration': duration,
        'title': title,
      });
    } on PlatformException catch (e) {
      throw CoreFailure('car', e.message ?? 'The car screen did not answer.');
    }
  }

  Future<CastStatus> status() async {
    final data = await _channel.invokeMethod<Map<Object?, Object?>>('status');
    return CastStatus.fromJson({
      for (final e in (data ?? const {}).entries) '${e.key}': e.value,
    });
  }

  Future<void> control(String action, [double value = 0]) async {
    try {
      await _channel.invokeMethod<void>('control', {
        'action': action,
        'value': value,
      });
    } on PlatformException catch (e) {
      throw CoreFailure('car', e.message ?? 'The car screen did not answer.');
    }
  }
}

/// The second line of a car screen Continue watching row.
@visibleForTesting
String shelfDetail(WatchEntry e) {
  final episode = e.anime.airedEpisodes == 1 ? '' : 'Episode ${e.episode}';
  if (e.position < 10) return episode;
  final m = e.position ~/ 60, s = e.position % 60;
  final at = 'from $m:${s.toString().padLeft(2, '0')}';
  return episode.isEmpty ? 'Resume $at' : '$episode · $at';
}
