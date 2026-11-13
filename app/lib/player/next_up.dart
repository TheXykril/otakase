import 'dart:async';

import 'package:flutter/foundation.dart';

import '../data/play_request.dart';

/// The "up next" countdown at the end of an episode or title.
///
/// [seconds] ticks on its own, so only the up-next card redraws each
/// second, not the whole player.
class NextUp {
  /// Seconds left before the next one plays; 0 when no card shows.
  final seconds = ValueNotifier(0);

  /// The film or series episode offered next, when a request named one.
  PlayRequest? title;

  /// The viewer closed the card: it stays away for this episode.
  bool cancelled = false;

  Timer? _timer;

  bool get showing => seconds.value > 0;

  /// Shows the card for ten seconds. With [prompt] it waits for the
  /// viewer; otherwise [done] runs when the count reaches 0. False when
  /// the card is already up or was closed.
  bool offer({required bool prompt, required void Function() done}) {
    if (cancelled || showing) return false;
    seconds.value = 10;
    if (prompt) return true;
    _timer?.cancel();
    _timer = Timer.periodic(const Duration(seconds: 1), (timer) {
      seconds.value--;
      if (seconds.value <= 0) {
        timer.cancel();
        done();
      }
    });
    return true;
  }

  /// Stops counting; the card stays as it is.
  void stop() => _timer?.cancel();

  /// Stops counting and takes the card away.
  void hide() {
    stop();
    seconds.value = 0;
  }

  /// The viewer closed the card.
  void cancel() {
    hide();
    cancelled = true;
    title = null;
  }

  /// A new episode or title starts: it may be offered a next one again.
  void reset() {
    cancelled = false;
    title = null;
  }
}
