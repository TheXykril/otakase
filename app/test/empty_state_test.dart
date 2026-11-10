import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/ui/components.dart';

void main() {
  testWidgets('empty state sits in the middle of a wide page', (tester) async {
    tester.view.physicalSize = const Size(1600, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              EmptyState(
                message: 'Nothing downloaded yet. Use Download on a show, film or episode to watch it offline.',
              ),
            ],
          ),
        ),
      ),
    );
    final glyph = tester.getCenter(find.text('空'));
    final tagline = tester.getCenter(find.text('Leave it to us.'));
    expect(glyph.dx, closeTo(800, 1));
    expect(tagline.dx, closeTo(800, 1));
    final message = tester.getSize(
      find.textContaining('Nothing downloaded yet.'),
    );
    expect(message.width, lessThanOrEqualTo(560));
  });
}
