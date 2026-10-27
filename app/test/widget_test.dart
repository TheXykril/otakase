import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/core.dart';
import 'package:otakase/main.dart';

void main() {
  testWidgets('TV search and episode list work with D-pad and OK', (
    tester,
  ) async {
    final calls = <String>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(CoreClient.channel, (call) async {
          if (call.method == 'start') {
            calls.add(call.arguments as String);
            return calls.length;
          }
          if (call.method == 'result') {
            final request = jsonDecode(calls[(call.arguments as int) - 1]);
            return jsonEncode({
              'version': 1,
              'data': request['operation'] == 'search'
                  ? {
                      'shows': [
                        {
                          'id': '52991',
                          'title': 'Frieren',
                          'label': 'Frieren (TV, 2023)',
                        },
                      ],
                    }
                  : {
                      'episodes': ['1', '2'],
                    },
            });
          }
          return null;
        });
    addTearDown(
      () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(CoreClient.channel, null),
    );
    await tester.pumpWidget(SpikeApp(core: CoreClient(), isTV: true));
    await tester.pumpAndSettle();
    Key? focusedButton() {
      Key? key;
      FocusManager.instance.primaryFocus?.context?.visitAncestorElements((
        element,
      ) {
        if (element.widget is OutlinedButton) {
          key = element.widget.key;
          return false;
        }
        return true;
      });
      return key;
    }

    Future<void> downTo(Key key) async {
      for (var i = 0; i < 6 && focusedButton() != key; i++) {
        await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
        await tester.pumpAndSettle();
      }
      expect(focusedButton(), key);
    }

    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.pumpAndSettle();
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.pumpAndSettle();
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowLeft);
    await tester.pumpAndSettle();
    expect(focusedButton(), const Key('search-button'));
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pumpAndSettle();
    expect(find.text('Frieren (TV, 2023)'), findsOneWidget);
    await downTo(const ValueKey('show-52991'));
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pumpAndSettle();
    expect(find.text('Play episode 1'), findsOneWidget);
    expect(find.text('Play episode 2'), findsOneWidget);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.text('Provider playback probe'), findsOneWidget);
  });

  test('typed errors retain the error code', () {
    expect(
      const CoreFailure('cancelled', 'Request cancelled.').toString(),
      'cancelled: Request cancelled.',
    );
  });
}
