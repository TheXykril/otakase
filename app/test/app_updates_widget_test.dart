import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/app_updates.dart';
import 'package:otakase/ui/app_updates.dart';
import 'package:otakase/ui/components.dart';

import 'app_updates_test.dart' show UpdateHost, manifest, asset, apkBytes;

void main() {
  for (final tv in [false, true]) {
    testWidgets(
      '${tv ? 'TV remote' : 'Phone'} checks, downloads and requests installation',
      (tester) async {
        final directory = (await tester.runAsync(
          () => Directory.systemTemp.createTemp('otakase-update-ui-'),
        ))!;
        final host = UpdateHost(directory);
        final updates = AppUpdates(
          platform: host,
          client: () => MockClient((request) async {
            if (request.url.host == 'api.github.com') {
              return http.Response(
                jsonEncode({
                  'assets': [asset(androidApk), asset(androidManifest)],
                }),
                200,
              );
            }
            if (request.url.path.endsWith('.json')) {
              return http.Response(jsonEncode(manifest()), 200);
            }
            return http.Response.bytes(apkBytes, 200);
          }),
        );
        await tester.binding.setSurfaceSize(
          Size(tv ? 960 : 360, tv ? 640 : 800),
        );
        addTearDown(() => tester.binding.setSurfaceSize(null));
        await tester.pumpWidget(
          AppLayout(
            isTV: tv,
            child: MaterialApp(
              theme: ThemeData.dark(),
              home: Scaffold(
                body: Align(
                  alignment: Alignment.topRight,
                  child: SizedBox(
                    width: tv ? 432 : 360,
                    child: ListView(
                      children: [
                        AppUpdatePanel(
                          updates: updates,
                          isTV: tv,
                          autoCheck: false,
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        Future<void> reach(UpdatePhase phase) async {
          for (var i = 0; i < 100 && updates.phase != phase; i++) {
            await tester.runAsync(
              () => Future<void>.delayed(const Duration(milliseconds: 10)),
            );
            await tester.pump();
          }
          expect(updates.phase, phase, reason: updates.error);
          await tester.pumpAndSettle();
        }

        if (tv) {
          await tester.sendKeyEvent(LogicalKeyboardKey.enter);
        } else {
          await tester.tap(find.byKey(const Key('check-update')));
        }
        await reach(UpdatePhase.available);
        expect(updates.phase, UpdatePhase.available, reason: updates.error);
        expect(find.byType(AppUpdatePanel), findsOneWidget);
        expect(find.text('Installed: 0.1.0 (build 3)'), findsOneWidget);
        expect(find.text('Download update'), findsOneWidget);
        if (tv) {
          await tester.sendKeyEvent(LogicalKeyboardKey.arrowUp);
          await tester.sendKeyEvent(LogicalKeyboardKey.enter);
        } else {
          await tester.tap(find.byKey(const Key('install-update')));
        }
        await reach(UpdatePhase.ready);
        expect(find.text('Install update'), findsOneWidget);
        expect(host.installs, 0);
        if (tv) {
          await tester.sendKeyEvent(LogicalKeyboardKey.enter);
        } else {
          await tester.tap(find.byKey(const Key('install-update')));
        }
        await tester.pumpAndSettle();
        expect(host.installs, 1);
        expect(
          find.text('Confirm the update in the Android installer.'),
          findsOneWidget,
        );
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
        updates.dispose();
        await tester.runAsync(() => directory.delete(recursive: true));
      },
    );
  }
}
