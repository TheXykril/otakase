import 'dart:async';
import 'dart:convert';

import 'package:flutter/services.dart';

import 'data/models.dart';
import 'data/store.dart';
import 'desktop/desktop_core.dart';

class CoreFailure implements Exception {
  const CoreFailure(this.code, this.message);
  final String code, message;
  @override
  String toString() => '$code: $message';
}

/// What a person reads when something fails: the message alone, without
/// Dart's "Bad state:" or "Exception:" prefixes or a core error code.
String errorMessage(Object error) {
  if (error is CoreFailure) return error.message;
  var text = switch (error) {
    StateError e => e.message,
    ArgumentError e => '${e.message}',
    _ => error.toString(),
  };
  for (var again = true; again;) {
    again = false;
    for (final prefix in ['Exception: ', 'Bad state: ']) {
      if (text.startsWith(prefix)) {
        text = text.substring(prefix.length);
        again = true;
      }
    }
  }
  return text;
}

class RequestScope {
  bool cancelled = false;
  final Set<int> handles = {};
  void cancel() {
    cancelled = true;
    for (final id in handles.toList()) {
      unawaited(CoreClient.cancelHandle(id));
    }
  }

  void check() {
    if (cancelled) throw const CoreFailure('cancelled', 'Request cancelled.');
  }
}

class CoreClient {
  static const channel = MethodChannel('otakase/core');
  // Desktop has no Android host; the core runs as the otakase-core helper.
  static Future<void> cancelHandle(int id) => isDesktop
      ? DesktopCore.instance.cancel(id)
      : channel.invokeMethod<void>('cancel', id);
  Future<bool> isTV() async =>
      !isDesktop && (await channel.invokeMethod<bool>('device') ?? false);
  Future<Json> request(
    Json input, {
    void Function(int)? onHandle,
    RequestScope? scope,
    Duration timeout = const Duration(seconds: 40),
  }) async {
    scope?.check();
    final json = jsonEncode({'version': 1, ...input});
    final id = isDesktop
        ? await DesktopCore.instance.start(json)
        : await channel.invokeMethod<int>('start', json);
    if (id == null) {
      throw const CoreFailure('invalid_handle', 'No request handle.');
    }
    scope?.handles.add(id);
    onHandle?.call(id);
    if (scope?.cancelled == true) await cancel(id);
    try {
      final raw =
          await (isDesktop
                  ? DesktopCore.instance.result(id)
                  : channel.invokeMethod<String>('result', id))
              .timeout(
                timeout,
                onTimeout: () {
                  unawaited(cancel(id));
                  throw const CoreFailure(
                    'timeout',
                    'Source request timed out.',
                  );
                },
              );
      scope?.check();
      final response = (jsonDecode(raw!) as Map).cast<String, dynamic>();
      if (response['error'] case final Map error) {
        throw CoreFailure(error['code'] as String, error['message'] as String);
      }
      return (response['data'] as Map).cast<String, dynamic>();
    } finally {
      scope?.handles.remove(id);
    }
  }

  Future<void> cancel(int id) => cancelHandle(id);
  Future<void> stopPlayback() async {
    await request({'operation': 'stop'});
  }

  /// Whether the device is on a metered connection such as mobile data.
  /// Hosts that cannot tell say no.
  Future<bool> metered() async {
    try {
      return await channel.invokeMethod<bool>('metered') ?? false;
    } catch (_) {
      return false;
    }
  }

  /// Progress of the torrent being played, or null when none is.
  Future<TorrentProgress?> torrentProgress() async {
    try {
      final d = await request({
        'operation': 'torrent',
      }, timeout: const Duration(seconds: 5));
      return d['active'] == true ? TorrentProgress.fromJson(d) : null;
    } catch (_) {
      return null;
    }
  }
}

class TorrentProgress {
  TorrentProgress.fromJson(Json json)
    : completed = number(json['completed']),
      length = number(json['length']),
      peers = number(json['peers']);
  final int completed, length, peers;
}

/// Sources that download the episode from other viewers while it plays.
const torrentSources = {'nyaa', 'sukebei'};

class ResolvedStream {
  ResolvedStream.fromJson(Json json)
    : url = json['url'] as String,
      hls = json['hls'] == true,
      headers = Map<String, String>.from(json['headers'] as Map? ?? {}),
      referrer = json['referrer'] as String? ?? '',
      subtitle = json['subtitle'] as String? ?? '',
      torrent = json['torrent'] == true,
      subtitles = (json['subtitles'] as List? ?? [])
          .whereType<Map>()
          .map((m) => m.cast<String, dynamic>())
          .toList();
  final String url, referrer, subtitle;
  final bool hls, torrent;
  final Map<String, String> headers;
  final List<Json> subtitles;
}

class PlaybackSource {
  const PlaybackSource({
    required this.provider,
    required this.showId,
    required this.mode,
    required this.streams,
    this.intro = const [],
    this.outro = const [],
    this.episodes = const [],
  });
  final String provider, showId, mode;
  final List<ResolvedStream> streams;
  final List<int> intro, outro, episodes;
}

class SourceResolver {
  SourceResolver(this.core, this.store, {this.report});
  final Future<void> Function(
    String provider,
    bool ok,
    String kind,
    String error,
  )?
  report;
  final CoreClient core;
  final AppStore store;
  final Map<String, String> _matches = {};
  Future<Json>? _coreOrder;
  Future<Json> _sources() => _coreOrder ??= _loadOrder();
  Future<Json> _loadOrder() async {
    try {
      return await core.request({'operation': 'providers'});
    } catch (_) {
      _coreOrder = null;
      rethrow;
    }
  }

  /// Every general source, in the order they are tried.
  Future<List<String>> providerOrder() async =>
      ((await _sources())['providers'] as List).cast<String>();

  /// Sources that carry only 18+ titles. Empty unless the viewer's AniList
  /// allows 18+ content.
  Future<List<String>> adultOrder() async {
    if (!store.adultAllowed) return [];
    return ((await _sources())['adult'] as List? ?? []).cast<String>();
  }

  /// The sources tried for a show. An 18+ show is only carried by the 18+
  /// sources, so with 18+ allowed it goes to those alone, as in the CLI;
  /// every other show never reaches them. Torrent sources follow the
  /// viewer's settings and connection.
  Future<List<String>> sourcesFor(Anime anime) async {
    final adult = anime.isAdult ? await adultOrder() : const <String>[];
    final order = adult.isNotEmpty ? adult : await providerOrder();
    if (!order.any(torrentSources.contains)) return order;
    final allowed =
        store.flag('TorrentSources') &&
        (store.flag('TorrentOnMobileData') || !await core.metered());
    return allowed
        ? order
        : order.where((p) => !torrentSources.contains(p)).toList();
  }

  Future<PlaybackSource> resolve(
    Anime anime,
    int episode, {
    required RequestScope scope,
    required void Function(String) status,
    Set<String> exclude = const {},
    String? mode,
    String? onlyProvider,
    bool allowAudioFallback = true,
  }) async {
    final order = (await sourcesFor(anime))
        .where((p) => onlyProvider == null || p == onlyProvider)
        .toList();
    scope.check();
    final remembered = store.preferredSource(anime);
    final providers = [
      if (order.contains(remembered) && !exclude.contains(remembered))
        remembered!,
      ...order.where((p) => p != remembered && !exclude.contains(p)),
    ];
    if (providers.isEmpty) {
      throw const CoreFailure(
        'no_stream',
        'This episode is unavailable. Try another copy.',
      );
    }
    final modes = [mode ?? store.text('SubOrDub')];
    if (allowAudioFallback && store.flag('AutoAudioFallback')) {
      modes.add(modes.first == 'sub' ? 'dub' : 'sub');
    }
    for (final provider in providers) {
      for (final audio in modes) {
        scope.check();
        status('Finding a copy · ${audio.toUpperCase()}');
        var kind = 'search';
        try {
          final key = '$provider:${anime.key}:$audio';
          var id = _matches[key];
          if (id == null) {
            final match = await core.request(
              {
                'operation': 'match',
                'provider': provider,
                'mode': audio,
                'titles': anime.titles.isEmpty ? [anime.title] : anime.titles,
                'mal_id': anime.malId,
                'anilist_id': anime.id,
                'episodes': anime.episodes,
              },
              scope: scope,
              timeout: const Duration(seconds: 30),
            );
            id = (match['show'] as Map?)?['id'] as String?;
            if (id == null || id.isEmpty) {
              throw const CoreFailure('not_found', 'No matching show.');
            }
            _matches[key] = id;
            unawaited(report?.call(provider, true, kind, '') ?? Future.value());
          }
          kind = 'episode';
          final torrent = torrentSources.contains(provider);
          final data = await core.request(
            {
              'operation': 'resolve',
              'provider': provider,
              'mode': audio,
              'show_id': id,
              'episode': episode,
              if (torrent) 'quality': int.tryParse(store.text('Quality')) ?? 0,
            },
            scope: scope,
            // A torrent waits for other viewers to answer, release by
            // release; the core gives up on its own first.
            timeout: torrent
                ? const Duration(minutes: 3, seconds: 10)
                : const Duration(seconds: 40),
          );
          final streams = (data['streams'] as List? ?? [])
              .whereType<Map>()
              .map((m) => ResolvedStream.fromJson(m.cast<String, dynamic>()))
              .toList();
          if (streams.isEmpty) {
            throw const CoreFailure('no_stream', 'No stream for this episode.');
          }
          store.health(provider, 'Working');
          unawaited(report?.call(provider, true, kind, '') ?? Future.value());
          scope.check();
          return PlaybackSource(
            provider: provider,
            showId: id,
            mode: audio,
            streams: streams,
          );
        } on CoreFailure catch (e) {
          if (e.code == 'cancelled') rethrow;
          unawaited(
            report?.call(
                  provider,
                  e.code == 'not_found' || e.code == 'no_results',
                  kind,
                  e.message,
                ) ??
                Future.value(),
          );
          store.health(
            provider,
            e.code == 'not_found' ? 'No match' : 'Unavailable',
          );
        } catch (e) {
          scope.check();
          unawaited(
            report?.call(provider, false, kind, e.toString()) ?? Future.value(),
          );
          store.health(provider, 'Unavailable');
        }
      }
    }
    throw const CoreFailure(
      'no_stream',
      'This episode is unavailable. Try another copy.',
    );
  }

  Future<List<SkipSpan>> skips(
    PlaybackSource source,
    int episode,
    RequestScope scope,
  ) async {
    try {
      final data = await core.request(
        {
          'operation': 'skip',
          'provider': source.provider,
          'mode': source.mode,
          'show_id': source.showId,
          'episode': episode,
        },
        scope: scope,
        timeout: const Duration(seconds: 15),
      );
      final spans = <SkipSpan>[];
      for (final entry in {'intro': 'op', 'outro': 'ed'}.entries) {
        final v = (data[entry.key] as List? ?? []).map(number).toList();
        if (v.length == 2 && v[1] > v[0] && v[0] >= 0) {
          spans.add(SkipSpan(entry.value, v[0].toDouble(), v[1].toDouble()));
        }
      }
      return spans;
    } catch (_) {
      return [];
    }
  }

  Future<List<int>> episodes(PlaybackSource source, RequestScope scope) async {
    try {
      final d = await core.request(
        {
          'operation': 'episodes',
          'provider': source.provider,
          'mode': source.mode,
          'show_id': source.showId,
        },
        scope: scope,
        timeout: const Duration(seconds: 15),
      );
      return (d['episodes'] as List? ?? [])
          .map((v) => int.tryParse(v.toString()) ?? 0)
          .where((v) => v > 0)
          .toList()
        ..sort();
    } catch (_) {
      return [];
    }
  }
}
