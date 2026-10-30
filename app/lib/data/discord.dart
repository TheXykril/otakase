import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import 'auth.dart';
import 'models.dart';
import 'store.dart';

const discordBotUrl = 'https://bot.xyverion.com';
const discordInviteUrl = 'https://otakase.xyverion.com/discord/';
// The CLI version, passed in by scripts/build-apk.sh. Other builds report a
// dev build.
const appVersion = String.fromEnvironment(
  'OTAKASE_VERSION',
  defaultValue: '0.0.0-dev',
);

class BotFailure implements Exception {
  const BotFailure(this.status, this.message);
  final int status;
  final String message;
  @override
  String toString() => status == 401
      ? 'This app is no longer linked to Discord. Link it again.'
      : 'Discord bot: $message (HTTP $status)';
}

class DiscordLinkCode {
  const DiscordLinkCode(this.code, this.poll, this.expires);
  final String code, poll;
  final DateTime expires;
}

class WatchMeter {
  double? _position;
  DateTime? _at;
  double _watched = 0;
  void observe(double position, DateTime now) {
    if (_position != null && _at != null) {
      final elapsed = now.difference(_at!).inMicroseconds / 1000000;
      final moved = position - _position!;
      if (moved > 0 && elapsed > 0 && moved <= elapsed * 4 + 2) {
        _watched += min(moved, elapsed);
      }
    }
    _position = position;
    _at = now;
  }

  void restart() {
    _position = null;
    _at = null;
  }

  int take() {
    final whole = _watched.truncate();
    _watched -= whole;
    return whole;
  }
}

class _ProviderReport {
  bool sent = false, ok = false;
  int failures = 0;
  DateTime? okAt;
}

String clipRunes(String text, int max) =>
    String.fromCharCodes(text.runes.take(max));
String cleanProviderError(String text) => clipRunes(
  text
      .replaceAll(RegExp('"[^"]*"|\'[^\']*\''), '…')
      .replaceAll(
        RegExp(r'\b[a-z][a-z0-9+.-]*://\S+', caseSensitive: false),
        '<url>',
      )
      .split(RegExp(r'\s+'))
      .where((s) => s.isNotEmpty)
      .join(' '),
  160,
);

class DiscordService extends ChangeNotifier {
  DiscordService({
    required this.client,
    required this.storage,
    required this.store,
    this.baseUrl = discordBotUrl,
    DateTime Function()? now,
  }) : now = now ?? DateTime.now;
  final http.Client client;
  final TokenStore storage;
  final AppStore store;
  final String baseUrl;
  final DateTime Function() now;
  Json _link = {};
  bool closed = false, initialized = false;
  int _epoch = 0;
  int _linkAttempt = 0;
  Timer? _inboxTimer, _watchTimer;
  Future<void> _heartbeats = Future.value();
  Future<void>? _linkWrites;
  final _reports = <String, _ProviderReport>{};
  final meter = WatchMeter();
  Anime? _watching;
  int _episode = 0;
  DateTime? _observedAt;
  String? lastError;
  Future<void> Function(String action, Json args)? remote;
  void Function(String text)? notify;
  bool get linked =>
      !closed && (_link['token'] as String? ?? '').trim().isNotEmpty;
  String get name => _link['discord_name'] as String? ?? '';
  bool get revoked => _link['revoked'] == true;
  List<Json> get inbox => (store.value('DiscordInbox') as List? ?? [])
      .whereType<Map>()
      .map((m) => m.cast<String, dynamic>())
      .toList();
  @override
  void notifyListeners() {
    if (!closed) super.notifyListeners();
  }

  Future<void> initialize({bool background = true}) async {
    if (initialized || closed) return;
    initialized = true;
    try {
      _link = (jsonDecode(await storage.read('discord.link') ?? '{}') as Map)
          .cast<String, dynamic>();
    } catch (_) {
      _link = {};
    }
    if (closed) return;
    notifyListeners();
    if (background) {
      unawaited(refreshNews());
      _startLinked();
    }
  }

  void _startLinked() {
    if (!linked) return;
    unawaited(heartbeat(null, 0, 0));
    unawaited(claimPendingGrass());
    _scheduleInbox(Duration.zero);
  }

  Future<void> _saveLink(Json value) {
    _link = value;
    notifyListeners();
    return _linkWrites = (_linkWrites ?? Future<void>.value())
        .catchError((Object _) {})
        .then((_) async {
          if (!closed) {
            await storage.write(
              'discord.link',
              value.isEmpty ? null : jsonEncode(value),
            );
          }
        });
  }

  Future<Json> request(
    String method,
    String path, {
    Json? body,
    bool authenticated = true,
    bool revokeOnUnauthorized = true,
    Duration timeout = const Duration(seconds: 5),
  }) async {
    if (closed) throw StateError('Profile closed');
    final token = authenticated ? _link['token'] as String? ?? '' : '';
    if (authenticated && token.isEmpty) {
      throw const BotFailure(401, 'Not linked');
    }
    final r = http.Request(method, Uri.parse('$baseUrl$path'));
    r.headers.addAll({
      'Accept': 'application/json',
      'User-Agent': 'Otakase/$appVersion',
      if (token.isNotEmpty) 'Authorization': 'Bearer $token',
    });
    if (body != null) {
      r.headers['Content-Type'] = 'application/json';
      r.body = jsonEncode(body);
    }
    final response = await (() async => http.Response.fromStream(
      await client.send(r),
    ))().timeout(timeout);
    if (closed || authenticated && token != _link['token']) {
      throw StateError('Profile link changed');
    }
    if (response.statusCode == 401 &&
        token.isNotEmpty &&
        revokeOnUnauthorized) {
      _epoch++;
      _inboxTimer?.cancel();
      await _saveLink({'discord_name': name, 'revoked': true});
      throw const BotFailure(401, 'Link revoked');
    }
    Json value = {};
    try {
      if (response.body.trim().isNotEmpty) {
        value = (jsonDecode(response.body) as Map).cast<String, dynamic>();
      }
    } catch (_) {
      if (response.statusCode ~/ 100 == 2) {
        throw const FormatException('Discord bot: unreadable answer');
      }
    }
    if (response.statusCode ~/ 100 != 2) {
      throw BotFailure(
        response.statusCode,
        value['error']?.toString().trim() ?? 'Request failed',
      );
    }
    return {'_status': response.statusCode, ...value};
  }

  Future<DiscordLinkCode> beginLink() async {
    final r = await request(
      'POST',
      '/v1/link/start',
      body: {},
      authenticated: false,
    );
    final code = r['code'] as String? ?? '', poll = r['poll'] as String? ?? '';
    if (code.isEmpty || poll.isEmpty) {
      throw const FormatException('Discord bot: no link code in the answer');
    }
    final seconds = number(r['expires_in']);
    return DiscordLinkCode(
      code,
      poll,
      now().add(Duration(seconds: seconds > 0 ? seconds : 600)),
    );
  }

  Future<bool> pollLink(DiscordLinkCode pending) async {
    if (!now().isBefore(pending.expires)) {
      throw const BotFailure(410, 'The link code expired. Start again.');
    }
    final attempt = _linkAttempt;
    final r = await request(
      'POST',
      '/v1/link/poll',
      body: {'poll': pending.poll},
      authenticated: false,
    );
    if (attempt != _linkAttempt || closed) return false;
    if (r['_status'] == 202) return false;
    final token = (r['token'] as String? ?? '').trim();
    if (r['_status'] != 200 || token.isEmpty) return false;
    _epoch++;
    await _saveLink({
      'token': token,
      'discord_name': r['discord_name'] ?? '',
      'linked_at': now().toUtc().toIso8601String(),
    });
    _startLinked();
    return true;
  }

  void cancelLink() {
    _linkAttempt++;
  }

  Future<bool> unlink() async {
    _epoch++;
    _linkAttempt++;
    _inboxTimer?.cancel();
    var online = true;
    try {
      if (linked) await request('POST', '/v1/unlink', body: {});
    } catch (_) {
      online = false;
    }
    await _saveLink({});
    return online;
  }

  Future<void> heartbeat(Anime? anime, int episode, int seconds) {
    final epoch = _epoch;
    final token = _link['token'];
    final payload = <String, dynamic>{
      'version': appVersion,
      'dev_build': appVersion
          .split('-')
          .skip(1)
          .join('-')
          .toLowerCase()
          .startsWith('dev'),
      'os': 'android',
      'share_watching': store.flag('DiscordShareWatching'),
      'watching': store.flag('DiscordShareWatching') && anime != null
          ? {
              'anilist_id': anime.id,
              'title': anime.title,
              'episode': episode,
              'episodes': anime.episodes,
            }
          : null,
      'watched_seconds': max(0, seconds),
    };
    return _heartbeats = _heartbeats.catchError((Object _) {}).then((_) async {
      if (!linked || epoch != _epoch || token != _link['token']) return;
      try {
        final r = await request('POST', '/v1/heartbeat', body: payload);
        final renamed = r['discord_name'] as String? ?? '';
        if (renamed.isNotEmpty && renamed != name) {
          await _saveLink({..._link, 'discord_name': renamed});
        }
      } catch (e) {
        _error(e);
      }
    });
  }

  void observe(Anime anime, int episode, double position) {
    if (!linked) return;
    final first = _watching == null;
    final changed = _watching?.key != anime.key || _episode != episode;
    if (changed) meter.restart();
    _watching = anime;
    _episode = episode;
    _observedAt = now();
    meter.observe(position, now());
    if (first || changed) unawaited(heartbeat(anime, episode, meter.take()));
    _watchTimer ??= Timer.periodic(const Duration(seconds: 60), (_) {
      if (_watching == null) return;
      // A suspended app has not observed playback since its last heartbeat.
      if (_observedAt != null &&
          now().difference(_observedAt!) < const Duration(seconds: 65)) {
        unawaited(heartbeat(_watching, _episode, meter.take()));
      }
    });
  }

  Future<void> stopWatching() async {
    _watchTimer?.cancel();
    _watchTimer = null;
    if (_watching == null) return;
    _watching = null;
    meter.restart();
    await heartbeat(null, 0, meter.take());
  }

  void _scheduleInbox(Duration delay) {
    _inboxTimer?.cancel();
    if (!linked) return;
    final epoch = _epoch;
    _inboxTimer = Timer(delay, () async {
      if (!linked || epoch != _epoch) return;
      try {
        await pollInbox();
        lastError = null;
        _retry = 30;
      } catch (e) {
        _error(e);
        if (linked) {
          final wait = _retry;
          _retry = min(600, _retry * 2);
          _scheduleInbox(Duration(seconds: wait));
        }
        return;
      }
      if (linked && epoch == _epoch) _scheduleInbox(Duration.zero);
    });
  }

  int _retry = 30;
  Future<void> pollInbox() async {
    final epoch = _epoch;
    final r = await request(
      'GET',
      '/v1/inbox?wait=25',
      timeout: const Duration(seconds: 40),
    );
    if (epoch != _epoch) return;
    final items = (r['items'] as List? ?? [])
        .whereType<Map>()
        .map((m) => m.cast<String, dynamic>())
        .toList();
    if (items.isEmpty) return;
    var kept = inbox;
    for (final item in items) {
      final expiry = DateTime.tryParse(item['expires'] as String? ?? '');
      if (expiry != null && now().isAfter(expiry)) continue;
      switch (item['kind']) {
        case 'queue':
          kept.removeWhere(
            (k) =>
                k['kind'] == 'queue' && k['anilist_id'] == item['anilist_id'],
          );
          kept.insert(0, item);
        case 'notice':
          kept.insert(0, item);
        case 'setting':
          if (item['key'] == 'DevBuilds') {
            await store.set('DevBuilds', item['value'] == 'true');
          }
        case 'remote':
          final action = item['key'] as String? ?? '';
          if (const [
            'pause',
            'play',
            'skip',
            'next',
            'previous',
            'stop',
          ].contains(action)) {
            try {
              await remote?.call(action, {});
            } catch (e) {
              _error(e);
            }
          }
      }
    }
    if (closed || epoch != _epoch) return;
    await store.set('DiscordInbox', kept.take(50).toList());
    notifyListeners();
    await request(
      'POST',
      '/v1/inbox/ack',
      body: {'ids': items.map((i) => i['id'] as String? ?? '').toList()},
    );
  }

  Future<void> refreshNews() async {
    final old = store.value('DiscordNews');
    final at = old is Map
        ? DateTime.tryParse(old['fetched_at'] as String? ?? '')
        : null;
    if (at != null && now().difference(at) < const Duration(hours: 6)) return;
    try {
      final r = await request('GET', '/v1/news', authenticated: false);
      if (!closed) {
        await store.set('DiscordNews', {
          ...r,
          'fetched_at': now().toUtc().toIso8601String(),
        });
      }
    } catch (e) {
      _error(e);
    }
  }

  bool ratingWanted(Anime anime, double score) {
    if (!linked ||
        anime.isAdult ||
        anime.id <= 0 ||
        !score.isFinite ||
        score < 0 ||
        score > 10 ||
        store.text('DiscordShareRatings').trim().toLowerCase() == 'never') {
      return false;
    }
    final news = store.value('DiscordNews');
    final at = news is Map
        ? DateTime.tryParse(news['fetched_at'] as String? ?? '')
        : null;
    return !(at != null &&
        now().difference(at) < const Duration(hours: 24) &&
        news['score_sharing'] == false);
  }

  Future<String> shareRating(
    Anime anime,
    double score, {
    String text = '',
  }) async {
    if (!ratingWanted(anime, score)) {
      throw StateError('Score sharing unavailable');
    }
    final r = await request(
      'POST',
      '/v1/rating',
      body: {
        'anilist_id': anime.id,
        'score': score,
        if (text.trim().isNotEmpty) 'text': clipRunes(text.trim(), 300),
      },
    );
    return r['url'] as String? ?? '';
  }

  Future<void> reportProvider(
    String provider,
    bool ok,
    String kind,
    String error,
  ) async {
    if (!linked || !const ['search', 'episode', 'playback'].contains(kind)) {
      return;
    }
    final s = _reports.putIfAbsent(provider, _ProviderReport.new);
    if (ok &&
            s.sent &&
            s.ok &&
            s.okAt != null &&
            now().difference(s.okAt!) < const Duration(hours: 1) ||
        !ok && (s.sent && !s.ok || s.failures >= 3)) {
      return;
    }
    s.sent = true;
    s.ok = ok;
    if (ok) {
      s.okAt = now();
    } else {
      s.failures++;
    }
    try {
      await request(
        'POST',
        '/v1/provider-report',
        body: {
          'provider': provider,
          'ok': ok,
          'kind': kind,
          if (!ok && error.isNotEmpty) 'error': cleanProviderError(error),
        },
      );
    } catch (e) {
      _error(e);
    }
  }

  Future<void> claimPendingGrass({
    Duration timeout = const Duration(seconds: 5),
  }) async {
    if (!linked || store.text('TouchGrass') != 'pending') return;
    try {
      await request(
        'POST',
        '/v1/badge/touch-grass',
        body: {},
        timeout: timeout,
      );
      if (!closed) await store.set('TouchGrass', 'claimed');
    } catch (e) {
      _error(e);
    }
  }

  Future<String> exitGreeting({int Function(int)? roll}) async {
    final choose = roll ?? Random.secure().nextInt;
    final rare = choose(100) == 0;
    final greetings = rare
        ? const [
            'Achievement unlocked: touch grass.',
            'Side quest unlocked: touch grass.',
            'Plot twist: you go touch grass now.',
          ]
        : const ['Mata ne!', 'Otsukare! Rest those eyes.'];
    final greeting = greetings[choose(greetings.length)];
    if (rare && store.text('TouchGrass').isEmpty && choose(10) == 0) {
      await store.set('TouchGrass', 'pending');
      await claimPendingGrass(timeout: const Duration(seconds: 3));
      return '$greeting\nRare drop: the Touch Grass badge for the Otakase Discord.';
    }
    return greeting;
  }

  void _error(Object e) {
    if (!closed) {
      lastError = e.toString();
      notifyListeners();
    }
  }

  Future<void> close() async {
    if (closed) return;
    _epoch++;
    _inboxTimer?.cancel();
    await stopWatching();
    if (_linkWrites != null) await _linkWrites;
    closed = true;
    _watchTimer?.cancel();
    super.dispose();
  }
}
