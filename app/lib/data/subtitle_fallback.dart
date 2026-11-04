import '../core.dart';
import 'external_subtitles.dart';

typedef SubtitleCopyCheck = ({bool selected, SubtitleFailure? failure});

/// Only reject an otherwise playable first copy when its preferred sidecar
/// could not be downloaded. Each alternative is opened with its own tracks.
Future<SubtitleFailure?> selectSubtitleCopy({
  required PlaybackSource initial,
  required int initialCopy,
  required bool enabled,
  required RequestScope scope,
  required Future<SubtitleCopyCheck> Function(PlaybackSource, int) openCopy,
  required Future<PlaybackSource?> Function(Set<String>) nextSource,
}) async {
  final first = await openCopy(initial, initialCopy);
  scope.check();
  final failure = first.failure;
  if (!enabled ||
      initial.mode != 'sub' ||
      first.selected ||
      failure?.host == null) {
    return null;
  }

  final tried = {initial.provider};
  var source = initial;
  var copy = initialCopy + 1;
  while (true) {
    scope.check();
    for (; copy < source.streams.length; copy++) {
      try {
        final candidate = await openCopy(source, copy);
        scope.check();
        if (candidate.selected) return null;
      } on Exception catch (error) {
        if (error is CoreFailure && error.code == 'cancelled') rethrow;
        scope.check();
      }
    }
    final next = await nextSource(tried);
    scope.check();
    if (next == null || !tried.add(next.provider)) return failure;
    source = next;
    copy = 0;
  }
}
