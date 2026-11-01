import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/desktop/desktop_core.dart';
import 'package:otakase/desktop/window_frame.dart';

void main() {
  final calls = <String>[];
  var maximized = false;

  setUp(() {
    calls.clear();
    maximized = false;
    debugDesktopOverride = true;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('window_manager'), (
          call,
        ) async {
          calls.add(call.method);
          switch (call.method) {
            case 'isMaximized':
              return maximized;
            case 'maximize':
              maximized = true;
            case 'unmaximize':
              maximized = false;
          }
          return null;
        });
  });

  tearDown(() {
    debugDesktopOverride = null;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('window_manager'), null);
  });

  Widget frame({Future<void> Function()? onClose}) => MaterialApp(
    home: Scaffold(
      body: WindowDragArea(child: WindowButtons(onClose: onClose)),
    ),
  );

  testWidgets('window buttons minimise, maximise and close', (tester) async {
    var closed = 0;
    await tester.pumpWidget(frame(onClose: () async => closed++));
    await tester.pump();
    expect(find.byKey(const Key('window-minimize')), findsOneWidget);

    await tester.tap(find.byKey(const Key('window-minimize')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('window-maximize')));
    await tester.pumpAndSettle();
    expect(calls, ['isMaximized', 'minimize', 'isMaximized', 'maximize']);

    await tester.tap(find.byKey(const Key('window-maximize')));
    await tester.pumpAndSettle();
    expect(calls.last, 'unmaximize');

    await tester.tap(find.byKey(const Key('window-close')));
    expect(closed, 1);
    expect(calls, isNot(contains('close')));
  });

  testWidgets('dragging empty space moves the window', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 60,
            child: WindowDragArea(
              child: Row(children: [const Spacer(), WindowButtons()]),
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.drag(
      find.byKey(const Key('window-drag-area')),
      const Offset(40, 0),
      warnIfMissed: false,
    );
    await tester.pumpAndSettle();
    expect(calls, contains('startDragging'));
  });

  testWidgets('nothing is drawn outside desktop builds', (tester) async {
    debugDesktopOverride = false;
    await tester.pumpWidget(frame());
    expect(find.byKey(const Key('window-close')), findsNothing);
    expect(calls, isEmpty);
  });
}
