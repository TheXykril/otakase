import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/ui/components.dart';

void main() {
  testWidgets('pages stay beside a side cutout, players fill it', (
    tester,
  ) async {
    const key = Key('content');
    await tester.pumpWidget(
      const MediaQuery(
        data: MediaQueryData(
          size: Size(800, 400),
          padding: EdgeInsets.only(left: 40),
          viewPadding: EdgeInsets.only(left: 40),
        ),
        child: Directionality(
          textDirection: TextDirection.ltr,
          child: CutoutInset(child: SizedBox.expand(key: key)),
        ),
      ),
    );
    expect(tester.getTopLeft(find.byKey(key)).dx, 40);

    FullBleed.enter();
    await tester.pump();
    await tester.pump();
    expect(tester.getTopLeft(find.byKey(key)).dx, 0);

    FullBleed.leave();
    await tester.pump();
    await tester.pump();
    expect(tester.getTopLeft(find.byKey(key)).dx, 40);
    expect(FullBleed.screens.value, 0);
  });
}
