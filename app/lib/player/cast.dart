import 'dart:async';

import '../cast/cast.dart';
import '../core.dart';
import '../data/external_subtitles.dart';
import '../data/models.dart';
import '../data/play_request.dart';

/// The cast device the player sends to: which one, what it last reported,
/// and the once-a-second status poll.
///
/// The player decides what to do with each status; this only talks to the
/// device and keeps track of it.
class PlayerCast {
  PlayerCast(CoreClient core, {this.awake}) : client = CastClient(core);

  final CastClient client;

  /// Keeps this device awake and on Wi-Fi while it casts, with [text]
  /// shown, or lets it sleep with null: a phone feeds the stream to the
  /// device, so a sleeping phone stalls the cast.
  final void Function(String? text)? awake;

  /// The device playing instead of this screen, or null.
  CastDevice? device;

  /// What the device last reported.
  CastStatus? status;

  /// Whether the device was playing at the last poll.
  bool playing = false;

  Timer? _timer;
  int _ended = -1;
  bool _polling = false;

  bool get active => device != null;

  /// Whether playback on the device is going: null before it answered.
  bool get paused => status?.paused == true;

  /// [to] takes over playback; nothing is sent until [send].
  void begin(CastDevice to) {
    device = to;
    awake?.call('Casting to ${to.name}');
    status = null;
    playing = false;
    _ended = -1;
  }

  /// Sends a copy to the device and starts polling it with [poll] every
  /// second. Throws [CoreFailure] when the device refuses it.
  Future<void> send({
    required ResolvedStream stream,
    required String title,
    required String subtitle,
    required double start,
    required double duration,
    required void Function() poll,
  }) async {
    final to = device;
    if (to == null) return;
    await client.start(
      device: to,
      stream: stream,
      title: title,
      subtitle: subtitle,
      start: start,
      duration: duration,
    );
    _timer?.cancel();
    _timer = Timer.periodic(const Duration(seconds: 1), (_) => poll());
  }

  /// Asks the device how playback is going. Null while another poll is
  /// still waiting for its answer or nothing is cast.
  Future<CastStatus?> read() async {
    if (!active || _polling) return null;
    _polling = true;
    try {
      final s = await client.status();
      if (active) status = s;
      return active ? s : null;
    } finally {
      _polling = false;
    }
  }

  /// Whether the device reports a load that ended, once per load.
  bool endedNow(CastStatus s) {
    if (s.state != 'ended' || _ended == s.load) return false;
    _ended = s.load;
    return true;
  }

  Future<void> control(String action, [double value = 0]) =>
      client.control(action, value);

  /// Opens this computer's firewall to the device.
  Future<bool> allow() => client.allow();

  /// Ends the cast. [remote] when the device already stopped by itself.
  Future<void> stop({bool remote = false}) async {
    _timer?.cancel();
    _timer = null;
    if (!remote) {
      try {
        await client.control('stop');
      } catch (_) {
        /* The device may already be gone. */
      }
    }
    drop();
  }

  /// Forgets the device without telling it, for one that refused the cast.
  void drop() {
    _timer?.cancel();
    _timer = null;
    device = null;
    status = null;
    awake?.call(null);
  }

  /// Stops polling and tells the device to stop, without waiting.
  void dispose() {
    _timer?.cancel();
    if (active) {
      unawaited(client.control('stop').catchError((_) {}));
      awake?.call(null);
    }
  }
}

/// The subtitle file a cast takes along: an explicit external choice,
/// else dialogue in the first preferred language. Empty for none.
String castSubtitleFor(
  ResolvedStream stream, {
  required bool isTitle,
  required String mode,
  required String languages,
  Json? externalTrack,
  bool skipEpisodeSubtitles = false,
  bool castsFile = false,
  bool subSelected = false,
  bool dubbedTitle = false,
}) {
  if (skipEpisodeSubtitles) return '';
  if (externalTrack case final Json track) {
    // A file on this device reaches the cast device only beside a
    // download, which the core serves from here.
    final url = track['url'] as String? ?? '';
    return !isTitle || url.startsWith('http') || castsFile ? url : '';
  }
  if (mode == 'dub') return '';
  // A title's embedded track picked here, or a dub, sends no file.
  if (isTitle && (subSelected || dubbedTitle)) return '';
  final files = isTitle && !castsFile
      ? stream.subtitles
            .where((t) => (t['url'] as String? ?? '').startsWith('http'))
            .toList()
      : stream.subtitles;
  Iterable<Json> ordered() => dialogueSubtitleOrder(
    files,
    language: (t) => t['language'] as String?,
    label: (t) => t['label'] as String?,
  );
  final preferences = languages
      .toLowerCase()
      .split(',')
      .map((v) => v.trim())
      .where((v) => v.isNotEmpty);
  for (final preferred in preferences) {
    if (preferred == 'none' || preferred == 'off') return '';
    for (final t in ordered()) {
      if (languageMatches(
        t['language'] as String?,
        t['label'] as String?,
        preferred,
      )) {
        return t['url'] as String? ?? '';
      }
    }
  }
  if (isTitle) return '';
  if (files.isNotEmpty) return ordered().first['url'] as String? ?? '';
  return stream.subtitle.startsWith('http') ? stream.subtitle : '';
}
