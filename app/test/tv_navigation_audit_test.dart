import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/profiles.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/community_page.dart';

import 'auth_test.dart' show MemoryTokens, json;
import 'tv_focus_test.dart' show key, navigation;
import 'widget_test.dart' as fixture;
import 'profiles_test.dart' as profiles;

Future<AppServices> launchAudit(
  WidgetTester tester, {
  bool voice = false,
}) async {
  await fixture.launch(tester, tv: true);
  await tester.pumpWidget(const SizedBox());
  if (voice) {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          const MethodChannel('otakase/device'),
          (call) async => call.method == 'status'
              ? {'voiceAvailable': true, 'use24Hour': true}
              : null,
        );
  }
  final base = await fixture.services();
  final preferences = await SharedPreferences.getInstance();
  await preferences.setString(
    'profiles.v1',
    jsonEncode({
      'active': '1',
      'profiles': [
        const Profile(id: '1', name: 'Profile 1').toJson(),
        const Profile(id: '2', name: 'Profile 2').toJson(),
      ],
    }),
  );
  final manager = await profiles.manager(
    tv: true,
    clientFactory: () => MockClient((_) async => json({})),
  );
  final s = AppServices(
    catalog: base.catalog,
    store: base.store,
    core: base.core,
    profiles: manager,
    auth: AuthService(
      storage: MemoryTokens(),
      client: MockClient((_) async => json({})),
    ),
  );
  await tester.pumpWidget(OtakaseApp(services: s, isTV: true));
  await tester.pumpAndSettle();
  addTearDown(() async {
    await tester.pumpWidget(const SizedBox());
    await s.close();
  });
  navigation(tester, 'Home').requestFocus();
  await tester.pumpAndSettle();
  return s;
}

bool focusedWithin(Finder finder) {
  final targets = finder.evaluate().toSet();
  if (targets.any((e) => Focus.maybeOf(e)?.hasPrimaryFocus == true)) {
    return true;
  }
  final focused = FocusManager.instance.primaryFocus?.context;
  var inside = targets.contains(focused);
  focused?.visitAncestorElements((element) {
    inside = inside || targets.contains(element);
    return !inside;
  });
  return inside;
}

Future<void> home(WidgetTester tester) async {
  for (var i = 0; i < 4 && !navigation(tester, 'Home').hasPrimaryFocus; i++) {
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
  }
  expect(navigation(tester, 'Home').hasPrimaryFocus, isTrue);
  await key(tester, LogicalKeyboardKey.arrowDown);
  expect(find.text('Find your next story'), findsOneWidget);
}

Future<void> moreRow(WidgetTester tester, String title) async {
  navigation(tester, 'More').requestFocus();
  await tester.pumpAndSettle();
  await key(tester, LogicalKeyboardKey.arrowDown);
  final row = find.byKey(ValueKey('more-$title'));
  for (var i = 0; i < 24 && !focusedWithin(row); i++) {
    await key(tester, LogicalKeyboardKey.arrowDown);
  }
  expect(focusedWithin(row), isTrue);
  await key(tester, LogicalKeyboardKey.select);
}

void main() {
  testWidgets('TV Search voice control and filters remain reachable', (
    tester,
  ) async {
    await launchAudit(tester, voice: true);
    await key(tester, LogicalKeyboardKey.arrowRight);
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(focusedWithin(find.byKey(const Key('catalog-search'))), isTrue);
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(focusedWithin(find.byTooltip('Voice search')), isTrue);
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(
      FocusManager.instance.primaryFocus?.context
          ?.findAncestorWidgetOfExactType<AppTab>(),
      isNotNull,
    );
    await home(tester);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV header moves left from More through Search to Home', (
    tester,
  ) async {
    await launchAudit(tester);
    await key(tester, LogicalKeyboardKey.arrowRight);
    await key(tester, LogicalKeyboardKey.arrowRight);
    await key(tester, LogicalKeyboardKey.arrowDown);
    await key(tester, LogicalKeyboardKey.arrowUp);
    expect(navigation(tester, 'More').hasPrimaryFocus, isTrue);
    await key(tester, LogicalKeyboardKey.arrowLeft);
    expect(navigation(tester, 'Search').hasPrimaryFocus, isTrue);
    await key(tester, LogicalKeyboardKey.arrowLeft);
    expect(navigation(tester, 'Home').hasPrimaryFocus, isTrue);
    await key(tester, LogicalKeyboardKey.select);
    expect(find.text('Find your next story'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV long choice dialog focuses its selected offscreen value', (
    tester,
  ) async {
    await launchAudit(tester);
    final context = tester.element(find.text('Find your next story'));
    final result = chooseValue(
      context,
      'Year',
      List.generate(50, (i) => '${2026 - i}'),
      selected: '1980',
    );
    await tester.pumpAndSettle();
    expect(focusedWithin(find.text('1980')), isTrue);
    final rect = tester.getRect(find.text('1980'));
    expect(rect.top, greaterThan(0));
    expect(rect.bottom, lessThan(tester.view.physicalSize.height));
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(focusedWithin(find.text('1979')), isTrue);
    await key(tester, LogicalKeyboardKey.select);
    expect(await result, '1979');
    await tester.pumpAndSettle();
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV capsule text leaves editing when its keyboard is hidden', (
    tester,
  ) async {
    await launchAudit(tester);
    final context = tester.element(find.text('Find your next story'));
    final result = communityText(context, title: 'Your letter', isTV: true);
    await tester.pumpAndSettle();
    final field = find.byKey(const Key('capsule-text'));
    await tester.showKeyboard(field);
    await tester.enterText(field, 'A letter\nwith two lines');
    tester.view.viewInsets = const FakeViewPadding(bottom: 240);
    addTearDown(tester.view.resetViewInsets);
    await tester.pumpAndSettle();
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(focusedWithin(field), isTrue);
    tester.view.viewInsets = const FakeViewPadding();
    await tester.pumpAndSettle();
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(focusedWithin(field), isFalse);
    expect(focusedWithin(find.text('Type on phone')), isTrue);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(await result, isNull);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV settings text allows Down to Cancel and Back to Home', (
    tester,
  ) async {
    await launchAudit(tester);
    await moreRow(tester, 'Subtitles & languages');
    final setting = find.text('Audio languages');
    for (var i = 0; i < 24 && !focusedWithin(setting); i++) {
      await key(tester, LogicalKeyboardKey.arrowDown);
    }
    expect(focusedWithin(setting), isTrue);
    await key(tester, LogicalKeyboardKey.select);
    expect(focusedWithin(find.byType(TextField)), isTrue);
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(focusedWithin(find.text('Cancel')), isTrue);
    await key(tester, LogicalKeyboardKey.select);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pumpAndSettle();
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    await home(tester);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV profile name allows Down to colours without trapping input', (
    tester,
  ) async {
    await launchAudit(tester);
    await moreRow(tester, 'Profiles');
    for (var i = 0; i < 24 && !focusedWithin(find.text('Add profile')); i++) {
      await key(tester, LogicalKeyboardKey.arrowDown);
    }
    expect(focusedWithin(find.text('Add profile')), isTrue);
    await key(tester, LogicalKeyboardKey.select);
    final field = find.byType(TextField);
    for (var i = 0; i < 24 && !focusedWithin(field); i++) {
      await key(tester, LogicalKeyboardKey.arrowUp);
    }
    expect(focusedWithin(field), isTrue);
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(focusedWithin(field), isFalse);
    expect(find.byType(AlertDialog), findsOneWidget);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    await home(tester);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV Search Down leaves text input for filters and results', (
    tester,
  ) async {
    await launchAudit(tester);
    await key(tester, LogicalKeyboardKey.arrowRight);
    await key(tester, LogicalKeyboardKey.arrowDown);
    final query = find.byKey(const Key('catalog-search'));
    expect(focusedWithin(query), isTrue);
    await key(tester, LogicalKeyboardKey.select);
    expect(tester.testTextInput.isVisible, isTrue);
    await tester.enterText(query, 'Frieren');
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pumpAndSettle();
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(focusedWithin(query), isFalse);
    expect(focusedWithin(find.text('Format')), isTrue);
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(
      FocusManager.instance.primaryFocus?.context
          ?.findAncestorWidgetOfExactType<PosterCard>(),
      isNotNull,
    );
    await home(tester);
    await tester.pumpWidget(const SizedBox());
  });

  for (final title in [
    'Accounts',
    'Profiles',
    'Playback',
    'Subtitles & languages',
    'Schedule',
    'Discord',
    'Time capsule',
    'Watch rooms',
    'Phone remote',
    'Appearance',
    'About & updates',
    'Diagnostics',
  ]) {
    testWidgets('TV More $title opens and returns to Home with remote only', (
      tester,
    ) async {
      await launchAudit(tester);
      await key(tester, LogicalKeyboardKey.arrowRight);
      await key(tester, LogicalKeyboardKey.arrowRight);
      await key(tester, LogicalKeyboardKey.arrowDown);
      final row = find.byKey(ValueKey('more-$title'));
      for (var i = 0; i < 24 && !focusedWithin(row); i++) {
        await key(tester, LogicalKeyboardKey.arrowDown);
      }
      expect(
        focusedWithin(row),
        isTrue,
        reason: 'Unreachable More row: $title',
      );
      final moreNavigation = navigation(tester, 'More');
      await key(tester, LogicalKeyboardKey.select);
      expect(moreNavigation.hasFocus, isFalse);
      // Every page and sheet must expose a remote-reachable Back control.
      expect(find.byTooltip('Back'), findsOneWidget);
      for (var i = 0; i < 24 && !focusedWithin(find.byTooltip('Back')); i++) {
        await key(tester, LogicalKeyboardKey.arrowUp);
      }
      expect(
        focusedWithin(find.byTooltip('Back')),
        isTrue,
        reason: 'Unreachable Back control: $title',
      );
      await key(tester, LogicalKeyboardKey.select);
      expect(focusedWithin(row), isTrue, reason: 'Return focus for $title');
      await home(tester);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
  }
}
