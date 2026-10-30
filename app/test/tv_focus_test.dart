import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/catalog_pages.dart';
import 'package:otakase/ui/more_page.dart';
import 'package:otakase/ui/show_page.dart';

import 'auth_test.dart' show MemoryTokens, account;
import 'widget_test.dart' as fixture;

// Exercise the login dialog and real completion event without a LAN listener.
class LoginAuth extends AuthService {
  LoginAuth({required super.client}) : super(storage: MemoryTokens());
  Uri? authorization;
  @override
  Future<Uri> begin(String service, {bool tv = false}) async =>
      authorization = await super.begin(service);
}

http.Response listResponse() => http.Response(
  jsonEncode({
    'data': {
      'MediaListCollection': {
        'lists': [
          {
            'entries': [
              {
                'status': 'CURRENT',
                'progress': 3,
                'media': fixture.anime.toJson(),
              },
            ],
          },
        ],
      },
    },
  }),
  200,
);

Future<AppServices> launch(
  WidgetTester tester, {
  required AuthService auth,
}) async {
  await fixture.launch(tester, tv: true);
  await tester.pumpWidget(const SizedBox());
  final base = await fixture.services();
  final services = AppServices(
    catalog: base.catalog,
    store: base.store,
    core: base.core,
    auth: auth,
  );
  await tester.pumpWidget(OtakaseApp(services: services, isTV: true));
  await tester.pumpAndSettle();
  addTearDown(services.close);
  return services;
}

FocusNode navigation(WidgetTester tester, String label) =>
    tester.widget<FocusBox>(find.byKey(ValueKey('nav-$label'))).focusNode!;

Future<void> key(WidgetTester tester, LogicalKeyboardKey key) async {
  await tester.sendKeyEvent(key);
  await tester.pumpAndSettle();
}

Future<void> focusPoster(WidgetTester tester, Finder poster) async {
  await tester.ensureVisible(poster);
  Focus.of(
    tester.element(
      find.descendant(of: poster, matching: find.byType(CoverImage)),
    ),
  ).requestFocus();
  await tester.pumpAndSettle();
  expect(
    FocusManager.instance.primaryFocus?.context
        ?.findAncestorWidgetOfExactType<PosterCard>(),
    isNotNull,
  );
}

bool inAccounts() =>
    FocusManager.instance.primaryFocus?.context
        ?.findAncestorWidgetOfExactType<AccountsPage>() !=
    null;

void main() {
  testWidgets('TV Up from Home posters reaches Home, Search and More tabs', (
    tester,
  ) async {
    final auth = AuthService(
      storage: MemoryTokens(),
      client: MockClient((_) async => listResponse()),
    );
    auth.accounts['AniList'] = account('AniList');
    await launch(tester, auth: auth);
    // Start on a real poster, below the category picker and page controls.
    await focusPoster(
      tester,
      find.byKey(ValueKey('AniList-CURRENT-${fixture.anime.key}')),
    );
    for (var i = 0; i < 4 && !navigation(tester, 'Home').hasFocus; i++) {
      await key(tester, LogicalKeyboardKey.arrowUp);
    }
    expect(navigation(tester, 'Home').hasPrimaryFocus, isTrue);
    await key(tester, LogicalKeyboardKey.arrowRight);
    expect(navigation(tester, 'Search').hasPrimaryFocus, isTrue);
    await key(tester, LogicalKeyboardKey.select);
    expect(find.byKey(const Key('catalog-search')), findsOneWidget);

    // Up must also leave a single-line search field, with the keyboard open.
    await tester.showKeyboard(find.byKey(const Key('catalog-search')));
    await key(tester, LogicalKeyboardKey.arrowUp);
    expect(navigation(tester, 'Search').hasPrimaryFocus, isTrue);
    await key(tester, LogicalKeyboardKey.arrowRight);
    expect(navigation(tester, 'More').hasPrimaryFocus, isTrue);
    await key(tester, LogicalKeyboardKey.select);
    await key(tester, LogicalKeyboardKey.arrowUp);
    expect(navigation(tester, 'More').hasPrimaryFocus, isTrue);
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(navigation(tester, 'More').hasFocus, isFalse);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(navigation(tester, 'More').hasPrimaryFocus, isTrue);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets(
    'TV Show keeps focus above the tabs and restores Search on Back',
    (tester) async {
      final auth = AuthService(storage: MemoryTokens());
      await launch(tester, auth: auth);
      navigation(tester, 'Search').requestFocus();
      await tester.pump();
      await key(tester, LogicalKeyboardKey.select);
      await focusPoster(
        tester,
        find.byKey(ValueKey('search-${fixture.anime.key}')),
      );
      await key(tester, LogicalKeyboardKey.select);
      expect(find.byType(ShowPage), findsOneWidget);
      await key(tester, LogicalKeyboardKey.arrowUp);
      expect(
        FocusManager.instance.primaryFocus?.context
            ?.findAncestorWidgetOfExactType<ShowPage>(),
        isNotNull,
      );
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(
        FocusManager.instance.primaryFocus?.context
            ?.findAncestorWidgetOfExactType<SearchPage>(),
        isNotNull,
      );
      await tester.pumpWidget(const SizedBox());
    },
  );

  for (final origin in ['Home', 'More']) {
    testWidgets('TV Accounts keeps focus after login from $origin', (
      tester,
    ) async {
      final rows = Completer<http.Response>();
      final auth = LoginAuth(
        client: MockClient((request) async {
          if (request.body.contains('MediaListCollection')) return rows.future;
          return http.Response(
            jsonEncode({
              'data': {
                'Viewer': {'id': 7, 'name': 'viewer'},
              },
            }),
            200,
          );
        }),
      );
      await launch(tester, auth: auth);
      if (origin == 'More') {
        navigation(tester, 'More').requestFocus();
        await tester.pump();
        await key(tester, LogicalKeyboardKey.select);
      } else {
        Focus.of(tester.element(find.text('Accounts'))).requestFocus();
        await tester.pump();
      }
      await key(tester, LogicalKeyboardKey.select);
      expect(find.byType(AccountsPage), findsOneWidget);
      await key(tester, LogicalKeyboardKey.select);
      expect(find.text('Cancel'), findsOneWidget);
      await auth.accept(
        Uri(
          scheme: 'otakase',
          host: 'login',
          queryParameters: {
            'state': auth.authorization!.queryParameters['state']!,
            'access_token': 'approved',
          },
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('Cancel'), findsNothing);
      expect(find.text('Log out of AniList'), findsOneWidget);
      expect(inAccounts(), isTrue);
      expect(FocusManager.instance.primaryFocus?.debugLabel, 'Accounts back');

      // The tracker reply mounts new Home controls after the dialog closes.
      rows.complete(listResponse());
      await tester.pumpAndSettle();
      expect(inAccounts(), isTrue);
      expect(FocusManager.instance.primaryFocus?.debugLabel, 'Accounts back');
      await key(tester, LogicalKeyboardKey.arrowDown);
      expect(inAccounts(), isTrue);
      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        isNot('Accounts back'),
      );
      await key(tester, LogicalKeyboardKey.arrowUp);
      expect(FocusManager.instance.primaryFocus?.debugLabel, 'Accounts back');
      await key(tester, LogicalKeyboardKey.select);
      expect(find.byType(AccountsPage), findsNothing);
      expect(FocusManager.instance.primaryFocus?.context, isNotNull);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
  }

  testWidgets('TV cancelling login restores the Accounts Back control', (
    tester,
  ) async {
    final auth = LoginAuth(client: MockClient((_) async => listResponse()));
    await launch(tester, auth: auth);
    Focus.of(tester.element(find.text('Accounts'))).requestFocus();
    await tester.pump();
    await key(tester, LogicalKeyboardKey.select);
    await key(tester, LogicalKeyboardKey.select);
    expect(find.text('Cancel'), findsOneWidget);
    await key(tester, LogicalKeyboardKey.select);
    expect(FocusManager.instance.primaryFocus?.debugLabel, 'Accounts back');
    await key(tester, LogicalKeyboardKey.select);
    expect(find.byType(AccountsPage), findsNothing);
    await tester.pumpWidget(const SizedBox());
  });
}
