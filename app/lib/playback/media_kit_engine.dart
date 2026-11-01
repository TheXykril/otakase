import 'package:flutter/widgets.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

import 'engine.dart';

class MediaKitEngine implements PlaybackEngine {
  MediaKitEngine() {
    video = VideoController(
      player,
      configuration: const VideoControllerConfiguration(
        hwdec: 'mediacodec',
        enableHardwareAcceleration: true,
      ),
    );
  }
  final player = Player(
    configuration: const PlayerConfiguration(
      libass: true,
      libassAndroidFont: 'assets/fonts/NotoSansJP-Regular.otf',
      libassAndroidFontName: 'Noto Sans JP',
    ),
  );
  late final VideoController video;
  @override
  late final EngineState state = _MediaKitState(player);
  @override
  late final EngineStreams stream = _MediaKitStreams(player);

  @override
  Future<void> open(
    String url, {
    Map<String, String> headers = const {},
    bool play = true,
  }) => player.open(Media(url, httpHeaders: headers), play: play);
  @override
  Future<void> play() => player.play();
  @override
  Future<void> pause() => player.pause();
  @override
  Future<void> playOrPause() => player.playOrPause();
  @override
  Future<void> seek(Duration position) => player.seek(position);
  @override
  Future<void> stop() => player.stop();
  @override
  Future<void> setVolume(double volume) => player.setVolume(volume);
  @override
  Future<void> setRate(double rate) => player.setRate(rate);
  @override
  Future<void> setAudioTrack(EngineTrack track) =>
      player.setAudioTrack(switch (track.id) {
        'auto' => AudioTrack.auto(),
        'no' => AudioTrack.no(),
        _ => AudioTrack(track.id, track.title, track.language),
      });
  @override
  Future<void> setSubtitleTrack(EngineTrack track) => player.setSubtitleTrack(
    switch (track.id) {
      'auto' => SubtitleTrack.auto(),
      'no' => SubtitleTrack.no(),
      // libass receives the original text, including ASS positions and styles.
      'data' => SubtitleTrack.data(
        track.data!,
        title: track.title,
        language: track.language,
      ),
      'uri' => SubtitleTrack.uri(
        track.uri!,
        title: track.title,
        language: track.language,
      ),
      _ => SubtitleTrack(track.id, track.title, track.language),
    },
  );
  @override
  Future<void> setProperty(String name, String value) async {
    final native = player.platform;
    if (native is NativePlayer) await native.setProperty(name, value);
  }

  @override
  Future<void> dispose() => player.dispose();
  @override
  Widget view() => Video(controller: video, controls: NoVideoControls);
}

class _MediaKitState implements EngineState {
  _MediaKitState(this.player);
  final Player player;
  @override
  Duration get position => player.state.position;
  @override
  Duration get duration => player.state.duration;
  @override
  bool get playing => player.state.playing;
  @override
  bool get buffering => player.state.buffering;
  @override
  double get volume => player.state.volume;
  @override
  double get rate => player.state.rate;
  @override
  EngineTracks get tracks => EngineTracks(
    audio: [
      for (final t in player.state.tracks.audio)
        EngineTrack(t.id, title: t.title, language: t.language),
    ],
    subtitle: [
      for (final t in player.state.tracks.subtitle)
        EngineTrack(t.id, title: t.title, language: t.language),
    ],
  );
}

class _MediaKitStreams implements EngineStreams {
  _MediaKitStreams(this.player);
  final Player player;
  @override
  Stream<Duration> get position => player.stream.position;
  @override
  Stream<Duration> get duration => player.stream.duration;
  @override
  Stream<bool> get completed => player.stream.completed;
  @override
  Stream<bool> get playing => player.stream.playing;
  @override
  Stream<String> get error => player.stream.error;
  @override
  Stream<void> get tracks => player.stream.tracks;
}
