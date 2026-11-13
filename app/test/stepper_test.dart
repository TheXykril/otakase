import 'package:flutter/material.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/ui/components.dart';

// The progress and score stepper: holding repeats and speeds up, the number
// can be typed, and values stay inside their range.
void main() {
  Future<ValueNotifier<int>> mount(
    WidgetTester tester, {
    int max = 1000,
  }) async {
    final value = ValueNotifier(0);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ValueListenableBuilder<int>(
            valueListenable: value,
            builder: (_, v, _) => AppStepper(
              label: 'Progress',
              value: v,
              max: max,
              text: '$v / $max',
              onChanged: (n) => value.value = n,
            ),
          ),
        ),
      ),
    );
    return value;
  }

  testWidgets('tap steps by one and holding repeats faster', (tester) async {
    final value = await mount(tester);
    await tester.tap(find.byTooltip('Increase progress'));
    await tester.pump();
    expect(value.value, 1);
    final gesture = await tester.startGesture(
      tester.getCenter(find.byTooltip('Increase progress')),
    );
    await tester.pump(kLongPressTimeout + const Duration(milliseconds: 10));
    for (var i = 0; i < 40; i++) {
      await tester.pump(const Duration(milliseconds: 90));
    }
    await gesture.up();
    await tester.pumpAndSettle();
    final held = value.value;
    // 40 ticks at one, five and ten per tick lands well past 40.
    expect(held, greaterThan(60));
    await tester.pump(const Duration(seconds: 1));
    expect(value.value, held);
  });

  testWidgets('tapping the number types a value, clamped to the range', (
    tester,
  ) async {
    final value = await mount(tester, max: 12);
    await tester.tap(find.byKey(const ValueKey('stepper-Progress')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('stepper-input')), '300');
    await tester.tap(find.text('Set'));
    await tester.pumpAndSettle();
    expect(value.value, 12);
    expect(find.text('12 / 12'), findsOneWidget);
    // At the top, + is off.
    final plus = tester.widget<AppButton>(
      find.ancestor(
        of: find.byTooltip('Increase progress'),
        matching: find.byType(AppButton),
      ),
    );
    expect(plus.onPressed, isNull);
  });
}
