import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/ui/components.dart';

import 'tv_focus_test.dart' show key, navigation;
import 'tv_navigation_audit_test.dart' show focusedWithin, launchAudit, moreRow;
import 'widget_test.dart' as fixture;

void main() {
  for (final display in [
    (name: '720p', pixels: const Size(1280, 720), density: 1.0),
    (name: '1080p', pixels: const Size(1920, 1080), density: 1.5),
    (name: '4K default density', pixels: const Size(3840, 2160), density: 3.0),
    (name: '4K high density', pixels: const Size(3840, 2160), density: 4.0),
    (name: '4K low density', pixels: const Size(3840, 2160), density: 2.0),
    (name: '4K native pixels', pixels: const Size(3840, 2160), density: 1.0),
  ]) {
    testWidgets(
      'TV ${display.name} keeps compact layout and remote navigation',
      (tester) async {
        await launchAudit(tester);
        tester.view.physicalSize = display.pixels;
        tester.view.devicePixelRatio = display.density;
        await tester.pumpAndSettle();
        final logicalScreen = display.pixels / display.density;
        final nav = find.byKey(const ValueKey('nav-Home'));
        expect(MediaQuery.sizeOf(tester.element(nav)), const Size(1600, 900));
        final homeRect = tester.getRect(nav);
        expect(homeRect.left, greaterThanOrEqualTo(0));
        expect(homeRect.right, lessThan(logicalScreen.width));
        expect(homeRect.bottom / logicalScreen.height, lessThan(.15));

        await key(tester, LogicalKeyboardKey.arrowRight);
        await key(tester, LogicalKeyboardKey.arrowDown);
        expect(focusedWithin(find.byKey(const Key('catalog-search'))), isTrue);
        expect(tester.testTextInput.isVisible, isFalse);
        final grid = tester.widget<GridView>(
          find
              .descendant(
                of: find.byType(FocusTraversalGroup),
                matching: find.byType(GridView),
              )
              .first,
        );
        expect(
          (grid.gridDelegate as SliverGridDelegateWithFixedCrossAxisCount)
              .crossAxisCount,
          8,
        );
        await key(tester, LogicalKeyboardKey.arrowUp);
        expect(navigation(tester, 'Search').hasPrimaryFocus, isTrue);
        await key(tester, LogicalKeyboardKey.arrowRight);
        await key(tester, LogicalKeyboardKey.arrowDown);
        expect(
          focusedWithin(find.byKey(const ValueKey('more-Accounts'))),
          isTrue,
        );
        await tester.pumpWidget(const SizedBox());
      },
    );
  }

  testWidgets('TV scaled settings accept taps and restore remote focus', (
    tester,
  ) async {
    await launchAudit(tester);
    tester.view.physicalSize = const Size(3840, 2160);
    tester.view.devicePixelRatio = 4;
    await tester.pumpAndSettle();
    await moreRow(tester, 'Appearance');
    final back = find.byTooltip('Back');
    final rect = tester.getRect(back);
    expect(rect.left, greaterThanOrEqualTo(0));
    expect(rect.right, lessThanOrEqualTo(960));
    await tester.tap(back);
    await tester.pumpAndSettle();
    expect(find.byTooltip('Back'), findsNothing);
    expect(
      focusedWithin(find.byKey(const ValueKey('more-Appearance'))),
      isTrue,
    );
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV scaling keeps system text size and all inset edges', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(3840, 2160);
    tester.view.devicePixelRatio = 4;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    const source = MediaQueryData(
      size: Size(960, 540),
      devicePixelRatio: 4,
      textScaler: TextScaler.linear(1.5),
      padding: EdgeInsets.fromLTRB(12, 18, 8, 0),
      viewPadding: EdgeInsets.fromLTRB(12, 18, 8, 12),
      viewInsets: EdgeInsets.only(bottom: 180),
      systemGestureInsets: EdgeInsets.all(6),
    );
    late MediaQueryData canvas;
    await tester.pumpWidget(
      MediaQuery(
        data: source,
        child: TVViewport(
          child: Builder(
            builder: (context) {
              canvas = MediaQuery.of(context);
              return const SizedBox.expand();
            },
          ),
        ),
      ),
    );
    expect(canvas.size, const Size(1600, 900));
    expect(canvas.textScaler.scale(20), 30);
    const factor = .6;
    expect(canvas.devicePixelRatio, closeTo(2.4, .0001));
    expect(canvas.padding * factor, source.padding);
    expect(canvas.viewPadding * factor, source.viewPadding);
    expect(canvas.viewInsets * factor, source.viewInsets);
    expect(canvas.systemGestureInsets * factor, source.systemGestureInsets);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('phone retains its layout size and text scale', (tester) async {
    await fixture.launch(tester);
    expect(find.byType(TVViewport), findsNothing);
    final context = tester.element(find.byKey(const ValueKey('nav-Home')));
    expect(MediaQuery.sizeOf(context), const Size(412, 915));
    expect(MediaQuery.textScalerOf(context).scale(16), 16);
    await tester.pumpWidget(const SizedBox());
  });
}
