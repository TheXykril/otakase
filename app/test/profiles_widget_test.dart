import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/profiles.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/main.dart';
import 'package:otakase/ui/components.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_test.dart' show MemoryTokens;
import 'profiles_test.dart' show fastLock, profileClient;
import 'widget_test.dart' as fixture;

Future<ProfileManager> prepare(WidgetTester tester, {bool tv = false}) async {
  await fixture.launch(tester, tv: tv);
  await tester.pumpWidget(const SizedBox());
  final m = ProfileManager(
    preferences: await SharedPreferences.getInstance(),
    tokens: MemoryTokens(),
    core: CoreClient(),
    isTV: tv,
    clientFactory: profileClient,
    hashLock: fastLock,
    platformCallbacks: false,
  );
  await m.initialize();
  return m;
}

void main() {
  testWidgets(
    'TV startup grid unlocks the chosen profile using D-pad directions',
    (tester) async {
      final first = await prepare(tester, tv: true);
      final second = await first.add('Family');
      await first.setLock(second.id, 'UDLR');
      await first.close();
      final startup = ProfileManager(
        preferences: first.preferences,
        tokens: first.tokens,
        core: CoreClient(),
        isTV: true,
        clientFactory: profileClient,
        hashLock: fastLock,
        platformCallbacks: false,
      );
      await startup.initialize();
      await tester.pumpWidget(ProfileHost(manager: startup));
      await tester.pumpAndSettle();
      expect(find.text("Who's watching?"), findsOneWidget);
      expect(find.byKey(ValueKey('profile-${second.id}')), findsOneWidget);
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
      await tester.sendKeyEvent(LogicalKeyboardKey.select);
      await tester.pumpAndSettle();
      expect(find.text('Unlock Family'), findsOneWidget);
      for (final key in [
        LogicalKeyboardKey.arrowUp,
        LogicalKeyboardKey.arrowDown,
        LogicalKeyboardKey.arrowLeft,
        LogicalKeyboardKey.arrowRight,
      ]) {
        await tester.sendKeyEvent(key);
      }
      await tester.sendKeyEvent(LogicalKeyboardKey.select);
      await tester.pumpAndSettle();
      expect(startup.activeId, second.id);
      expect(find.text('Find your next story'), findsOneWidget);
      expect(startup.session!.auth.accounts, isEmpty);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      await startup.close();
    },
  );
  testWidgets(
    'phone adds, edits and deletes profiles and enters a lock twice',
    (tester) async {
      final m = await prepare(tester);
      await tester.pumpWidget(ProfileHost(manager: m));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('nav-More')));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.byKey(const ValueKey('more-Profiles')));
      await tester.tap(find.byKey(const ValueKey('more-Profiles')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('lock-profile-1')));
      await tester.pumpAndSettle();
      for (var pass = 0; pass < 2; pass++) {
        for (final direction in ['U', 'D', 'L', 'R']) {
          await tester.tap(find.byKey(ValueKey('code-$direction')));
        }
        await tester.pump();
        await tester.tap(find.byKey(const Key('code-continue')));
        await tester.pumpAndSettle();
      }
      expect(m.profile('1').locked, isTrue);
      await tester.ensureVisible(find.text('Add profile'));
      await tester.tap(find.text('Add profile'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), 'Family');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      final second = m.profiles.last;
      expect(second.name, 'Family');
      final remove = find.byKey(ValueKey('delete-profile-${second.id}'));
      await tester.ensureVisible(remove);
      await tester.tap(remove);
      await tester.pumpAndSettle();
      expect(find.text('Delete Family?'), findsOneWidget);
      expect(Focus.of(tester.element(find.text('Cancel'))).hasFocus, isTrue);
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();
      expect(m.profiles, hasLength(2));
      await tester.tap(remove);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Delete profile'));
      await tester.pumpAndSettle();
      expect(m.profiles, hasLength(1));
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      await m.close();
    },
  );
  testWidgets(
    'profile switch removes the old navigation stack and private history',
    (tester) async {
      final m = await prepare(tester);
      await m.session!.store.set('AniListUsername', 'old-viewer');
      await m.session!.store.progress(
        const Anime(id: 42, title: 'Private history'),
        3,
        60,
        1200,
      );
      final second = await m.add('Other');
      await tester.pumpWidget(ProfileHost(manager: m));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('nav-More')));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.byKey(const ValueKey('more-Profiles')));
      await tester.tap(find.byKey(const ValueKey('more-Profiles')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Switch profile'));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(ValueKey('profile-${second.id}')));
      await tester.pumpAndSettle();
      expect(m.activeId, second.id);
      expect(find.text('Find your next story'), findsOneWidget);
      expect(find.text('old-viewer'), findsNothing);
      expect(find.text('Private history'), findsNothing);
      expect(m.session!.store.history, isEmpty);
      expect(m.session!.store.text('AniListUsername'), '');
      expect(find.byType(BackButton), findsNothing);
      expect(find.byType(FocusBox), findsWidgets);
      await tester.pumpWidget(const SizedBox());
      await m.close();
    },
  );
}
