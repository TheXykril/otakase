import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/ui/skip_marker.dart';

void main() {
  testWidgets('marking starts at the moment with a typical length', (
    tester,
  ) async {
    double? sentStart, sentEnd;
    var now = 61.0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SkipMarkerCard(
            type: 'op',
            position: () => now,
            duration: 1420,
            seekTo: (_) async {},
            send: (start, end) async {
              sentStart = start;
              sentEnd = end;
              return null;
            },
            onClose: () {},
            isTV: false,
          ),
        ),
      ),
    );
    expect(find.text('1:01'), findsOneWidget);
    expect(find.text('2:31'), findsOneWidget);
    // End: one second earlier, then set to the moment.
    await tester.tap(find.text('−1s').last);
    await tester.pump();
    expect(find.text('2:30'), findsOneWidget);
    now = 150.5;
    await tester.tap(find.text('Now').last);
    await tester.pump();
    await tester.tap(find.text('Send'));
    await tester.pump();
    expect(sentStart, 61);
    expect(sentEnd, 150.5);
    expect(find.text('Sent to AniSkip. Thank you!'), findsOneWidget);
    await tester.pump(const Duration(seconds: 3));
  });

  test('clock reads like a player', () {
    expect(clock(5), '0:05');
    expect(clock(3725), '1:02:05');
  });
}
