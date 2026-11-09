import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/services.dart';
import 'package:otakase/data/community_extras.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/tracking.dart';
import 'package:otakase/ui/community_extras_page.dart';
import 'package:otakase/ui/playback_report.dart';
import 'package:otakase/ui/account_sync_status.dart';
import 'package:otakase/ui/components.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_test.dart' show MemoryTokens, account, json;
import 'widget_test.dart' as fixture;

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test(
    'sync status retains failed titles and resets for another account',
    () async {
      var fail = true;
      final gate = Completer<void>();
      final auth = AuthService(
        storage: MemoryTokens(),
        client: MockClient((r) async {
          final data = jsonDecode(r.body) as Map;
          if (data['variables']['id'] == 42 && fail) {
            await gate.future;
            throw Exception('offline');
          }
          return json({
            'data': {
              'SaveMediaListEntry': {'id': 1},
            },
          });
        }),
      );
      auth.accounts['AniList'] = account('AniList');
      final tracking = TrackingService(auth);
      addTearDown(tracking.dispose);
      addTearDown(auth.dispose);
      const first = Anime(id: 42, title: 'First');
      const second = Anime(id: 43, title: 'Second');
      expect(tracking.progressStatus('AniList'), contains('no updates sent'));
      final write = tracking.update(first, progress: 1);
      final failed = expectLater(write, throwsA(isA<LoginFailure>()));
      await Future<void>.delayed(Duration.zero);
      expect(tracking.progressStatus('AniList'), contains('Sending update'));
      gate.complete();
      await failed;
      expect(tracking.needsAttention('AniList'), isTrue);
      await tracking.update(second, progress: 1);
      expect(tracking.progressStatus('AniList'), contains('needs attention'));
      fail = false;
      await tracking.update(first, progress: 1);
      expect(tracking.progressStatus('AniList'), contains('Last update sent'));
      await auth.logout('AniList');
      auth.accounts['AniList'] = Account(
        service: 'AniList',
        username: 'Other',
        userId: 999,
        token: 'other-token',
        expires: DateTime(2100),
      );
      expect(tracking.progressStatus('AniList'), contains('no updates sent'));
    },
  );

  testWidgets('Accounts card updates while a tracker write is in flight', (
    tester,
  ) async {
    final gate = Completer<void>();
    final auth = AuthService(
      storage: MemoryTokens(),
      client: MockClient((_) async {
        await gate.future;
        return json({
          'data': {
            'SaveMediaListEntry': {'id': 1},
          },
        });
      }),
    );
    auth.accounts['AniList'] = account('AniList');
    final services = AppServices(
      catalog: fixture.TestCatalog(),
      store: AppStore(await SharedPreferences.getInstance()),
      core: CoreClient(),
      auth: auth,
    );
    addTearDown(services.close);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: AccountSyncStatus(services: services)),
      ),
    );
    expect(
      find.text('Progress saved locally · view sync status'),
      findsOneWidget,
    );
    final update = services.tracking.update(
      const Anime(id: 42, title: 'Story'),
      progress: 2,
    );
    await tester.pump();
    expect(find.textContaining('Syncing…'), findsOneWidget);
    gate.complete();
    await update;
    await tester.pump();
    expect(find.text('Last update sent to AniList'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });

  test('playback report keeps position without addresses, credentials or source names', () {
    final report = playbackReportDetails(
      provider: 'megavid',
      position: const Duration(seconds: 312),
      problem: 'Failed https://user:pass@sub.example/secret/path?token=secret Bearer private-token api_key=hidden; lookup cdn.example.to: no such host; animex gave no links',
    );
    expect(report, contains('312 seconds'));
    expect(report, contains('Playback source: Source 2'));
    expect(report, contains('Source 3 gave no links'));
    for (final secret in [
      'user:pass',
      'sub.example',
      'cdn.example.to',
      '/secret/path',
      'token=secret',
      'private-token',
      'api_key=hidden',
      'megavid',
      'animex',
    ]) {
      expect(report, isNot(contains(secret)));
    }
  });

  test('a film report numbers its source and keeps the language', () {
    expect(reportSource('8Filmai · Lithuanian'), 'Movie source 2 · Lithuanian');
    expect(reportSource('somethingnew'), 'Other source');
  });

  testWidgets(
    'playback report opens prefilled for review without sending, and details can be excluded',
    (tester) async {
      final services = AppServices(
        catalog: fixture.TestCatalog(),
        store: AppStore(await SharedPreferences.getInstance()),
        core: CoreClient(),
        auth: AuthService(
          storage: MemoryTokens(),
          client: MockClient((_) async => json({})),
        ),
      );
      addTearDown(services.close);
      String? copied;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(SystemChannels.platform, (call) async {
            if (call.method == 'Clipboard.setData') {
              copied = (call.arguments as Map)['text'] as String;
            }
            return null;
          });
      addTearDown(
        () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(SystemChannels.platform, null),
      );
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: PlaybackReportButton(
              services: services,
              isTV: false,
              title: 'Story',
              episode: 'Episode 3',
              problem: 'Subtitle host unavailable',
              position: const Duration(seconds: 32),
              provider: 'example',
            ),
          ),
        ),
      );
      await tester.tap(find.text('Report playback problem'));
      await tester.pumpAndSettle();
      expect(find.byType(ReportPage), findsOneWidget);
      expect(find.text('Playback problem: Story'), findsOneWidget);
      expect(find.text('Story · Episode 3'), findsOneWidget);
      expect(find.textContaining('Subtitle host unavailable'), findsOneWidget);
      await tester.scrollUntilVisible(
        find.text('Send report'),
        200,
        scrollable: find
            .descendant(
              of: find.byType(ReportPage),
              matching: find.byType(Scrollable),
            )
            .first,
      );
      final send = tester.widget<AppButton>(
        find.widgetWithText(AppButton, 'Send report'),
      );
      expect(
        send.onPressed,
        isNull,
        reason: 'Unlinked reports can be copied, not sent',
      );
      await tester.ensureVisible(find.text('Copy report'));
      await tester.tap(find.text('Copy report'));
      await tester.pumpAndSettle();
      expect(copied, contains('Position: 32 seconds'));
      await tester.ensureVisible(find.byType(SwitchListTile));
      await tester.tap(find.byType(SwitchListTile));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('Copy report'));
      await tester.tap(find.text('Copy report'));
      await tester.pumpAndSettle();
      expect(copied, contains('Episode 3'));
      expect(copied, isNot(contains('Subtitle host unavailable')));
      expect(copied, isNot(contains('Otakase app')));
      await tester.pumpWidget(const SizedBox());
    },
  );
}
