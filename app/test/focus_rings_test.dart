import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/ui/components.dart';

// One app-wide signal for focus rings; it fires once per change of input
// mode, and only the focused box redraws for it.
void main() {
  testWidgets('focus rings follow the input mode and only bother the focused '
      'box', (tester) async {
    FocusManager.instance.highlightStrategy = FocusHighlightStrategy.automatic;
    var builds = 0;
    final first = FocusNode(), second = FocusNode();
    addTearDown(first.dispose);
    addTearDown(second.dispose);
    await tester.pumpWidget(
      MaterialApp(
        home: Column(
          children: [
            FocusBox(
              isTV: true,
              focusNode: first,
              onPressed: () {},
              child: Builder(
                builder: (_) {
                  builds++;
                  return const SizedBox(width: 40, height: 40);
                },
              ),
            ),
            FocusBox(
              isTV: true,
              focusNode: second,
              onPressed: () {},
              child: const SizedBox(width: 40, height: 40),
            ),
          ],
        ),
      ),
    );
    var changes = 0;
    void count() => changes++;
    FocusRings.instance.addListener(count);
    addTearDown(() => FocusRings.instance.removeListener(count));
    await tester.tapAt(const Offset(500, 500));
    await tester.pump();
    expect(FocusRings.show, isFalse);
    second.requestFocus();
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.pump();
    expect(FocusRings.show, isTrue);
    expect(changes, 1);
    final before = builds;
    await tester.tapAt(const Offset(500, 500));
    await tester.pump();
    expect(FocusRings.show, isFalse);
    expect(changes, 2);
    // The unfocused box with the counting child did not rebuild.
    expect(builds, before);
  });
}
