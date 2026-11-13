import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:lucide_icons_flutter/lucide_icons.dart';
import 'package:otakase/ui/components.dart';

// Parts of the anime page: the synopsis opens with More, the watched stamp
// and icon-only buttons have spoken names.
void main() {
  Widget host(Widget child) => MaterialApp(
    home: Scaffold(
      body: SingleChildScrollView(child: SizedBox(width: 300, child: child)),
    ),
  );

  testWidgets('long synopsis opens with More and closes with Less', (
    tester,
  ) async {
    await tester.pumpWidget(host(Synopsis('word ' * 200, lines: 2)));
    expect(find.text('More'), findsOneWidget);
    final short = tester.getSize(find.byKey(const Key('synopsis'))).height;
    await tester.tap(find.text('More'));
    await tester.pumpAndSettle();
    expect(find.text('Less'), findsOneWidget);
    expect(
      tester.getSize(find.byKey(const Key('synopsis'))).height,
      greaterThan(short * 3),
    );
  });

  testWidgets('short synopsis has no More', (tester) async {
    await tester.pumpWidget(host(const Synopsis('Short.')));
    expect(find.text('More'), findsNothing);
  });

  testWidgets('stamp and icon-only button are named', (tester) async {
    await tester.pumpWidget(
      host(
        Row(
          children: [
            const WatchedStamp(),
            AppButton(
              label: 'Download episode 3',
              icon: LucideIcons.download,
              iconOnly: true,
              onPressed: () {},
            ),
          ],
        ),
      ),
    );
    expect(find.bySemanticsLabel('Watched'), findsOneWidget);
    expect(find.bySemanticsLabel('Download episode 3'), findsOneWidget);
    expect(find.text('Download episode 3'), findsNothing);
  });
}
