import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/desktop/desktop_core.dart';
import 'package:otakase/ui/components.dart';

// One picker call, three presentations: sheet on a phone, side panel on a
// TV, menu at the pointer on a computer.
void main() {
  Future<BuildContext> mount(WidgetTester tester, {bool tv = false}) async {
    late BuildContext page;
    tester.view.physicalSize = const Size(1600, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        home: AppLayout(
          isTV: tv,
          child: Builder(
            builder: (context) {
              page = context;
              return const Scaffold();
            },
          ),
        ),
      ),
    );
    return page;
  }

  Future<String?> pick(BuildContext page) =>
      chooseValue(page, 'Quality', ['1080p', '720p'], selected: '720p');

  testWidgets('phone uses a bottom sheet', (tester) async {
    final result = pick(await mount(tester));
    await tester.pumpAndSettle();
    expect(find.byType(BottomSheet), findsOneWidget);
    expect(find.byType(AlertDialog), findsNothing);
    await tester.tap(find.text('1080p'));
    await tester.pumpAndSettle();
    expect(await result, '1080p');
  });

  testWidgets('TV uses a side panel', (tester) async {
    final result = pick(await mount(tester, tv: true));
    await tester.pumpAndSettle();
    expect(find.byType(BottomSheet), findsNothing);
    expect(find.byType(AlertDialog), findsNothing);
    final panel = tester.getRect(find.text('Quality'));
    expect(panel.left, greaterThan(800));
    await tester.tap(find.text('1080p'));
    await tester.pumpAndSettle();
    expect(await result, '1080p');
  });

  testWidgets('computer uses a menu', (tester) async {
    debugDesktopOverride = true;
    addTearDown(() => debugDesktopOverride = null);
    DesktopInput.keyboardMode.value = false;
    DesktopInput.lastPointerDown = const Offset(200, 200);
    final result = pick(await mount(tester, tv: true));
    await tester.pumpAndSettle();
    expect(find.byType(PopupMenuItem<String>), findsNWidgets(2));
    expect(find.byType(AlertDialog), findsNothing);
    await tester.tap(find.text('1080p'));
    await tester.pumpAndSettle();
    expect(await result, '1080p');
  });
}
