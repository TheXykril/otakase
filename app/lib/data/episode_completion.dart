import 'dart:async';
import 'dart:math' as math;

import 'models.dart';

/// An EOF alone also occurs when a source stops delivering the episode.
bool reachedEpisodeEnd(Duration position, Duration duration) {
  if (position <= Duration.zero || duration <= Duration.zero) return false;
  final tolerance = math.min(5000, duration.inMilliseconds ~/ 50);
  return position.inMilliseconds >= duration.inMilliseconds - tolerance;
}

/// Whether a copy reporting [duration] is about as long as an episode that
/// usually runs [expectedSeconds] (0 when unknown). A copy under half of it is
/// cut short or a placeholder clip, and never counts as watched.
bool copyLengthPlausible(Duration duration, int expectedSeconds) =>
    expectedSeconds <= 0 || duration.inSeconds * 2 >= expectedSeconds;

/// Keeps viewing evidence separate from the playhead used to resume.
class EpisodeViewing {
  int _through = 0, _last = 0, _length = 0, _sampleAt = 0;
  int? _target, _skipEnd;
  int _manualSkipped = 0, _pendingManualSkip = 0;
  bool _tailSeek = false, _playedAfterTailSeek = false;
  final _clock = Stopwatch()..start();

  void start(Duration duration, List<int>? checkpoint) {
    final length = duration.inMilliseconds;
    if (length <= 0 || _length != 0) return;
    _length = length;
    if (checkpoint != null &&
        checkpoint.length >= 2 &&
        (checkpoint[0] - length).abs() <= 1000) {
      _through = checkpoint[1].clamp(0, length);
      if (checkpoint.length == 3) {
        _manualSkipped = checkpoint[2].clamp(0, 30000);
      }
    }
  }

  /// Whether playback was followed without big jumps up to [position], with
  /// [slack] of leeway for small gaps.
  bool watchedUpTo(
    Duration position, {
    Duration slack = const Duration(minutes: 2),
  }) =>
      _length > 0 && _through + slack.inMilliseconds >= position.inMilliseconds;

  List<int>? get checkpoint => _length == 0
      ? null
      : [_length, _through, if (_manualSkipped > 0) _manualSkipped];

  void sample(Duration position, {required bool playing, double rate = 1}) {
    if (_length == 0) return;
    final at = position.inMilliseconds;
    final now = _clock.elapsedMilliseconds;
    if (_target case final target?) {
      if ((at - target).abs() <= 1500) {
        if (_skipEnd case final end?) {
          _through = math.max(_through, end);
          _manualSkipped += _pendingManualSkip;
        }
      } else if (now - _sampleAt < 3000) {
        return;
      } else {
        _tailSeek = false;
      }
      _target = null;
      _skipEnd = null;
      _pendingManualSkip = 0;
    } else {
      final delta = at - _last;
      final allowance = ((now - _sampleAt) * rate + 1500).clamp(1500, 5000);
      if (playing && delta > 0 && delta <= allowance) {
        if (_last <= _through + 1000) _through = math.max(_through, at);
        if (_tailSeek) _playedAfterTailSeek = true;
      } else if (delta < -1000 || delta > allowance) {
        _tailSeek = false;
        _playedAfterTailSeek = false;
      }
    }
    _last = at;
    _sampleAt = now;
  }

  void seek(Duration from, Duration to, {SkipSpan? span}) {
    final start = from.inMilliseconds;
    final end = to.inMilliseconds;
    final continuedTail = _playedAfterTailSeek;
    _target = end;
    _skipEnd = null;
    _pendingManualSkip = 0;
    _sampleAt = _clock.elapsedMilliseconds;
    _tailSeek = false;
    _playedAfterTailSeek = false;
    if (_length == 0 || end <= start) return;
    if (span != null) {
      if (span.contains(start / 1000) &&
          _through + 1000 >= (span.start * 1000).round()) {
        _skipEnd = end;
      }
    } else if (start <= _through + 1000 &&
        end - start <= 15000 &&
        _manualSkipped + end - start <= 30000 &&
        end < _length) {
      // A few ordinary seek steps should not discard a full viewing session.
      _skipEnd = end;
      _pendingManualSkip = end - start;
    } else if ((start <= _through + 1000 || continuedTail) &&
        _length - start <= math.min(120000, _length ~/ 4) &&
        end < _length) {
      _tailSeek = true;
    }
  }

  void externalSeek(Duration from, Duration to) {
    if (_target == null) seek(from, to);
  }

  bool credits(Duration position, Duration duration, List<SkipSpan> spans) {
    if (duration <= Duration.zero) return false;
    final at = position.inMilliseconds;
    return spans.any((span) {
      final start = (span.start * 1000).round();
      return span.type == 'ed' &&
          start >= duration.inMilliseconds * .75 &&
          at >= start &&
          _through + 1000 >= start &&
          reachedEpisodeEnd(
            Duration(milliseconds: (span.end * 1000).round()),
            duration,
          );
    });
  }

  /// Some players emit EOF before reporting the landing position of a skip.
  bool endingSkip(Duration position, Duration duration, List<SkipSpan> spans) {
    final end = _skipEnd;
    return end != null &&
        reachedEpisodeEnd(Duration(milliseconds: end), duration) &&
        credits(
          position,
          duration,
          spans.where((span) => (span.end * 1000).round() == end).toList(),
        );
  }

  bool ended(Duration position, Duration duration, List<SkipSpan> spans) =>
      endingSkip(position, duration, spans) ||
      reachedEpisodeEnd(position, duration) &&
          (_through >=
                  duration.inMilliseconds - math.min(6000, _length ~/ 20) ||
              _playedAfterTailSeek ||
              credits(position, duration, spans));
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
