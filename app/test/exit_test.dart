import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';

import 'widget_test.dart' as fixture;
import 'auth_test.dart' show MemoryTokens;

void main() {
  late int exits;

  setUp(() {
    exits = 0;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
          if (call.method == 'SystemNavigator.pop') exits++;
          return null;
        });
  });

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, null);
  });

  Future<void> launch(WidgetTester tester, {required bool tv}) async {
    await fixture.launch(tester, tv: tv);
    await tester.pumpWidget(const SizedBox());
    final base = await fixture.services();
    final services = AppServices(
      catalog: base.catalog,
      store: base.store,
      core: base.core,
      auth: AuthService(client: base.catalog.client, storage: MemoryTokens()),
    );
    addTearDown(services.close);
    await tester.pumpWidget(OtakaseApp(services: services, isTV: tv));
    await tester.pumpAndSettle();
    if (tv) {
      tester
          .widget<FocusBox>(find.byKey(const ValueKey('nav-Home')))
          .focusNode!
          .requestFocus();
      await tester.pumpAndSettle();
    }
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.text('Exit Otakase?'), findsOneWidget);
  }

  testWidgets('TV exit prompt starts with Exit selected', (tester) async {
    await launch(tester, tv: true);
    expect(Focus.of(tester.element(find.text('Exit'))).hasPrimaryFocus, isTrue);
    expect(exits, 0);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV exits with one OK and no greeting dialog', (tester) async {
    await launch(tester, tv: true);
    Focus.of(tester.element(find.text('Exit'))).requestFocus();
    await tester.pumpAndSettle();

    await tester.sendKeyEvent(LogicalKeyboardKey.select);
    await tester.pumpAndSettle();

    expect(exits, 1);
    expect(find.byType(AlertDialog), findsNothing);
    expect(find.text('Close'), findsNothing);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV Stay and Back both cancel exit', (tester) async {
    await launch(tester, tv: true);
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowLeft);
    await tester.pumpAndSettle();
    expect(Focus.of(tester.element(find.text('Stay'))).hasPrimaryFocus, isTrue);
    await tester.sendKeyEvent(LogicalKeyboardKey.select);
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
    expect(exits, 0);

    tester
        .widget<FocusBox>(find.byKey(const ValueKey('nav-Home')))
        .focusNode!
        .requestFocus();
    await tester.pumpAndSettle();
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.text('Exit Otakase?'), findsOneWidget);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
    expect(exits, 0);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('phone keeps Stay selected and its exit greeting', (
    tester,
  ) async {
    await launch(tester, tv: false);
    expect(Focus.of(tester.element(find.text('Stay'))).hasPrimaryFocus, isTrue);
    await tester.tap(find.text('Exit'));
    await tester.pumpAndSettle();
    expect(find.text('Exit Otakase?'), findsNothing);
    expect(find.byType(AlertDialog), findsOneWidget);
    expect(find.text('Close'), findsOneWidget);
    expect(exits, 0);
    await tester.tap(find.text('Close'));
    await tester.pumpAndSettle();
    expect(exits, 1);
    await tester.pumpWidget(const SizedBox());
  });
}
