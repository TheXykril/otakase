import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/player/skips.dart';
import 'package:otakase/services.dart';

import 'widget_test.dart' as fixture;

const opening = SkipSpan('op', 60, 150);
const ending = SkipSpan('ed', 1300, 1390);

Future<PlayerSkips> skipsWith({bool autoOp = false, int delay = 0}) async {
  final AppServices services = await fixture.services();
  await services.store.set('SkipOp', autoOp);
  await services.store.set('SkipDelay', delay);
  return PlayerSkips(services)..spans = [opening, ending];
}

void main() {
  test('entering a span offers the button once when not automatic', () async {
    final skips = await skipsWith();
    expect(skips.at(10), (changed: false, cue: SkipCue.none));
    expect(skips.at(61), (changed: true, cue: SkipCue.offer));
    expect(skips.active, opening);
    expect(skips.at(70), (changed: false, cue: SkipCue.none));
    expect(skips.at(200), (changed: true, cue: SkipCue.none));
    expect(skips.active, isNull);
  });

  test('automatic skipping waits for the delay and skips once', () async {
    final skips = await skipsWith(autoOp: true, delay: 3);
    expect(skips.at(61).cue, SkipCue.none);
    expect(skips.at(63).cue, SkipCue.skip);
    expect(skips.at(64).cue, SkipCue.none);
    // Back into it later: kept, so it plays.
    skips.at(10);
    expect(skips.at(65).cue, SkipCue.none);
    skips.reset();
    expect(skips.spans, isEmpty);
    expect(skips.kept(opening), isFalse);
  });

  test('a span watched on purpose is not skipped', () async {
    final skips = await skipsWith(autoOp: true);
    skips.keep(opening);
    expect(skips.at(61).cue, SkipCue.none);
    skips.keepAll();
    expect(skips.kept(ending), isTrue);
  });

  test('voting needs an AniSkip time', () async {
    final skips = await skipsWith();
    expect(await skips.vote(up: true), 'No AniSkip time to vote on');
  });
}
