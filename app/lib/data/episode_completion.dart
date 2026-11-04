import 'dart:async';
import 'dart:math' as math;

import 'models.dart';

/// An EOF alone also occurs when a source stops delivering the episode.
bool reachedEpisodeEnd(Duration position, Duration duration) {
  if (position <= Duration.zero || duration <= Duration.zero) return false;
  final tolerance = math.min(5000, duration.inMilliseconds ~/ 50);
  return position.inMilliseconds >= duration.inMilliseconds - tolerance;
}

bool watchedEpisode(
  Duration position,
  Duration duration,
  int percentage,
  List<SkipSpan> spans,
) {
  if (position <= Duration.zero || duration <= Duration.zero) return false;
  if (position.inMilliseconds * 100 >= duration.inMilliseconds * percentage) {
    return true;
  }
  if (percentage >= 100) return false;
  final seconds = position.inMilliseconds / 1000;
  return spans.any(
    (span) =>
        span.type == 'ed' &&
        span.start >= duration.inMilliseconds * .00075 &&
        seconds >= span.start &&
        reachedEpisodeEnd(
          Duration(milliseconds: (span.end * 1000).round()),
          duration,
        ),
  );
}

/// A one-shot timeout rearmed whenever the player advances.
class PlaybackStallWatchdog {
  Timer? _timer;

  void arm(void Function() onStall) {
    cancel();
    _timer = Timer(const Duration(seconds: 25), onStall);
  }

  void cancel() => _timer?.cancel();
}
