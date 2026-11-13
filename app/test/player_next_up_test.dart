import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/player/next_up.dart';

void main() {
  testWidgets('counts down from ten and plays next once', (tester) async {
    final next = NextUp();
    var played = 0;
    expect(next.offer(prompt: false, done: () => played++), isTrue);
    expect(next.seconds.value, 10);
    expect(next.offer(prompt: false, done: () => played++), isFalse);
    await tester.pump(const Duration(seconds: 4));
    expect(next.seconds.value, 6);
    await tester.pump(const Duration(seconds: 10));
    expect(next.seconds.value, 0);
    expect(played, 1);
  });

  testWidgets('a prompt waits for the viewer', (tester) async {
    final next = NextUp();
    var played = 0;
    next.offer(prompt: true, done: () => played++);
    await tester.pump(const Duration(seconds: 30));
    expect(next.seconds.value, 10);
    expect(played, 0);
  });

  testWidgets('closing the card keeps it away until a new episode', (
    tester,
  ) async {
    final next = NextUp();
    var played = 0;
    next.offer(prompt: false, done: () => played++);
    await tester.pump(const Duration(seconds: 3));
    next.cancel();
    await tester.pump(const Duration(seconds: 20));
    expect(played, 0);
    expect(next.showing, isFalse);
    expect(next.offer(prompt: false, done: () => played++), isFalse);
    next.reset();
    expect(next.offer(prompt: false, done: () => played++), isTrue);
    next.hide();
  });

  testWidgets('stop holds the count, hide takes the card away', (tester) async {
    final next = NextUp();
    next.offer(prompt: false, done: () {});
    await tester.pump(const Duration(seconds: 2));
    next.stop();
    await tester.pump(const Duration(seconds: 5));
    expect(next.seconds.value, 8);
    next.hide();
    expect(next.showing, isFalse);
  });
}
