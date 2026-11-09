import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import 'discord.dart';
import 'models.dart';
import 'movies.dart';
import 'store.dart';
import '../host.dart';

// The CLI's other Community rows, now in the app: the server club, the
// settings backup, the cheat code hunt and Report a problem. All talk to the
// Discord bot (bot/watchclub.go, appbackup.go, cheathunt.go, appreport.go).

/// The server club: a show and an episode to reach by a deadline.
class ServerClub {
  ServerClub.fromJson(Json j)
    : anilistId = number(j['anilist_id']),
      title = j['title'] as String? ?? '',
      episode = number(j['episode']),
      deadline = DateTime.tryParse(j['deadline'] as String? ?? '')?.toLocal(),
      caughtUp = number(j['caught_up']),
      joined = number(j['joined']),
      url = j['url'] as String? ?? '';
  final int anilistId, episode, caughtUp, joined;
  final String title, url;
  final DateTime? deadline;
  Json toJson() => {
    'anilist_id': anilistId,
    'title': title,
    'episode': episode,
    'deadline': deadline?.toUtc().toIso8601String(),
    'caught_up': caughtUp,
    'joined': joined,
    'url': url,
  };
}

/// Options that never leave the device in a backup: anything private, and
/// the AdultContent switches (they follow the AniList setting here).
final _privateKey = RegExp(
  r'(secret|password|passwd|token|apikey|api_key|clientid|client_id)',
  caseSensitive: false,
);
const _localKeys = {'StoragePath', 'AdultContent', 'ContinueWatchingAdult'};
bool backupKeyKept(String key) =>
    key.isNotEmpty && !_privateKey.hasMatch(key) && !_localKeys.contains(key);

/// The settings as a backup carries them: `Key=value` lines, like the CLI's
/// config file, so a CLI backup restores here and the other way round.
String backupText(AppStore store) {
  final lines = <String>[];
  for (final key in settingDefaults.keys) {
    if (!backupKeyKept(key)) continue;
    final v = store.value(key);
    lines.add('$key=${v is List ? v.join(',') : v}');
  }
  return lines.join('\n');
}

/// Reads a backup's value for [key] in the type its default has.
dynamic backupValue(String key, String raw) {
  final d = settingDefaults[key];
  final v = raw.trim();
  if (d is bool) {
    final low = v.toLowerCase();
    if (low == 'true' || low == 'false') return low == 'true';
    return null;
  }
  if (d is int) return int.tryParse(v);
  if (d is List) {
    return v
        .split(',')
        .map((s) => s.trim())
        .where((s) => s.isNotEmpty)
        .toList();
  }
  return v;
}

/// The settings a backup sets: only ones this app knows, in their types.
Map<String, dynamic> backupSettings(String text) {
  final out = <String, dynamic>{};
  for (final line in text.split('\n')) {
    final i = line.indexOf('=');
    if (i <= 0) continue;
    final key = line.substring(0, i).trim();
    if (key.startsWith('#') ||
        !backupKeyKept(key) ||
        !settingDefaults.containsKey(key)) {
      continue;
    }
    final value = backupValue(key, line.substring(i + 1));
    if (value != null) out[key] = value;
  }
  return out;
}

/// A cheat hunt step, as the bot answers it.
class CheatView {
  CheatView.fromJson(Json j)
    : correct = j['correct'] == true,
      status = j['status'] as String? ?? '',
      kind = j['kind'] as String? ?? '',
      step = number(j['step']),
      steps = number(j['steps']),
      text = j['text'] as String? ?? '',
      links = (j['links'] as List? ?? []).whereType<String>().toList(),
      where = j['where'] as String? ?? 'app',
      wrongLeft = number(j['wrong_left']),
      code = j['code'] as String? ?? '',
      redeemed = j['redeemed'] == true,
      until = DateTime.tryParse(j['until'] as String? ?? '')?.toLocal(),
      message = j['message'] as String? ?? '';
  final bool correct, redeemed;
  final String status, kind, text, where, code, message;
  final int step, steps, wrongLeft;
  final List<String> links;
  final DateTime? until;
  bool get puzzle => status == 'puzzle';
}

/// The Konami code: ↑↑↓↓←→←→BA. A TV remote has no B or A, so two presses
/// of Select stand in for them.
class KonamiTracker {
  static const code = [
    'up', 'up', 'down', 'down', 'left', 'right', 'left', 'right', 'b', 'a', //
  ];
  final _keys = <String>[];

  /// Records [key] and reports whether it finished the code.
  bool press(String key) {
    _keys.add(key.toLowerCase());
    if (_keys.length > code.length) _keys.removeAt(0);
    if (_keys.length != code.length) return false;
    final tail = _keys.sublist(8).join();
    final ok =
        listEquals(_keys.sublist(0, 8), code.sublist(0, 8)) &&
        (tail == 'ba' || tail == 'selectselect');
    if (ok) _keys.clear();
    return ok;
  }
}

/// Whether text typed in search is the code spelt out (uuddlrlrba).
bool isKonamiText(String text) {
  final t = text.toLowerCase().replaceAll(RegExp(r'\s+'), '');
  return t == 'uuddlrlrba' || t == '↑↑↓↓←→←→ba';
}

String get deviceSystem {
  if (kIsWeb) return 'web';
  try {
    return Host.name;
  } catch (_) {
    return 'unknown';
  }
}

/// What a report carries so it can be acted on: the version, the system,
/// the settings that change playback, and how the sources are doing.
/// Reports may be shared in public, so sources are numbered as in
/// Diagnostics and never named.
String reportDiagnostics(AppStore store, {String extra = ''}) {
  final off = (store.value('DisabledProviders') as List? ?? const [])
      .map((e) => sourceLabel('$e'))
      .join(', ');
  final b = StringBuffer()
    ..writeln('Otakase app $appVersion')
    ..writeln('System: $deviceSystem')
    ..writeln('Sources: ${store.text('Provider')}')
    ..writeln('Turned off: ${off.isEmpty ? 'none' : off}')
    ..writeln(
      'Sub or dub: ${store.text('SubOrDub')}, subs ${store.text('SubsLanguage')}',
    )
    ..writeln('Quality: ${store.text('Quality')}')
    ..writeln('Torrents: ${store.flag('TorrentSources') ? 'on' : 'off'}')
    ..writeln('External mpv: ${store.flag('ExternalMpv') ? 'on' : 'off'}');
  final health = store.providerHealth.entries
      .where((e) => store.adultAllowed || !adultSourceNames.contains(e.key))
      .map((e) => '${sourceLabel(e.key)} ${e.value}')
      .join(', ');
  if (health.isNotEmpty) b.writeln('Sources this run: $health');
  if (extra.trim().isNotEmpty) b.writeln(hideSourceNames(extra.trim()));
  return b.toString().trim();
}

/// Sources that carry only 18+ titles (mobilecore adultProviderNames).
const adultSourceNames = {'sukebei'};

/// What a report calls a source: its number in the order Diagnostics lists
/// them, never its name.
String sourceLabel(String key) {
  final name = key
      .replaceFirst(RegExp('^movie:'), '')
      .split(':')
      .first
      .trim()
      .toLowerCase();
  final i = sourceNames.indexOf(name);
  if (i >= 0) return 'Source ${i + 1}';
  if (adultSourceNames.contains(name)) return '18+ source';
  final m = knownMovieSources.indexWhere(
    (s) => s.name == name || s.label.toLowerCase() == name,
  );
  if (m >= 0) return 'Movie source ${m + 1}';
  return name.isEmpty ? '' : 'Other source';
}

/// Replaces every source name in [text] with its number.
String hideSourceNames(String text) {
  final names = <String, String>{
    for (final n in sourceNames) n: n,
    for (final n in adultSourceNames) n: n,
    for (final s in knownMovieSources) ...{s.name: s.name, s.label: s.name},
    // Other spellings of the same sources.
    'kickass': 'kickassanime',
  };
  final keys = names.keys.where((n) => n.isNotEmpty).toList()
    ..sort((a, b) => b.length.compareTo(a.length));
  return text.replaceAllMapped(
    RegExp(
      '(?<![a-z0-9])(?:${keys.map(RegExp.escape).join('|')})(?![a-z0-9])',
      caseSensitive: false,
    ),
    (m) => sourceLabel(
      names[keys.firstWhere((k) => k.toLowerCase() == m[0]!.toLowerCase())]!,
    ),
  );
}

/// Support needs neither web addresses (which can name a source) nor
/// signed paths, query strings or authorization values, so none of them
/// enter a report.
String redactPlaybackProblem(String value) => hideSourceNames(
  value
      .replaceAll(
        RegExp(r'https?://[^\s<>"\x27]+', caseSensitive: false),
        '[address omitted]',
      )
      .replaceAll(
        RegExp(r'Bearer\s+[^\s,;]+', caseSensitive: false),
        'Bearer [omitted]',
      )
      .replaceAll(
        RegExp(
          r'(?:token|authorization|password|secret|api[_-]?key)\s*[:=]\s*[^\s,;]+',
          caseSensitive: false,
        ),
        '[credential omitted]',
      )
      // Bare host names, as in "lookup example.to: no such host".
      .replaceAll(
        RegExp(
          r'(?<![\w@/.-])(?:[a-z0-9-]+\.)+(?!(?:mp4|mkv|ts|m3u8|ass|srt|vtt|json|html?|go|dart|log|txt)\b)[a-z]{2,24}(?![\w.-])',
          caseSensitive: false,
        ),
        '[address omitted]',
      ),
);

/// The playback source as a report shows it: "Source 2", or for a film
/// "Movie source 1 · English".
String reportSource(String provider) {
  final parts = provider.split('·').map((p) => p.trim()).toList();
  return [sourceLabel(parts.first), ...parts.skip(1)].join(' · ');
}

String playbackReportDetails({
  required String provider,
  required Duration position,
  required String problem,
}) => [
  if (provider.isNotEmpty) 'Playback source: ${reportSource(provider)}',
  'Position: ${position.inSeconds.clamp(0, 2147483647)} seconds',
  'Problem: ${redactPlaybackProblem(problem)}',
].join('\n');

/// Talks to the bot for the club, backup, cheat code and reports.
class CommunityExtras extends ChangeNotifier {
  CommunityExtras({
    required this.discord,
    required this.store,
    DateTime Function()? now,
  }) : now = now ?? DateTime.now;
  final DiscordService discord;
  final AppStore store;
  final DateTime Function() now;

  /// The club cached for a few hours, while it runs.
  ServerClub? get club {
    final c = store.value('DiscordClub');
    if (c is! Map) return null;
    final at = DateTime.tryParse(c['fetched_at'] as String? ?? '');
    final raw = c['club'];
    if (at == null ||
        raw is! Map ||
        now().difference(at) > const Duration(hours: 24)) {
      return null;
    }
    final club = ServerClub.fromJson(raw.cast<String, dynamic>());
    if (club.anilistId <= 0 ||
        club.deadline == null ||
        !now().isBefore(club.deadline!)) {
      return null;
    }
    return club;
  }

  Future<void> refreshClub() async {
    final c = store.value('DiscordClub');
    final at = c is Map
        ? DateTime.tryParse(c['fetched_at'] as String? ?? '')
        : null;
    if (at != null && now().difference(at) < const Duration(hours: 3)) return;
    try {
      final r = await discord.request('GET', '/v1/club', authenticated: false);
      await store.set('DiscordClub', {
        'club': r['club'],
        'fetched_at': now().toUtc().toIso8601String(),
      });
      notifyListeners();
    } catch (_) {
      /* The club row just stays hidden. */
    }
  }

  /// Backs up the settings; returns how many went.
  Future<int> backUp() async {
    final text = backupText(store);
    if (utf8.encode(text).length > 64 << 10) {
      throw StateError('The settings are too large to back up.');
    }
    await discord.request(
      'POST',
      '/v1/backup',
      body: {'config': text},
      timeout: const Duration(seconds: 15),
    );
    return '\n'.allMatches(text).length + 1;
  }

  /// The saved backup, or null when there is none.
  Future<({String config, DateTime? at, String fromOS})?> fetchBackup() async {
    try {
      final r = await discord.request(
        'GET',
        '/v1/backup',
        timeout: const Duration(seconds: 15),
      );
      return (
        config: r['config'] as String? ?? '',
        at: DateTime.tryParse(r['at'] as String? ?? '')?.toLocal(),
        fromOS: r['from_os'] as String? ?? '',
      );
    } on BotFailure catch (e) {
      if (e.status == 404) return null;
      rethrow;
    }
  }

  /// Lays a backup over the settings; returns how many it set.
  Future<int> restore(String config) async {
    final values = backupSettings(config);
    for (final e in values.entries) {
      await store.set(e.key, e.value);
    }
    return values.length;
  }

  Future<void> deleteBackup() => discord.request('DELETE', '/v1/backup');

  Future<CheatView> cheat() async => CheatView.fromJson(
    await discord.request(
      'POST',
      '/v1/cheat',
      body: {},
      timeout: const Duration(seconds: 30),
    ),
  );

  Future<CheatView> cheatAnswer(String answer) async => CheatView.fromJson(
    await discord.request(
      'POST',
      '/v1/cheat/answer',
      body: {'answer': answer},
      timeout: const Duration(seconds: 30),
    ),
  );

  /// Posts a report to the support forum as the linked member; returns the
  /// post's link.
  Future<String> report(String title, String description, String diag) async {
    final token = discord.linkToken;
    if (!discord.linked || token.isEmpty) {
      throw const BotFailure(401, 'Not linked');
    }
    final r = http.MultipartRequest(
      'POST',
      Uri.parse('${discord.baseUrl}/v1/report'),
    );
    r.headers.addAll({
      'Accept': 'application/json',
      'User-Agent': 'Otakase/$appVersion',
      'Authorization': 'Bearer $token',
    });
    r.fields['title'] = clipRunes(title.trim(), 100);
    r.fields['description'] = clipRunes(description.trim(), 4000);
    r.fields['diag'] = clipRunes(diag, 8000);
    final response = await http.Response.fromStream(
      await discord.client.send(r),
    ).timeout(const Duration(seconds: 60));
    Json value = {};
    try {
      value = (jsonDecode(response.body) as Map).cast<String, dynamic>();
    } catch (_) {}
    if (response.statusCode ~/ 100 != 2) {
      throw BotFailure(
        response.statusCode,
        value['error']?.toString() ?? 'Request failed',
      );
    }
    return value['url'] as String? ?? '';
  }
}
