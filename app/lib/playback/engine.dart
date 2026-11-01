import 'package:flutter/widgets.dart';

import 'media_kit_engine.dart';
import 'platform.dart';
import 'tizen_engine.dart';

/// One audio or subtitle choice. `auto` and `no` are the automatic and off
/// entries; [data] holds subtitle text and [uri] a subtitle file.
@immutable
class EngineTrack {
  const EngineTrack(this.id, {this.title, this.language, this.data, this.uri});
  const EngineTrack.data(String text, {this.title, this.language})
    : id = 'data',
      data = text,
      uri = null;
  const EngineTrack.uri(String url, {this.title, this.language})
    : id = 'uri',
      data = null,
      uri = url;
  static const no = EngineTrack('no');
  final String id;
  final String? title, language, data, uri;
  @override
  bool operator ==(Object other) =>
      other is EngineTrack &&
      other.id == id &&
      other.data == data &&
      other.uri == uri;
  @override
  int get hashCode => Object.hash(id, data, uri);
}

class EngineTracks {
  const EngineTracks({this.audio = const [], this.subtitle = const []});
  final List<EngineTrack> audio, subtitle;
}

/// A snapshot of the player, read whenever the UI needs the current value.
abstract class EngineState {
  Duration get position;
  Duration get duration;
  bool get playing;
  bool get buffering;
  double get volume;
  double get rate;
  EngineTracks get tracks;
}

abstract class EngineStreams {
  Stream<Duration> get position;
  Stream<Duration> get duration;
  Stream<bool> get completed;
  Stream<bool> get playing;
  Stream<String> get error;
  Stream<void> get tracks;
}

/// The video player behind the player pages. Android uses libmpv through
/// media_kit; Samsung TVs use the TV's own AVPlay player.
abstract class PlaybackEngine {
  factory PlaybackEngine.create() => isTizen ? TizenEngine() : MediaKitEngine();
  EngineState get state;
  EngineStreams get stream;
  Future<void> open(
    String url, {
    Map<String, String> headers = const {},
    bool play = true,
  });
  Future<void> play();
  Future<void> pause();
  Future<void> playOrPause();
  Future<void> seek(Duration position);
  Future<void> stop();
  Future<void> setVolume(double volume);
  Future<void> setRate(double rate);
  Future<void> setAudioTrack(EngineTrack track);
  Future<void> setSubtitleTrack(EngineTrack track);

  /// Sets an mpv property. Engines without mpv ignore it.
  Future<void> setProperty(String name, String value);
  Future<void> dispose();

  /// The video surface, without controls.
  Widget view();
}
