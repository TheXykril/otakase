import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/desktop/launcher.dart';

import 'widget_test.dart' as fixture;

Future<void> pumpLauncher(WidgetTester tester, {VoidCallback? onOpen}) async {
  await fixture.launch(tester, tv: true);
  final services = await fixture.services();
  await services.store.progress(fixture.anime, 3, 400, 1440);
  await tester.pumpWidget(
    MaterialApp(
      home: LauncherPage(services: services, onOpenApp: onOpen),
    ),
  );
  await tester.pumpAndSettle();
}

Future<void> press(WidgetTester tester, LogicalKeyboardKey key) async {
  await tester.sendKeyEvent(key);
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('starts on Continue and filters by typing', (tester) async {
    await pumpLauncher(tester);
    expect(find.text('Continue: Frieren · Ep 3'), findsOneWidget);
    expect(find.text('Open full app'), findsOneWidget);

    await tester.enterText(find.byKey(const Key('launcher-input')), 'full');
    await tester.pumpAndSettle();
    expect(find.text('Open full app'), findsOneWidget);
    expect(find.text('Continue: Frieren · Ep 3'), findsNothing);
    expect(find.text('Search for “full”'), findsOneWidget);
  });

  testWidgets('search, show menu, episode menu and back', (tester) async {
    await pumpLauncher(tester);
    await tester.enterText(find.byKey(const Key('launcher-input')), 'frier');
    await tester.pumpAndSettle();
    // The Continue row matches too; the search row is last.
    await press(tester, LogicalKeyboardKey.arrowDown);
    await press(tester, LogicalKeyboardKey.enter);
    expect(find.text('Search'), findsOneWidget);
    expect(find.text('Frieren'), findsOneWidget);

    await press(tester, LogicalKeyboardKey.enter);
    expect(find.text('Play episode 1'), findsOneWidget);
    expect(find.text('Choose episode…'), findsOneWidget);

    await press(tester, LogicalKeyboardKey.arrowDown);
    await press(tester, LogicalKeyboardKey.enter);
    expect(find.text('Episode 1'), findsOneWidget);

    await tester.enterText(find.byKey(const Key('launcher-input')), '27');
    await tester.pumpAndSettle();
    expect(find.text('Episode 27'), findsWidgets);

    // Esc clears the text first, then goes back one menu at a time.
    await press(tester, LogicalKeyboardKey.escape);
    expect(find.text('Episode 1'), findsOneWidget);
    await press(tester, LogicalKeyboardKey.escape);
    expect(find.text('Play episode 1'), findsOneWidget);
    await press(tester, LogicalKeyboardKey.escape);
    await press(tester, LogicalKeyboardKey.escape);
    expect(find.text('Continue: Frieren · Ep 3'), findsOneWidget);
  });

  testWidgets('Ctrl+W and the menu row open the full app', (tester) async {
    var opened = 0;
    await pumpLauncher(tester, onOpen: () => opened++);
    await tester.sendKeyDownEvent(LogicalKeyboardKey.controlLeft);
    await tester.sendKeyEvent(LogicalKeyboardKey.keyW);
    await tester.sendKeyUpEvent(LogicalKeyboardKey.controlLeft);
    await tester.pumpAndSettle();
    expect(opened, 1);
    await tester.tap(find.text('Open full app'));
    await tester.pumpAndSettle();
    expect(opened, 2);
  });
}
