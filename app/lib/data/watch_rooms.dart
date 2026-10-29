import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';

import 'discord.dart';
import 'models.dart';
import 'store.dart';

double decimal(dynamic v) => v is num ? v.toDouble() : 0;

Json? roomSubtitleTrack(List<Json> tracks, String preferences) {
  const aliases = {
    'english': ['en', 'eng', 'english'],
    'japanese': ['ja', 'jpn', 'japanese'],
    'chinese': ['zh', 'chi', 'zho', 'chinese'],
    'russian': ['ru', 'rus', 'russian'],
    'spanish': ['es', 'spa', 'spanish'],
    'french': ['fr', 'fra', 'fre', 'french'],
    'german': ['de', 'deu', 'ger', 'german'],
  };
  for (final language
      in preferences
          .toLowerCase()
          .split(',')
          .map((v) => v.trim())
          .where((v) => v.isNotEmpty)) {
    if (language == 'none' || language == 'off') return null;
    final names = aliases[language] ?? [language];
    final match = tracks
        .where(
          (t) => names.any(
            (name) =>
                (t['lang'] as String? ?? '').toLowerCase() == name ||
                name.length > 2 &&
                    (t['label'] as String? ?? '').toLowerCase().contains(name),
          ),
        )
        .firstOrNull;
    if (match != null) return match;
  }
  return tracks.firstOrNull;
}

class RoomState {
  const RoomState(this.json, this.receivedAt);
  final Json json;
  final DateTime receivedAt;
  String get id => json['room'] as String? ?? '';
  String get phase => json['phase'] as String? ?? 'over';
  String get media => json['media'] as String? ?? '';
  int get episode => number(json['ep']);
  int get version => number(json['ver']);
  int get hold => number(json['hold']);
  bool get host => json['is_host'] == true;
  bool get playing => json['playing'] == true;
  bool get adult =>
      json['adult'] == true || (json['show'] as Map?)?['adult'] == true;
  double get position => decimal(json['pos']);
  double get duration => decimal(json['dur']);
  Anime? get anime {
    final s = json['show'];
    if (s is! Map) return null;
    return Anime(
      id: number(s['id']),
      malId: number(s['mal_id']),
      title: s['title'] as String? ?? 'Watch room',
      cover: s['cover'] as String? ?? '',
      episodes: number(s['episodes']),
      isAdult: adult,
    );
  }

  double target(DateTime now) {
    final target = max(
      0.0,
      position +
          (playing
              ? max(0, now.difference(receivedAt).inMilliseconds) / 1000
              : 0),
    );
    return duration > 0 ? min(target, duration) : target;
  }

  bool jumpedFrom(RoomState previous) =>
      phase == 'play' &&
      previous.phase == 'play' &&
      ((position - previous.target(receivedAt)).abs() > 3 ||
          playing != previous.playing);
}

class RoomDrift {
  const RoomDrift({this.seek, this.rate = 1, this.behind = 0});
  final double? seek;
  final double rate, behind;
}

RoomDrift followRoom({
  required double position,
  required double target,
  required bool playing,
  bool force = false,
  bool jump = false,
  bool hold = false,
  bool stalled = false,
}) {
  final off = target - position;
  if (off < -2 ||
      (force || jump || !playing) && off.abs() > 2 ||
      hold && off.abs() > 1) {
    return RoomDrift(seek: target);
  }
  if (off > 4) return RoomDrift(behind: off);
  return RoomDrift(rate: off > .6 && !stalled && playing ? 1.15 : 1);
}

class WatchRooms extends ChangeNotifier {
  WatchRooms({
    required this.discord,
    required this.store,
    DateTime Function()? now,
    this.polling = true,
  }) : now = now ?? DateTime.now {
    store.addListener(_policy);
    discord.addListener(_policy);
  }
  final DiscordService discord;
  final AppStore store;
  final DateTime Function() now;
  final bool polling;
  List<Json> rooms = [];
  RoomState? state;
  String? error;
  bool busy = false, closed = false;
  int _generation = 0;
  Timer? _poll;
  Future<Json>? _leaving;
  @override
  void notifyListeners() {
    if (!closed) super.notifyListeners();
  }

  void _policy() {
    if (!discord.linked) {
      rooms = [];
    } else if (!store.adultAllowed) {
      rooms = rooms.where((r) => r['adult'] != true).toList();
    }
    if (state != null &&
        (!discord.linked || state!.adult && !store.adultAllowed)) {
      _leaveQuietly();
    }
    notifyListeners();
  }

  void _leaveQuietly() {
    unawaited(
      leave().catchError((Object e) {
        if (!closed) {
          error = e.toString();
          notifyListeners();
        }
        return <String, dynamic>{};
      }),
    );
  }

  Future<Json> api(
    String method,
    String action, {
    Json? body,
    Duration timeout = const Duration(seconds: 5),
  }) async {
    try {
      return await discord.request(
        method,
        '/activity/api/watch/$action',
        body: body,
        timeout: timeout,
        revokeOnUnauthorized: false,
      );
    } on BotFailure catch (e) {
      if (e.status == 401) {
        unawaited(discord.heartbeat(null, 0, 0));
        throw StateError(
          'Watch rooms are unavailable. Check your Discord link and retry.',
        );
      }
      rethrow;
    }
  }

  Future<void> load() async {
    if (closed) return;
    busy = true;
    error = null;
    notifyListeners();
    try {
      final r = await api('GET', 'status');
      if (closed) return;
      rooms = (r['rooms'] as List? ?? [])
          .whereType<Map>()
          .map((m) => m.cast<String, dynamic>())
          .where(
            (r) =>
                (r['id'] as String? ?? '').isNotEmpty &&
                (store.adultAllowed || r['adult'] != true),
          )
          .toList();
    } catch (e) {
      if (!closed) error = e.toString();
    } finally {
      if (!closed) {
        busy = false;
        notifyListeners();
      }
    }
  }

  Future<void> join(Json room) async {
    if (closed || !discord.linked) {
      throw StateError('Link Discord before joining a room.');
    }
    if (room['adult'] == true && !store.adultAllowed) {
      throw StateError('This room is unavailable.');
    }
    final id = room['id'] as String? ?? '';
    if (id.isEmpty) throw StateError('This room is private.');
    if (state != null) await leave();
    final generation = ++_generation;
    final r = await api(
      'POST',
      'open',
      body: {'room': id, 'channel': room['channel'] ?? ''},
    );
    if (closed || generation != _generation) return;
    _accept(r);
    _schedule(Duration.zero, generation);
  }

  bool _accept(Json json) {
    final next = RoomState(json, now());
    if (state?.id == next.id &&
        next.phase != 'over' &&
        next.version < state!.version) {
      return false;
    }
    if (next.adult && !store.adultAllowed) {
      state = next;
      _leaveQuietly();
      error = 'This room is unavailable.';
      notifyListeners();
      return false;
    }
    state = next;
    error = null;
    notifyListeners();
    return true;
  }

  void _schedule(Duration delay, int generation) {
    _poll?.cancel();
    if (!polling ||
        closed ||
        state == null ||
        state!.phase == 'over' ||
        generation != _generation) {
      return;
    }
    _poll = Timer(delay, () async {
      final current = state;
      if (current == null || closed || generation != _generation) return;
      try {
        final query = Uri(
          queryParameters: {'room': current.id, 'v': '${current.version}'},
        ).query;
        final r = await api(
          'GET',
          'state?$query',
          timeout: const Duration(seconds: 40),
        );
        if (closed || generation != _generation) return;
        _accept(r);
        _schedule(Duration.zero, generation);
      } catch (e) {
        if (closed || generation != _generation) return;
        error = e.toString();
        notifyListeners();
        if (e is BotFailure && e.status == 403 || !discord.linked) {
          _leaveQuietly();
          return;
        }
        _schedule(const Duration(seconds: 5), generation);
      }
    });
  }

  Future<void> control(String action, {double? position}) async {
    final current = state;
    if (current == null) return;
    final r = await api(
      'POST',
      current.host ? 'ctl' : 'ask',
      body: current.host
          ? {
              'room': current.id,
              'act': action,
              'pos': position ?? current.target(now()),
            }
          : {
              'room': current.id,
              'kind': action,
              if (action == 'forward') 'by': 10,
              if (action == 'seek') 'pos': position ?? current.target(now()),
            },
    );
    if (!closed && state?.id == current.id && r.containsKey('phase')) {
      _accept(r);
    }
  }

  Future<void> jumpBy(double by) async {
    final current = state;
    if (current == null) return;
    if (current.host) {
      await control(
        'seek',
        position: (current.target(now()) + by).clamp(
          0,
          current.duration > 0 ? current.duration : 21600,
        ),
      );
      return;
    }
    final mine = (current.json['asks'] as List? ?? [])
        .whereType<Map>()
        .where((a) => a['kind'] == 'forward' && a['user'] == current.json['me'])
        .firstOrNull;
    final r = await api(
      'POST',
      'ask',
      body: {
        'room': current.id,
        'kind': 'forward',
        'by': (decimal(mine?['by']) + by).clamp(-60, 60),
      },
    );
    if (!closed && state?.id == current.id && r.containsKey('phase')) {
      _accept(r);
    }
  }

  Future<void> answer(int id, bool yes) async {
    final current = state;
    if (current == null || !current.host) return;
    final r = await api(
      'POST',
      'answer',
      body: {'room': current.id, 'id': id, 'ok': yes},
    );
    if (!closed && state?.id == current.id) _accept(r);
  }

  Future<void> beat({
    required double position,
    required double duration,
    required bool playing,
    required bool buffering,
  }) async {
    final current = state;
    if (current == null || current.phase != 'play') return;
    await api(
      'POST',
      'beat',
      body: {
        'room': current.id,
        'pos': position,
        'dur': duration,
        'playing': playing,
        'buffering': buffering,
      },
    );
  }

  Future<void> ready(int hold) async {
    final current = state;
    if (current == null || hold <= 0) return;
    final r = await api(
      'POST',
      'ready',
      body: {'room': current.id, 'id': hold},
    );
    if (!closed && state?.id == current.id) _accept(r);
  }

  Future<void> sync(bool on) async {
    final current = state;
    if (current == null) return;
    final r = await api('POST', 'sync', body: {'room': current.id, 'on': on});
    if (!closed && state?.id == current.id) {
      state = RoomState({...state!.json, 'sync': r['sync']}, state!.receivedAt);
      notifyListeners();
    }
  }

  Future<void> count(int episode, bool yes) async {
    final current = state;
    if (current == null) return;
    final r = await api(
      'POST',
      'count',
      body: {'room': current.id, 'ep': episode, 'ok': yes},
    );
    if (!closed && state?.id == current.id && r.containsKey('phase')) {
      _accept(r);
    }
  }

  Uri mediaUri(String path) {
    final bot = Uri.parse(discord.baseUrl),
        uri = Uri.parse('${discord.baseUrl}/activity/').resolve(path);
    if (uri.scheme != bot.scheme ||
        uri.host != bot.host ||
        uri.port != bot.port) {
      throw const FormatException(
        'The room returned an invalid stream address.',
      );
    }
    return uri;
  }

  Future<Json> leave() {
    if (_leaving != null) return _leaving!;
    _poll?.cancel();
    _generation++;
    final current = state;
    state = null;
    notifyListeners();
    if (current == null || !discord.linked) return Future.value({});
    return _leaving = api(
      'POST',
      'leave',
      body: {'room': current.id},
    ).whenComplete(() => _leaving = null);
  }

  Future<void> close() async {
    if (closed) return;
    try {
      await leave();
    } catch (_) {
      /* The server expires absent members. */
    }
    closed = true;
    _poll?.cancel();
    store.removeListener(_policy);
    discord.removeListener(_policy);
    super.dispose();
  }
}
