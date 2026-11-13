import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/ui/catalog_pages.dart';

// The phone Schedule week: all seven days fit 412 px without scrolling and
// a tap picks that day.
void main() {
  testWidgets('seven days fit a phone and a tap picks the day', (tester) async {
    tester.view.physicalSize = const Size(412, 915);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final monday = DateTime(2026, 11, 9);
    DateTime? picked;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20),
            child: WeekStrip(
              monday: monday,
              selected: DateTime(2026, 11, 13),
              today: DateTime(2026, 11, 13),
              onChanged: (d) => picked = d,
            ),
          ),
        ),
      ),
    );
    expect(find.byType(Scrollable), findsNothing);
    for (var i = 0; i < 7; i++) {
      final cell = tester.getRect(find.byKey(ValueKey('schedule-day-$i')));
      expect(cell.left, greaterThanOrEqualTo(20));
      expect(cell.right, lessThanOrEqualTo(392));
    }
    expect(find.bySemanticsLabel('Fri 13, today'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('schedule-day-0')));
    expect(picked, monday);
    expect(tester.takeException(), isNull);
  });
}
