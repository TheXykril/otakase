import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/data/tv_remote.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/tv_remote_page.dart';
import 'package:otakase/ui/watch_rooms_page.dart';
import 'package:otakase/ui/movie_page.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_test.dart' show MemoryTokens, json;
import 'widget_test.dart' as fixture;

const tv = TVDevice(
  id: 'tv-1',
  name: 'Living room',
  host: '192.168.1.8',
  port: 12345,
);

class FakeLan extends LanTransport {
  final commands = <String>[];
  bool adult = false;
  @override
  Future<Json> post(
    TVDevice device,
    String path,
    Json body, {
    String token = '',
  }) async {
    if (path == '/pair') {
      return {
        'id': device.id,
        'name': device.name,
        'token': 'pair-token',
        'client': 'phone',
      };
    }
    if (path == '/command') {
      commands.add('${body['action']}:${jsonEncode(body['args'])}');
    }
    return {
      'has_player': true,
      'title': adult ? 'Hidden title' : 'Playing story',
      'adult': adult,
      'episode': 3,
      'episodes': 12,
      'position': 40,
      'volume': 50,
    };
  }
}

Future<AppServices> services({FakeLan? lan}) async {
  final tokens = MemoryTokens();
  tokens.values['discord.link'] = jsonEncode({
    'token': 'app-token',
    'discord_name': 'Reader',
  });
  final client = MockClient(
    (r) async => json({
      'rooms': [
        {
          'id': 'room-1',
          'show': 'Shared story',
          'people': 2,
          'max': 8,
          'ep': 4,
          'channel_name': 'Lounge',
        },
        {
          'id': 'hidden',
          'show': 'Hidden room',
          'adult': true,
          'people': 1,
          'max': 8,
        },
      ],
    }),
  );
  final s = AppServices(
    catalog: Catalog(client: client),
    store: AppStore(await SharedPreferences.getInstance()),
    core: CoreClient(),
    auth: AuthService(client: client, storage: tokens),
    remoteControl: TVRemote(storage: tokens, transport: lan ?? FakeLan()),
  );
  await s.discord.initialize(background: false);
  await s.tvRemote.initialize();
  return s;
}

Widget page(Widget home, {bool tv = false}) => AppLayout(
  isTV: tv,
  child: MaterialApp(theme: ThemeData.dark(), home: home),
);
void main() {
  testWidgets('the film stays hidden from Search while switched off', (
    tester,
  ) async {
    await fixture.launch(tester, tv: true);
    await tester.tap(find.byKey(const Key('nav-Search')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('catalog-search')), 'Otakase');
    await tester.testTextInput.receiveAction(TextInputAction.search);
    await tester.pumpAndSettle();
    expect(find.text('Otakase: The Movie'), findsNothing);
  });

  testWidgets('searching otakase opens the film and Back returns to Search', (
    tester,
  ) async {
    otakaseMovieEnabled = true;
    addTearDown(() => otakaseMovieEnabled = false);
    await fixture.launch(tester, tv: true);
    await tester.tap(find.byKey(const Key('nav-Search')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('catalog-search')), 'Otakase');
    await tester.testTextInput.receiveAction(TextInputAction.search);
    await tester.pumpAndSettle();
    expect(find.text('Otakase: The Movie'), findsOneWidget);
    final card = find.byKey(const Key('search-mal:-1'));
    await tester.ensureVisible(card);
    await tester.tap(card);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));
    expect(find.byType(OtakaseMovie), findsOneWidget);
    expect(find.text('Counting the Stars'), findsOneWidget);
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
    await tester.pump(const Duration(milliseconds: 600));
    expect(find.text('The Voices'), findsOneWidget);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('catalog-search')), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets(
    'watch-room lobby shows allowed live rooms and remote reaches Refresh',
    (tester) async {
      await fixture.launch(tester, tv: true);
      await tester.pumpWidget(const SizedBox());
      final s = await services();
      await tester.pumpWidget(
        page(WatchRoomsPage(services: s, isTV: true), tv: true),
      );
      await tester.pumpAndSettle();
      expect(find.text('Shared story'), findsOneWidget);
      expect(find.text('Hidden room'), findsNothing);
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
      await tester.pump();
      expect(FocusManager.instance.primaryFocus, isNotNull);
      final card = tester.widget<FocusBox>(
        find.byKey(const Key('room-room-1')),
      );
      expect(card.onPressed, isNotNull);
      await tester.pumpWidget(const SizedBox());
      await s.close();
    },
  );
  testWidgets('phone pairs and exposes playback, episode and volume controls', (
    tester,
  ) async {
    await fixture.launch(tester);
    await tester.pumpWidget(const SizedBox());
    final lan = FakeLan();
    final active = await services(lan: lan);
    await tester.pumpWidget(
      page(
        TVRemotePage(services: active, isTV: false, findTVs: () async => [tv]),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Living room'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('tv-pair-code')), '012345');
    await tester.pump();
    await tester.tap(find.text('Pair'));
    await tester.pumpAndSettle();
    expect(find.text('Playing story'), findsOneWidget);
    for (final label in [
      'Play',
      'Pause',
      '+10 s',
      'Next episode',
      'Volume +',
    ]) {
      await tester.ensureVisible(find.text(label));
      await tester.tap(find.text(label));
      await tester.pumpAndSettle();
    }
    expect(lan.commands, [
      'play:{}',
      'pause:{}',
      'seek:{"seconds":10}',
      'next:{}',
      'volume:{"volume":60}',
    ]);
    await tester.ensureVisible(find.text('Pick episode'));
    await tester.tap(find.text('Pick episode'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('4').last);
    await tester.pumpAndSettle();
    expect(lan.commands.last, 'episode:{"episode":4}');
    await tester.pumpWidget(const SizedBox());
    await active.close();
  });
  testWidgets(
    'phone remote hides restricted title and episode picker when AniList is off',
    (tester) async {
      await fixture.launch(tester);
      await tester.pumpWidget(const SizedBox());
      final lan = FakeLan()..adult = true;
      final active = await services(lan: lan);
      await active.tvRemote.pair(tv, '012345');
      await tester.pumpWidget(
        page(
          TVRemotePage(
            services: active,
            isTV: false,
            findTVs: () async => [tv],
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('Living room · Paired'));
      await tester.pumpAndSettle();
      expect(find.text('Hidden title'), findsNothing);
      expect(find.text('Playing on TV'), findsOneWidget);
      expect(find.text('Pick episode'), findsNothing);
      await tester.pumpWidget(const SizedBox());
      await active.close();
    },
  );
}
