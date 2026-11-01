import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/ui/more_page.dart';

import 'auth_test.dart' show MemoryTokens, account;
import 'tv_focus_test.dart' show key, launch, listResponse, navigation;
import 'tv_navigation_audit_test.dart' show focusedWithin, launchAudit, moreRow;
import 'widget_test.dart' as fixture;

const arrows = [
  LogicalKeyboardKey.arrowUp,
  LogicalKeyboardKey.arrowDown,
  LogicalKeyboardKey.arrowLeft,
  LogicalKeyboardKey.arrowRight,
];

String describe(FocusNode node) {
  final context = node.context;
  if (context == null) return node.toString();
  final words = <String>[];
  void visit(Element e) {
    final w = e.widget;
    if (w is Text && w.data != null) words.add(w.data!);
    if (w is Tooltip && w.message != null) words.add(w.message!);
    if (w is Icon && words.isEmpty) words.add('icon');
    e.visitChildElements(visit);
  }

  (context as Element).visitChildElements(visit);
  return '${node.debugLabel ?? context.widget.runtimeType}: ${words.take(3).join(' | ')}';
}

bool usable(FocusNode node) =>
    node.context != null &&
    node.canRequestFocus &&
    !node.skipTraversal &&
    node.descendantsAreFocusable &&
    !node.rect.isEmpty;

/// Walks every arrow move from the focused control and returns the controls
/// in the same focus scope that no chain of arrow presses can reach.
bool trace = false;
Future<List<String>> unreachable(WidgetTester tester) async {
  final start = FocusManager.instance.primaryFocus!;
  final scope = start.nearestScope!;
  final seen = <FocusNode>{start};
  final reached = {describe(start)};
  final queue = [start];
  while (queue.isNotEmpty) {
    final node = queue.removeLast();
    for (final arrow in arrows) {
      if (node.context == null) break;
      node.requestFocus();
      await tester.pumpAndSettle();
      if (!node.hasPrimaryFocus) break;
      // Jumping between controls is not a remote path, so forget the moves
      // the directional policy remembers for going back.
      FocusTraversalGroup.maybeOf(node.context!)
          ?.invalidateScopeData(node.nearestScope!);
      await key(tester, arrow);
      final next = FocusManager.instance.primaryFocus;
      if (trace) {
        // ignore: avoid_print
        print(
          '${describe(node)} --$arrow--> ${next == null ? null : describe(next)}',
        );
      }
      if (next != null && next.nearestScope == scope && seen.add(next)) {
        reached.add(describe(next));
        queue.add(next);
      }
    }
  }
  // Lazy lists rebuild controls that scroll away with new focus nodes, so
  // compare what the controls show as well as the nodes themselves.
  return scope.traversalDescendants
      .where((n) => usable(n) && !seen.contains(n))
      .map(describe)
      .where((label) => !reached.contains(label))
      .toList();
}

bool inAccounts() =>
    FocusManager.instance.primaryFocus?.context
        ?.findAncestorWidgetOfExactType<AccountsPage>() !=
    null;

/// Launches the TV app with a catalog and trackers that answer nothing but
/// errors, or empty lists when [empty] is set.
Future<void> launchOffline(
  WidgetTester tester, {
  bool signedIn = false,
  bool empty = false,
}) async {
  await fixture.launch(tester, tv: true);
  await tester.pumpWidget(const SizedBox());
  final base = await fixture.services();
  http.Response reply(http.Request _) => empty
      ? http.Response(
          '{"data":{"MediaListCollection":{"lists":[]},"Page":{"media":[],"airingSchedules":[],"pageInfo":{"hasNextPage":false}}}}',
          200,
        )
      : http.Response('down', 500);
  final auth = AuthService(
    storage: MemoryTokens(),
    client: MockClient((r) async => reply(r)),
  );
  if (signedIn) auth.accounts['AniList'] = account('AniList');
  final services = AppServices(
    catalog: Catalog(client: MockClient((r) async => reply(r))),
    store: base.store,
    core: base.core,
    auth: auth,
  );
  await tester.pumpWidget(OtakaseApp(services: services, isTV: true));
  await tester.pumpAndSettle();
  addTearDown(services.close);
}

bool onTopPage() {
  final context = FocusManager.instance.primaryFocus?.context;
  if (context == null) return false;
  final route = ModalRoute.of(context);
  return route != null &&
      route.isCurrent &&
      FocusManager.instance.primaryFocus is! FocusScopeNode;
}

bool focusedOn(String text) {
  var found = false;
  void visit(Element e) {
    if (e.widget is Text && (e.widget as Text).data == text) found = true;
    if (!found) e.visitChildElements(visit);
  }

  final context = FocusManager.instance.primaryFocus?.context;
  if (context != null) visit(context as Element);
  return found;
}

Future<void> openAccounts(WidgetTester tester) async {
  navigation(tester, 'More').requestFocus();
  await tester.pump();
  await key(tester, LogicalKeyboardKey.select);
  await key(tester, LogicalKeyboardKey.select);
  expect(find.byType(AccountsPage), findsOneWidget);
}

void main() {
  final states = <String, void Function(AuthService)>{
    'signed out': (_) {},
    'AniList signed in': (a) => a.accounts['AniList'] = account('AniList'),
    'MAL signed in': (a) => a.accounts['MAL'] = account('MAL'),
    'both signed in': (a) {
      a.accounts['AniList'] = account('AniList');
      a.accounts['MAL'] = account('MAL');
    },
    'AniList expired': (a) =>
        a.accounts['AniList'] = account('AniList', expired: true),
  };
  for (final entry in states.entries) {
    testWidgets('TV Accounts controls are all reachable: ${entry.key}', (
      tester,
    ) async {
      final auth = AuthService(
        storage: MemoryTokens(),
        client: MockClient((_) async => listResponse()),
      );
      entry.value(auth);
      await launch(tester, auth: auth);
      await openAccounts(tester);
      expect(inAccounts(), isTrue, reason: 'Focus starts behind Accounts');
      expect(FocusManager.instance.primaryFocus, isNot(isA<FocusScopeNode>()));
      expect(await unreachable(tester), isEmpty);
      await tester.pumpWidget(const SizedBox());
    });
  }

  testWidgets('TV log out keeps the remote on that tracker', (tester) async {
    final auth = AuthService(
      storage: MemoryTokens(),
      client: MockClient((_) async => listResponse()),
    );
    auth.accounts['AniList'] = account('AniList');
    auth.accounts['MAL'] = account('MAL');
    await launch(tester, auth: auth);
    await openAccounts(tester);
    expect(focusedOn('Log out of AniList'), isTrue);
    await key(tester, LogicalKeyboardKey.select);
    expect(focusedOn('Sign in to AniList'), isTrue);
    for (var i = 0; i < 8 && !focusedOn('Log out of MAL'); i++) {
      await key(tester, LogicalKeyboardKey.arrowDown);
    }
    expect(focusedOn('Log out of MAL'), isTrue);
    await key(tester, LogicalKeyboardKey.select);
    expect(focusedOn('Sign in to MAL'), isTrue);
    expect(await unreachable(tester), isEmpty);
    await tester.pumpWidget(const SizedBox());
  });

  final pages = [
    'Accounts',
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
  ];
  for (final signedIn in [false, true]) {
    final state = signedIn ? 'signed in' : 'guest';
    for (final title in pages) {
      testWidgets('TV More $title has no unreachable controls ($state)', (
        tester,
      ) async {
        final auth = AuthService(
          storage: MemoryTokens(),
          client: MockClient((_) async => listResponse()),
        );
        if (signedIn) auth.accounts['AniList'] = account('AniList');
        await launch(tester, auth: auth);
        navigation(tester, 'More').requestFocus();
        await tester.pump();
        await key(tester, LogicalKeyboardKey.select);
        final row = find.byKey(ValueKey('more-$title'));
        for (var i = 0; i < 24 && !focusedWithin(row); i++) {
          await key(tester, LogicalKeyboardKey.arrowDown);
        }
        expect(focusedWithin(row), isTrue);
        await key(tester, LogicalKeyboardKey.select);
        expect(onTopPage(), isTrue, reason: 'Focus starts behind $title');
        expect(await unreachable(tester), isEmpty);
        await tester.pumpWidget(const SizedBox());
      });
    }
    testWidgets('TV Home has no unreachable controls ($state)', (tester) async {
      final auth = AuthService(
        storage: MemoryTokens(),
        client: MockClient((_) async => listResponse()),
      );
      if (signedIn) auth.accounts['AniList'] = account('AniList');
      await launch(tester, auth: auth);
      navigation(tester, 'Home').requestFocus();
      await tester.pump();
      await key(tester, LogicalKeyboardKey.arrowDown);
      expect(await unreachable(tester), isEmpty);
      await tester.pumpWidget(const SizedBox());
    });
    testWidgets('TV Search has no unreachable controls ($state)', (
      tester,
    ) async {
      final auth = AuthService(
        storage: MemoryTokens(),
        client: MockClient((_) async => listResponse()),
      );
      if (signedIn) auth.accounts['AniList'] = account('AniList');
      await launch(tester, auth: auth);
      navigation(tester, 'Home').requestFocus();
      await tester.pump();
      await key(tester, LogicalKeyboardKey.arrowRight);
      await key(tester, LogicalKeyboardKey.arrowDown);
      expect(await unreachable(tester), isEmpty);
      await tester.pumpWidget(const SizedBox());
    });
  }

  testWidgets('TV Profiles has no unreachable controls', (tester) async {
    await launchAudit(tester);
    await moreRow(tester, 'Profiles');
    expect(onTopPage(), isTrue);
    expect(await unreachable(tester), isEmpty);
    await tester.pumpWidget(const SizedBox());
  });

  Future<void> openShow(WidgetTester tester, {bool signedIn = true}) async {
    final auth = AuthService(
      storage: MemoryTokens(),
      client: MockClient((_) async => listResponse()),
    );
    if (signedIn) auth.accounts['AniList'] = account('AniList');
    await launch(tester, auth: auth);
    navigation(tester, 'Search').requestFocus();
    await tester.pump();
    await key(tester, LogicalKeyboardKey.arrowDown);
    final query = find.byKey(const Key('catalog-search'));
    await tester.enterText(query, 'Frieren');
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pumpAndSettle();
    await key(tester, LogicalKeyboardKey.arrowDown);
    await key(tester, LogicalKeyboardKey.select);
    expect(find.byKey(const Key('show-status')), findsOneWidget);
  }

  for (final signedIn in [false, true]) {
    final who = signedIn ? 'signed in' : 'guest';
    testWidgets('TV list status dialog has no unreachable controls ($who)', (
      tester,
    ) async {
      await openShow(tester, signedIn: signedIn);
      Focus.of(
        tester.element(
          find.descendant(
            of: find.byKey(const Key('show-status')),
            matching: find.byType(Text),
          ),
        ),
      ).requestFocus();
      await tester.pumpAndSettle();
      await key(tester, LogicalKeyboardKey.select);
      expect(find.byType(Dialog), findsOneWidget);
      expect(onTopPage(), isTrue);
      expect(await unreachable(tester), isEmpty);
      await tester.pumpWidget(const SizedBox());
    });
  }

  testWidgets('TV All episodes has no unreachable controls', (tester) async {
    await openShow(tester);
    final all = find.text('All episodes');
    await tester.ensureVisible(all);
    Focus.of(tester.element(all)).requestFocus();
    await tester.pumpAndSettle();
    await key(tester, LogicalKeyboardKey.select);
    expect(find.byType(BackButton), findsOneWidget);
    expect(onTopPage(), isTrue);
    expect(await unreachable(tester), isEmpty);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV show page has no unreachable controls', (tester) async {
    final auth = AuthService(
      storage: MemoryTokens(),
      client: MockClient((_) async => listResponse()),
    );
    auth.accounts['AniList'] = account('AniList');
    await launch(tester, auth: auth);
    navigation(tester, 'Home').requestFocus();
    await tester.pump();
    await key(tester, LogicalKeyboardKey.arrowDown);
    final poster = find.byKey(ValueKey('AniList-CURRENT-${fixture.anime.key}'));
    for (var i = 0; i < 12 && !focusedWithin(poster); i++) {
      await key(tester, LogicalKeyboardKey.arrowDown);
    }
    expect(focusedWithin(poster), isTrue);
    await key(tester, LogicalKeyboardKey.select);
    expect(onTopPage(), isTrue);
    expect(await unreachable(tester), isEmpty);
    await tester.pumpWidget(const SizedBox());
  });

  for (final empty in [false, true]) {
    final state = empty ? 'empty' : 'offline';
    for (final signedIn in [false, true]) {
      final who = signedIn ? 'signed in' : 'guest';
      testWidgets('TV Home has no unreachable controls ($state, $who)', (
        tester,
      ) async {
        await launchOffline(tester, signedIn: signedIn, empty: empty);
        navigation(tester, 'Home').requestFocus();
        await tester.pump();
        await key(tester, LogicalKeyboardKey.arrowDown);
        expect(onTopPage(), isTrue);
        expect(await unreachable(tester), isEmpty);
        await tester.pumpWidget(const SizedBox());
      });
    }
    for (final title in ['Schedule', 'Watch rooms', 'Discord']) {
      testWidgets('TV More $title has no unreachable controls ($state)', (
        tester,
      ) async {
        await launchOffline(tester, empty: empty);
        navigation(tester, 'More').requestFocus();
        await tester.pump();
        await key(tester, LogicalKeyboardKey.select);
        final row = find.byKey(ValueKey('more-$title'));
        for (var i = 0; i < 24 && !focusedWithin(row); i++) {
          await key(tester, LogicalKeyboardKey.arrowDown);
        }
        await key(tester, LogicalKeyboardKey.select);
        expect(onTopPage(), isTrue);
        expect(await unreachable(tester), isEmpty);
        await tester.pumpWidget(const SizedBox());
      });
    }
  }
}
