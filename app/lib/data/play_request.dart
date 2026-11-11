import '../core.dart';
import 'episode_completion.dart';
import 'models.dart';

/// Something to play: a stream found by a source, or a downloaded file.
class PlayableMedia {
  const PlayableMedia({
    required this.url,
    this.headers = const {},
    this.subtitles = const [],
    this.audioLanguage = '',
    this.copy = '',
    this.label = '',
  });
  final String url;
  final Map<String, String> headers;

  /// Subtitle files as {url, label, language}; a url may be a local path.
  final List<Json> subtitles;

  /// The audio track to pick, as language codes such as "lt" or "en,eng".
  final String audioLanguage;

  /// Names this copy so it can be skipped when it fails, such as
  /// vidsrc:server 1. Empty for a file, which has no other copy.
  final String copy;

  /// Where it plays from, for the title line.
  final String label;
}

/// What the player shows and tells as it plays.
class PlayRequest {
  const PlayRequest({
    required this.title,
    required this.subtitle,
    required this.open,
    this.resumeAt,
    this.onProgress,
    this.onScrobble,
    this.next,
    this.length = 0,
    this.onClose,
    this.cover = '',
    this.remote,
    this.skips,
    this.previous,
    this.pick,
    this.number = 0,
    this.count = 0,
  });
  final String title, subtitle;

  /// Art for the phone remote while this plays on a TV.
  final String cover;

  /// What an Otakase TV needs to play this too, as the phone remote's
  /// start sends it; null when only this screen can play it.
  final Json? remote;

  /// The usual running time in seconds, 0 when unknown. A copy that reports
  /// far less than this is cut short and never marks the title watched.
  final int length;
  final Future<PlayableMedia> Function(
    Set<String> failed,
    RequestScope scope,
    void Function(String) status,
  )
  open;

  /// The second to start at once the length is known.
  final int Function(int duration)? resumeAt;
  final Future<void> Function(int position, int duration, bool completed)?
  onProgress;

  /// Trakt's start, pause and stop, with the percentage watched.
  final void Function(String action, double percent)? onScrobble;

  /// The episode after this one, or null at the end.
  final PlayRequest? Function()? next;

  /// Run as the player closes, after the last save.
  final Future<void> Function()? onClose;

  /// Opening, recap and credits times for a copy [length] seconds long;
  /// null when nothing knows them.
  final Future<List<SkipSpan>> Function(double length, RequestScope scope)?
  skips;

  /// The episode before this one, or null at the start.
  final PlayRequest? Function()? previous;

  /// Episode [number] of the same season, or null when there is none.
  final PlayRequest? Function(int number)? pick;

  /// This episode's number in its season and how many the season has, 0
  /// for a film or a lone file.
  final int number, count;
}

/// Whether a track's [language] code or [title] names the [preferred]
/// language, given as a name ("english") or a code ("en").
bool languageMatches(String? language, String? title, String preferred) {
  const aliases = <String, List<String>>{
    'english': ['en', 'eng', 'english'],
    'lithuanian': ['lt', 'lit', 'lithuanian', 'lietuvių'],
    'japanese': ['ja', 'jpn', 'japanese'],
    'chinese': ['zh', 'chi', 'zho', 'chinese'],
    'russian': ['ru', 'rus', 'russian'],
    'spanish': ['es', 'spa', 'spanish'],
    'french': ['fr', 'fra', 'fre', 'french'],
    'german': ['de', 'deu', 'ger', 'german'],
  };
  final p = preferred.toLowerCase().trim();
  final values = aliases[p] ?? [p];
  final lang = (language ?? '')
      .trim()
      .toLowerCase()
      .split(RegExp('[-_]'))
      .first;
  final label = (title ?? '').toLowerCase();
  return values.any((s) => lang == s || (s.length > 2 && label.contains(s)));
}

/// Whether a copy reporting [duration] is about as long as a title that
/// usually runs [expected] seconds (0 when unknown). A copy under half of
/// it is cut short.
bool copyLengthTrusted(Duration duration, int expected) =>
    copyLengthPlausible(duration, expected);

/// Whether playback at [at] is the real end of the title, not a source that
/// stopped sending.
bool filmEnded(Duration at, Duration duration, int expected) =>
    copyLengthTrusted(duration, expected) && reachedEpisodeEnd(at, duration);

/// A found copy as a source, so copy handling, tracks and casting work the
/// same for every kind of title. Subtitle files kept as a path get it as
/// their url.
PlaybackSource mediaSource(PlayableMedia m) => PlaybackSource(
  provider: m.copy,
  showId: '',
  mode: 'sub',
  streams: [
    ResolvedStream.fromJson({
      'url': m.url,
      'headers': m.headers,
      'hls': m.url.contains('.m3u8'),
      'subtitles': [
        for (final t in m.subtitles)
          {...t, 'url': (t['url'] ?? t['path'] ?? '') as String},
      ],
    }),
  ],
);
