import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:video_player_avplay/video_player.dart';

import 'ass_overlay.dart';
import 'engine.dart';
import 'subtitle_cues.dart';
import 'tizen_core.dart';

/// Samsung TV playback through the TV's AVPlay player. AVPlay draws video
/// under the Flutter layer, so [view] punches a hole for it. ASS subtitle
/// files are drawn by libass ([AssOverlay]); other subtitles and AVPlay's
/// embedded captions are shown as plain text.
class TizenEngine implements PlaybackEngine {
  final _controller = ValueNotifier<VideoPlayerController?>(null);
  final _subtitle = ValueNotifier<String>('');
  final _position = StreamController<Duration>.broadcast();
  final _duration = StreamController<Duration>.broadcast();
  final _completed = StreamController<bool>.broadcast();
  final _playing = StreamController<bool>.broadcast();
  final _error = StreamController<String>.broadcast();
  final _tracks = StreamController<void>.broadcast();
  late final _state = _TizenState(this);
  late final _streams = _TizenStreams(this);
  var _audio = <AudioTrack>[];
  var _text = <TextTrack>[];
  SubtitleCues? _cues;
  final _ass = ValueNotifier<AssOverlay?>(null);
  Timer? _assTimer;
  // Last position AVPlay reported and when, to move ASS frames smoothly
  // between its updates.
  Duration _reported = Duration.zero;
  final _sinceReport = Stopwatch();
  // Embedded subtitles come from AVPlay captions; null shows none.
  bool _embedded = false;
  double _volume = 100, _rate = 1;
  VideoPlayerValue? _last;
  int _generation = 0;
  bool _disposed = false;

  @override
  EngineState get state => _state;
  @override
  EngineStreams get stream => _streams;
  VideoPlayerValue? get _value => _controller.value?.value;

  @override
  Future<void> open(
    String url, {
    Map<String, String> headers = const {},
    bool play = true,
  }) async {
    await stop();
    final g = ++_generation;
    final controller = VideoPlayerController.network(url, httpHeaders: headers);
    _controller.value = controller;
    controller.addListener(() => _changed(controller));
    try {
      await controller.initialize();
    } catch (e) {
      if (g == _generation) _error.add('$e');
      return;
    }
    if (g != _generation || _disposed) return;
    await controller.setVolume(_volume / 100);
    if (_rate != 1) await controller.setPlaybackSpeed(_rate);
    _audio = await controller.audioTracks ?? [];
    _text = await controller.textTracks ?? [];
    if (g != _generation) return;
    // Show embedded captions unless a subtitle file was chosen meanwhile.
    _embedded = _text.isNotEmpty && _cues == null;
    _tracks.add(null);
    if (play) await controller.play();
  }

  void _changed(VideoPlayerController controller) {
    if (!identical(controller, _controller.value)) return;
    final value = controller.value, last = _last;
    _last = value;
    if (value.hasError && value.errorDescription != last?.errorDescription) {
      _error.add(value.errorDescription!);
    }
    if (value.position != last?.position) {
      _reported = value.position;
      _sinceReport
        ..reset()
        ..start();
      _position.add(value.position);
      final cues = _cues;
      if (cues != null) {
        _subtitle.value = cues.at(value.position);
      }
    }
    if (_embedded) {
      _subtitle.value = [
        for (final c in value.captions.textCaptions ?? const <TextCaption>[])
          if (c.text.isNotEmpty) c.text,
      ].join('\n');
    }
    final duration = value.duration.end;
    if (duration != last?.duration.end) _duration.add(duration);
    if (value.isPlaying != last?.isPlaying) _playing.add(value.isPlaying);
    if (value.isCompleted != last?.isCompleted) {
      _completed.add(value.isCompleted);
    }
  }

  @override
  Future<void> play() async => _controller.value?.play();
  @override
  Future<void> pause() async => _controller.value?.pause();
  @override
  Future<void> playOrPause() async => state.playing ? pause() : play();
  @override
  Future<void> seek(Duration position) async =>
      _controller.value?.seekTo(position);

  @override
  Future<void> stop() async {
    _generation++;
    final old = _controller.value;
    _controller.value = null;
    _last = null;
    _audio = [];
    _text = [];
    _clearSubtitles();
    await old?.dispose();
  }

  @override
  Future<void> setVolume(double volume) async {
    _volume = volume;
    await _controller.value?.setVolume(volume / 100);
  }

  @override
  Future<void> setRate(double rate) async {
    _rate = rate;
    await _controller.value?.setPlaybackSpeed(rate);
  }

  @override
  Future<void> setAudioTrack(EngineTrack track) async {
    for (final t in _audio) {
      if ('${t.trackId}' == track.id) {
        await _controller.value?.setTrackSelection(t);
        return;
      }
    }
  }

  void _clearSubtitles() {
    _cues = null;
    _embedded = false;
    _subtitle.value = '';
    _assTimer?.cancel();
    _assTimer = null;
    _ass.value?.dispose();
    _ass.value = null;
  }

  Duration get _assPosition {
    final value = _value;
    if (value == null || !value.isPlaying) return _reported;
    final moved = _sinceReport.elapsedMicroseconds * _rate;
    // Never run more than a second ahead of a stalled report.
    return _reported + Duration(microseconds: moved.clamp(0, 1000000).round());
  }

  Future<void> _showFile(String text, int generation) async {
    if (looksLikeAss(text)) {
      final overlay = await AssOverlay.create(
        tizenLibrary('libotakase_ass.so'),
        text,
      );
      if (generation != _generation || _ass.value != null || _cues != null) {
        overlay?.dispose();
        return;
      }
      if (overlay != null) {
        _ass.value = overlay;
        _assTimer = Timer.periodic(
          const Duration(milliseconds: 40),
          (_) => overlay.show(_assPosition),
        );
        return;
      }
    }
    if (generation == _generation) _cues = SubtitleCues.parse(text);
  }

  @override
  Future<void> setSubtitleTrack(EngineTrack track) async {
    _clearSubtitles();
    final g = _generation;
    switch (track.id) {
      case 'no':
        return;
      case 'auto':
        _embedded = _text.isNotEmpty;
      case 'data':
        await _showFile(track.data!, g);
      case 'uri':
        final response = await http
            .get(Uri.parse(track.uri!))
            .timeout(const Duration(seconds: 15));
        if (g != _generation) return;
        if (response.statusCode != 200) {
          throw StateError('Subtitle download failed');
        }
        await _showFile(
          utf8.decode(response.bodyBytes, allowMalformed: true),
          g,
        );
      default:
        for (final t in _text) {
          if ('${t.trackId}' == track.id) {
            await _controller.value?.setTrackSelection(t);
            _embedded = true;
          }
        }
    }
    final value = _value;
    if (_cues != null && value != null) {
      _subtitle.value = _cues!.at(value.position);
    }
  }

  @override
  Future<void> setProperty(String name, String value) async {}

  @override
  Future<void> dispose() async {
    _disposed = true;
    await stop();
    for (final c in [
      _position,
      _duration,
      _completed,
      _playing,
      _error,
      _tracks,
    ]) {
      await c.close();
    }
  }

  @override
  Widget view() => ColoredBox(
    color: Colors.transparent,
    child: Stack(
      fit: StackFit.expand,
      children: [
        ValueListenableBuilder<VideoPlayerController?>(
          valueListenable: _controller,
          builder: (context, controller, _) {
            if (controller == null) return const SizedBox.expand();
            // The TV viewport scales the app canvas; AVPlay needs the real
            // screen rectangle.
            final scale =
                MediaQuery.devicePixelRatioOf(context) /
                View.of(context).devicePixelRatio;
            return ValueListenableBuilder<VideoPlayerValue>(
              valueListenable: controller,
              builder: (context, value, _) => Center(
                child: AspectRatio(
                  aspectRatio: value.isInitialized && value.aspectRatio > 0
                      ? value.aspectRatio
                      : 16 / 9,
                  child: Stack(
                    fit: StackFit.expand,
                    children: [
                      VideoPlayer(controller, scale: scale),
                      ValueListenableBuilder<AssOverlay?>(
                        valueListenable: _ass,
                        builder: (context, overlay, _) => overlay == null
                            ? const SizedBox.shrink()
                            : AssLayer(overlay),
                      ),
                    ],
                  ),
                ),
              ),
            );
          },
        ),
        Positioned(
          left: 48,
          right: 48,
          bottom: 40,
          child: ValueListenableBuilder<String>(
            valueListenable: _subtitle,
            builder: (context, text, _) => text.isEmpty
                ? const SizedBox.shrink()
                : Text(
                    text,
                    textAlign: TextAlign.center,
                    style: const TextStyle(
                      fontFamily: 'NotoSansJP',
                      fontSize: 34,
                      height: 1.25,
                      color: Colors.white,
                      shadows: [
                        Shadow(blurRadius: 4, color: Colors.black),
                        Shadow(offset: Offset(2, 2), color: Colors.black),
                        Shadow(offset: Offset(-2, -2), color: Colors.black),
                      ],
                    ),
                  ),
          ),
        ),
      ],
    ),
  );
}

class _TizenState implements EngineState {
  _TizenState(this.engine);
  final TizenEngine engine;
  @override
  Duration get position => engine._value?.position ?? Duration.zero;
  @override
  Duration get duration => engine._value?.duration.end ?? Duration.zero;
  @override
  bool get playing => engine._value?.isPlaying ?? false;
  @override
  bool get buffering => engine._value?.isBuffering ?? false;
  @override
  double get volume => engine._volume;
  @override
  double get rate => engine._rate;
  @override
  EngineTracks get tracks => EngineTracks(
    audio: [
      for (final t in engine._audio)
        EngineTrack(
          '${t.trackId}',
          title: t.language.isEmpty ? null : t.language,
          language: t.language,
        ),
    ],
    subtitle: [
      if (engine._text.isNotEmpty) EngineTrack.no,
      for (final t in engine._text)
        EngineTrack(
          '${t.trackId}',
          title: t.language.isEmpty ? null : t.language,
          language: t.language,
        ),
    ],
  );
}

class _TizenStreams implements EngineStreams {
  _TizenStreams(this.engine);
  final TizenEngine engine;
  @override
  Stream<Duration> get position => engine._position.stream;
  @override
  Stream<Duration> get duration => engine._duration.stream;
  @override
  Stream<bool> get completed => engine._completed.stream;
  @override
  Stream<bool> get playing => engine._playing.stream;
  @override
  Stream<String> get error => engine._error.stream;
  @override
  Stream<void> get tracks => engine._tracks.stream;
}
