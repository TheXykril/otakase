import 'dart:async';
import 'dart:convert';

import 'package:flutter/services.dart';

import 'data/models.dart';
import 'data/store.dart';

class CoreFailure implements Exception {
  const CoreFailure(this.code, this.message);
  final String code, message;
  @override
  String toString() => '$code: $message';
}

class RequestScope {
  bool cancelled = false;
  final Set<int> handles = {};
  void cancel() {
    cancelled = true;
    for (final id in handles.toList()) {
      unawaited(CoreClient.channel.invokeMethod<void>('cancel', id));
    }
  }

  void check() {
    if (cancelled) throw const CoreFailure('cancelled', 'Request cancelled.');
  }
}

class CoreClient {
  static const channel = MethodChannel('otakase/core');
  Future<bool> isTV() async =>
      await channel.invokeMethod<bool>('device') ?? false;
  Future<Json> request(
    Json input, {
    void Function(int)? onHandle,
    RequestScope? scope,
    Duration timeout = const Duration(seconds: 40),
  }) async {
    scope?.check();
    final id = await channel.invokeMethod<int>(
      'start',
      jsonEncode({'version': 1, ...input}),
    );
    if (id == null) {
      throw const CoreFailure('invalid_handle', 'No request handle.');
    }
    scope?.handles.add(id);
    onHandle?.call(id);
    if (scope?.cancelled == true) await cancel(id);
    try {
      final raw = await channel
          .invokeMethod<String>('result', id)
          .timeout(
            timeout,
            onTimeout: () {
              unawaited(cancel(id));
              throw const CoreFailure('timeout', 'Source request timed out.');
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

  Future<void> cancel(int id) => channel.invokeMethod<void>('cancel', id);
  Future<void> stopPlayback() async {
    await request({'operation': 'stop'});
  }
}

class ResolvedStream {
  ResolvedStream.fromJson(Json json)
    : url = json['url'] as String,
      hls = json['hls'] == true,
      headers = Map<String, String>.from(json['headers'] as Map? ?? {}),
      referrer = json['referrer'] as String? ?? '',
      subtitle = json['subtitle'] as String? ?? '',
      subtitles = (json['subtitles'] as List? ?? [])
          .whereType<Map>()
          .map((m) => m.cast<String, dynamic>())
          .toList();
  final String url, referrer, subtitle;
  final bool hls;
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
  SourceResolver(this.core, this.store);
  final CoreClient core;
  final AppStore store;
  final Map<String, String> _matches = {};
  Future<PlaybackSource> resolve(
    Anime anime,
    int episode, {
    required RequestScope scope,
    required void Function(String) status,
    String? source,
    String? mode,
  }) async {
    final enabled = store.enabledSources;
    final chosen = source ?? store.activeSource;
    final providers = [
      if (enabled.contains(chosen)) chosen,
      ...enabled.where((s) => s != chosen),
    ];
    if (providers.isEmpty) {
      throw const CoreFailure('no_sources', 'Enable a source in Settings.');
    }
    final modes = [mode ?? store.text('SubOrDub')];
    if (store.flag('AutoAudioFallback')) {
      modes.add(modes.first == 'sub' ? 'dub' : 'sub');
    }
    for (final provider
        in store.flag('AutoFallback') ? providers : providers.take(1)) {
      for (final audio in modes) {
        scope.check();
        status('Looking on $provider · ${audio.toUpperCase()}');
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
          }
          final data = await core.request({
            'operation': 'resolve',
            'provider': provider,
            'mode': audio,
            'show_id': id,
            'episode': episode,
          }, scope: scope);
          final streams = (data['streams'] as List? ?? [])
              .whereType<Map>()
              .map((m) => ResolvedStream.fromJson(m.cast<String, dynamic>()))
              .toList();
          if (streams.isEmpty) {
            throw const CoreFailure('no_stream', 'No stream for this episode.');
          }
          store.health(provider, 'Working');
          scope.check();
          return PlaybackSource(
            provider: provider,
            showId: id,
            mode: audio,
            streams: streams,
          );
        } on CoreFailure catch (e) {
          if (e.code == 'cancelled') rethrow;
          store.health(
            provider,
            e.code == 'not_found' ? 'No match' : 'Unavailable',
          );
        } catch (_) {
          scope.check();
          store.health(provider, 'Unavailable');
        }
      }
    }
    throw const CoreFailure(
      'no_stream',
      'No enabled source could play this episode. Try another audio mode or source.',
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
