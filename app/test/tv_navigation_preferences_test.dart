import 'dart:ui' show PointerDeviceKind;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/ui/components.dart';

import 'tv_focus_test.dart' show key, navigation;
import 'tv_navigation_audit_test.dart' show focusedWithin, launchAudit;
import 'widget_test.dart' as fixture;

bool inResults() =>
    FocusManager.instance.primaryFocus?.context
        ?.findAncestorWidgetOfExactType<PosterCard>() !=
    null;

void main() {
  testWidgets('TV header focus switches pages without OK and stays in header', (
    tester,
  ) async {
    final services = await launchAudit(tester);
    await key(tester, LogicalKeyboardKey.arrowRight);
    expect(find.byKey(const Key('catalog-search')), findsOneWidget);
    expect(services.root, 1);
    expect(navigation(tester, 'Search').hasPrimaryFocus, isTrue);
    expect(tester.testTextInput.isVisible, isFalse);
    await key(tester, LogicalKeyboardKey.arrowRight);
    expect(find.byKey(const ValueKey('more-Accounts')), findsOneWidget);
    expect(services.root, 2);
    expect(navigation(tester, 'More').hasPrimaryFocus, isTrue);
    await key(tester, LogicalKeyboardKey.arrowRight);
    expect(navigation(tester, 'More').hasPrimaryFocus, isTrue);
    await key(tester, LogicalKeyboardKey.arrowLeft);
    expect(find.byKey(const Key('catalog-search')), findsOneWidget);
    await key(tester, LogicalKeyboardKey.arrowLeft);
    expect(find.text('Find your next story'), findsOneWidget);
    expect(services.root, 0);
    expect(navigation(tester, 'Home').hasPrimaryFocus, isTrue);
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(navigation(tester, 'Home').hasFocus, isFalse);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV pointer hover switches root without a click', (tester) async {
    await launchAudit(tester);
    final mouse = await tester.createGesture(kind: PointerDeviceKind.mouse);
    await mouse.addPointer(location: Offset.zero);
    addTearDown(mouse.removePointer);
    await mouse.moveTo(
      tester.getCenter(find.byKey(const ValueKey('nav-Search'))),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('catalog-search')), findsOneWidget);
    expect(navigation(tester, 'Search').hasPrimaryFocus, isTrue);
    expect(tester.testTextInput.isVisible, isFalse);
    await mouse.moveTo(
      tester.getCenter(find.byKey(const ValueKey('nav-More'))),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('more-Accounts')), findsOneWidget);
    expect(navigation(tester, 'More').hasPrimaryFocus, isTrue);
    await tester.pumpWidget(const SizedBox());
  });

  for (final activation in [
    LogicalKeyboardKey.select,
    LogicalKeyboardKey.enter,
    LogicalKeyboardKey.numpadEnter,
  ]) {
    testWidgets('TV Search opens keyboard only on ${activation.keyLabel}', (
      tester,
    ) async {
      await launchAudit(tester);
      await key(tester, LogicalKeyboardKey.arrowRight);
      await key(tester, LogicalKeyboardKey.arrowDown);
      final field = find.byKey(const Key('catalog-search'));
      expect(focusedWithin(field), isTrue);
      expect(tester.testTextInput.isVisible, isFalse);
      expect(
        tester.testTextInput.log.where((c) => c.method == 'TextInput.show'),
        isEmpty,
      );
      await key(tester, activation);
      expect(focusedWithin(field), isTrue);
      expect(tester.testTextInput.isVisible, isTrue);
      await tester.enterText(field, 'Frieren');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pumpAndSettle();
      expect(focusedWithin(field), isFalse);
      expect(inResults(), isTrue);
      expect(tester.testTextInput.isVisible, isFalse);
      await key(tester, LogicalKeyboardKey.arrowUp);
      expect(focusedWithin(field), isTrue);
      expect(tester.widget<TextField>(field).controller!.text, 'Frieren');
      expect(tester.testTextInput.isVisible, isFalse);
      await key(tester, activation);
      expect(tester.testTextInput.isVisible, isTrue);
      await tester.pumpWidget(const SizedBox());
    });
  }

  testWidgets('TV Search Down and Up browse without requesting a keyboard', (
    tester,
  ) async {
    await launchAudit(tester, voice: true);
    await key(tester, LogicalKeyboardKey.arrowRight);
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(focusedWithin(find.byKey(const Key('catalog-search'))), isTrue);
    await key(tester, LogicalKeyboardKey.arrowRight);
    expect(focusedWithin(find.byTooltip('Voice search')), isTrue);
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(inResults(), isTrue);
    expect(tester.testTextInput.isVisible, isFalse);
    expect(
      tester.testTextInput.log.where((c) => c.method == 'TextInput.show'),
      isEmpty,
    );
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(navigation(tester, 'Search').hasPrimaryFocus, isTrue);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV Search OK reopens a dismissed keyboard on the same field', (
    tester,
  ) async {
    await launchAudit(tester);
    await key(tester, LogicalKeyboardKey.arrowRight);
    await key(tester, LogicalKeyboardKey.arrowDown);
    final field = find.byKey(const Key('catalog-search'));
    await key(tester, LogicalKeyboardKey.select);
    expect(tester.testTextInput.isVisible, isTrue);
    tester.testTextInput.hide();
    expect(tester.testTextInput.isVisible, isFalse);
    await key(tester, LogicalKeyboardKey.select);
    expect(tester.testTextInput.isVisible, isTrue);
    expect(focusedWithin(field), isTrue);
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(inResults(), isTrue);
    expect(tester.testTextInput.isVisible, isFalse);
    await key(tester, LogicalKeyboardKey.arrowUp);
    expect(focusedWithin(field), isTrue);
    expect(tester.testTextInput.isVisible, isFalse);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('phone Search still opens the keyboard on tap', (tester) async {
    await fixture.launch(tester);
    await tester.tap(find.byKey(const ValueKey('nav-Search')));
    await tester.pumpAndSettle();
    expect(tester.testTextInput.isVisible, isTrue);
    tester.testTextInput.hide();
    await tester.tap(find.byKey(const Key('catalog-search')));
    await tester.pumpAndSettle();
    expect(tester.testTextInput.isVisible, isTrue);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('phone Search filters open from the button beside the field', (
    tester,
  ) async {
    await fixture.launch(tester);
    await tester.tap(find.byKey(const ValueKey('nav-Search')));
    await tester.pumpAndSettle();
    tester.testTextInput.hide();
    final button = find.byKey(const Key('search-filters'));
    expect(
      tester.getCenter(button).dy,
      closeTo(tester.getCenter(find.byKey(const Key('catalog-search'))).dy, 4),
    );
    expect(find.text('Season'), findsNothing);
    await tester.tap(button);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Movie'));
    await tester.tap(find.text('Fall'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byKey(const Key('filters-apply')));
    await tester.tap(find.byKey(const Key('filters-apply')));
    await tester.pumpAndSettle();
    expect(find.text('Filters · 2'), findsOneWidget);
    await tester.tap(button);
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('filters-reset')));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byKey(const Key('filters-apply')));
    await tester.tap(find.byKey(const Key('filters-apply')));
    await tester.pumpAndSettle();
    expect(find.text('Filters'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });
}
