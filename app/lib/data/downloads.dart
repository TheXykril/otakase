import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

import '../core.dart';
import '../desktop/desktop_core.dart';
import 'models.dart';
import '../host.dart';

/// An episode or a film saved for watching offline.
class DownloadItem {
  DownloadItem({
    required this.id,
    required this.kind,
    required this.title,
    required this.label,
    required this.base,
    this.poster = '',
    this.path = '',
    this.state = 'queued',
    this.bytes = 0,
    this.total = 0,
    this.parts = 0,
    this.partsTotal = 0,
    this.error = '',
    this.created = 0,
    this.subtitles = const [],
    this.coreId = 0,
  });

  /// anime, movie or show.
  final String kind;
  final String id, title, label, base, poster;
  String path, state, error;
  int bytes, total, parts, partsTotal, created, coreId;

  /// Saved subtitle files beside the video, as {path, label, language}.
  List<Json> subtitles;
  bool get running => state == 'running' || state == 'queued';
  bool get done => state == 'done';
  double get progress => partsTotal > 0
      ? parts / partsTotal
      : total > 0
      ? (bytes / total).clamp(0, 1).toDouble()
      : 0;

  factory DownloadItem.fromJson(Json j) => DownloadItem(
    id: j['id'] as String? ?? '',
    kind: j['kind'] as String? ?? 'anime',
    title: j['title'] as String? ?? '',
    label: j['label'] as String? ?? '',
    base: j['base'] as String? ?? '',
    poster: j['poster'] as String? ?? '',
    path: j['path'] as String? ?? '',
    state: j['state'] as String? ?? 'failed',
    bytes: number(j['bytes']),
    total: number(j['total']),
    parts: number(j['parts']),
    partsTotal: number(j['parts_total']),
    error: j['error'] as String? ?? '',
    created: number(j['created']),
    subtitles: (j['subtitles'] as List? ?? [])
        .whereType<Map>()
        .map((m) => m.cast<String, dynamic>())
        .toList(),
  );
  Json toJson() => {
    'id': id,
    'kind': kind,
    'title': title,
    'label': label,
    'base': base,
    'poster': poster,
    'path': path,
    'state': state,
    'bytes': bytes,
    'total': total,
    'parts': parts,
    'parts_total': partsTotal,
    'error': error,
    'created': created,
    'subtitles': subtitles,
  };
}

/// A file name that every system accepts.
String safeFileName(String name) {
  final cleaned = name
      .replaceAll(RegExp(r'[<>:"/\\|?*\x00-\x1f]'), ' ')
      .replaceAll(RegExp(r'\s+'), ' ')
      .trim()
      .replaceAll(RegExp(r'[. ]+$'), '');
  final short = cleaned.length > 120
      ? cleaned.substring(0, 120).trim()
      : cleaned;
  return short.isEmpty ? 'Otakase' : short;
}

/// Saves streams to the device through the core and keeps the list of saved
/// files. The list is the device's, shared by its profiles.
class DownloadManager extends ChangeNotifier {
  DownloadManager({
    required this.core,
    required this.preferences,
    required this.client,
    Future<String> Function()? folder,
  }) : _folder = folder ?? defaultDownloadFolder {
    _read();
  }
  final CoreClient core;
  final SharedPreferences preferences;
  final http.Client client;
  final Future<String> Function() _folder;
  final List<DownloadItem> items = [];
  Timer? _poll;
  bool _disposed = false;
  static const key = 'downloads.v1';

  void _read() {
    try {
      for (final j in (jsonDecode(
        preferences.getString(key) ?? '[]',
      ) as List).whereType<Map>()) {
        final item = DownloadItem.fromJson(j.cast<String, dynamic>());
        // A download the app was closed during stops there; the part file
        // is kept, so starting it again resumes where it can.
        if (item.running) {
          item.state = 'failed';
          item.error = 'Stopped when the app closed.';
        }
        items.add(item);
      }
    } catch (_) {
      /* A damaged list starts empty; the files stay on disk. */
    }
  }

  Future<void> _persist() => preferences.setString(
    key,
    jsonEncode([for (final i in items) i.toJson()]),
  );

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _poll?.cancel();
    super.dispose();
  }

  DownloadItem? find(String id) => items.where((i) => i.id == id).firstOrNull;

  /// Starts saving [stream]. [id] names what it is, such as
  /// anime:al:21:ep3, so the same episode is not saved twice.
  Future<DownloadItem> start({
    required String id,
    required String kind,
    required String title,
    required String label,
    required ResolvedStream stream,
    String poster = '',
    int quality = 0,
  }) async {
    if (stream.torrent) {
      throw const CoreFailure(
        'unsupported',
        'Torrent copies cannot be downloaded yet. Try another copy.',
      );
    }
    final existing = find(id);
    if (existing != null && (existing.running || existing.done)) {
      return existing;
    }
    final folder = await _folder();
    final name = safeFileName(label.isEmpty ? title : '$title - $label');
    final dir = '$folder${Host.pathSeparator}${safeFileName(title)}';
    final base = '$dir${Host.pathSeparator}$name';
    final headers = Map<String, String>.of(stream.headers);
    if (stream.referrer.isNotEmpty) headers['Referer'] = stream.referrer;
    final d = await core.request({
      'operation': 'download_start',
      'url': stream.url,
      'headers': headers,
      'hls': stream.hls,
      'path': base,
      'quality': quality,
    });
    final item = DownloadItem(
      id: id,
      kind: kind,
      title: title,
      label: label,
      base: base,
      poster: poster,
      state: 'running',
      created: DateTime.now().millisecondsSinceEpoch,
      coreId: number(d['id']),
    );
    items.removeWhere((i) => i.id == id);
    items.insert(0, item);
    _notify();
    await _persist();
    unawaited(_saveSubtitles(item, stream, headers));
    _poll ??= Timer.periodic(const Duration(seconds: 1), (_) => refresh());
    return item;
  }

  /// Copies the stream's subtitles next to the video: local files as they
  /// are, web ones fetched now while their links still work.
  Future<void> _saveSubtitles(
    DownloadItem item,
    ResolvedStream stream,
    Map<String, String> headers,
  ) async {
    final saved = <Json>[];
    var n = 0;
    for (final track in stream.subtitles.take(6)) {
      final url = track['url'] as String? ?? '';
      if (url.isEmpty) continue;
      final language = track['language'] as String? ?? '';
      final ext =
          RegExp(
            r'\.(srt|vtt|ass|ssa)(\?|$)',
            caseSensitive: false,
          ).firstMatch(url)?.group(1)?.toLowerCase() ??
          'srt';
      final path =
          '${item.base}.${n++}${language.isEmpty ? '' : '.${safeFileName(language)}'}.$ext';
      try {
        if (url.startsWith('http')) {
          final response = await client
              .get(Uri.parse(url), headers: headers)
              .timeout(const Duration(seconds: 20));
          if (response.statusCode != 200) continue;
          await File(path).writeAsBytes(response.bodyBytes);
        } else {
          await File(url.replaceFirst('file://', '')).copy(path);
        }
        saved.add({
          'path': path,
          'label': track['label'] as String? ?? language,
          'language': language,
        });
      } catch (_) {
        /* The video is still worth having without this track. */
      }
    }
    item.subtitles = saved;
    await _persist();
  }

  Future<void> refresh() async {
    final running = items.where((i) => i.running && i.coreId > 0).toList();
    if (running.isEmpty) {
      _poll?.cancel();
      _poll = null;
      return;
    }
    Json d;
    try {
      d = await core.request({
        'operation': 'download_status',
      }, timeout: const Duration(seconds: 5));
    } catch (_) {
      return;
    }
    final byId = {
      for (final s in (d['downloads'] as List? ?? []).whereType<Map>())
        number(s['id']): s.cast<String, dynamic>(),
    };
    var changed = false;
    for (final item in running) {
      final s = byId[item.coreId];
      if (s == null) {
        item.state = 'failed';
        item.error = 'The download stopped.';
        changed = true;
        continue;
      }
      item
        ..bytes = number(s['bytes'])
        ..total = number(s['total'])
        ..parts = number(s['parts'])
        ..partsTotal = number(s['parts_total']);
      final state = s['state'] as String? ?? 'running';
      if (state != 'running') {
        item.state = state;
        item.error = s['error'] as String? ?? '';
        item.path = s['path'] as String? ?? '';
        changed = true;
        unawaited(
          core
              .request({'operation': 'download_forget', 'id': item.coreId})
              .then((_) {}, onError: (_) {}),
        );
      }
    }
    _notify();
    if (changed) await _persist();
  }

  Future<void> cancel(DownloadItem item) async {
    if (item.coreId > 0) {
      try {
        await core.request({'operation': 'download_cancel', 'id': item.coreId});
      } catch (_) {
        /* Removing it below still clears it from the list. */
      }
    }
    await remove(item);
  }

  /// Deletes the item and its files.
  Future<void> remove(DownloadItem item) async {
    if (item.running && item.coreId > 0) {
      try {
        await core.request({'operation': 'download_cancel', 'id': item.coreId});
      } catch (_) {}
    }
    items.remove(item);
    _notify();
    await _persist();
    final dir = Directory(File(item.base).parent.path);
    final prefix = File(item.base).uri.pathSegments.last;
    try {
      await for (final f in dir.list()) {
        final name = f.uri.pathSegments.last;
        if (f is File && name.startsWith(prefix)) await f.delete();
      }
      if (await dir.exists() && await dir.list().isEmpty) await dir.delete();
    } catch (_) {
      /* Files the system holds open go when it lets them. */
    }
  }
}

/// The folder downloads go to: the app's own Movies folder on Android, the
/// viewer's Videos folder on a desktop.
Future<String> defaultDownloadFolder() async {
  if (Host.android) {
    final dir = await CoreClient.channel.invokeMethod<String>('downloadsDir');
    if (dir == null) throw const CoreFailure('storage', 'No download folder.');
    return dir;
  }
  final home = Host.environment[Host.windows ? 'USERPROFILE' : 'HOME'] ?? '';
  final sep = Host.pathSeparator;
  if (isDesktop && home.isNotEmpty) {
    final videos = Directory('$home$sep${Host.macOS ? 'Movies' : 'Videos'}');
    return '${videos.path}${sep}Otakase';
  }
  return '${Directory.systemTemp.path}${sep}Otakase';
}
