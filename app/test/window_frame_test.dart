import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/desktop/desktop_core.dart';
import 'package:otakase/desktop/launcher.dart';
import 'package:otakase/desktop/window_frame.dart';

void main() {
  final calls = <String>[];
  final requests = <MethodCall>[];
  var maximized = false;

  setUp(() {
    calls.clear();
    requests.clear();
    maximized = false;
    debugTilingOverride = false;
    debugDesktopOverride = true;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('window_manager'), (
          call,
        ) async {
          calls.add(call.method);
          requests.add(call);
          switch (call.method) {
            case 'isMinimized':
            case 'isFullScreen':
              return false;
            case 'getBounds':
              return {'x': 0.0, 'y': 0.0, 'width': 680.0, 'height': 560.0};
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
    debugTilingOverride = null;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('window_manager'), null);
  });

  Widget frame({Future<void> Function()? onClose}) => MaterialApp(
    home: Scaffold(
      body: WindowDragArea(child: WindowButtons(onClose: onClose)),
    ),
  );

  testWidgets('launcher fades in and releases window controls for playback', (
    tester,
  ) async {
    const screen = MethodChannel('dev.leanflutter.plugins/screen_retriever');
    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    const origin = {'dx': 0.0, 'dy': 0.0};
    const display = {
      'id': 'primary',
      'size': {'width': 1440.0, 'height': 900.0},
      'visiblePosition': origin,
    };
    messenger.setMockMethodCallHandler(
      screen,
      (call) async => switch (call.method) {
        'getPrimaryDisplay' => display,
        'getAllDisplays' => {
          'displays': [display],
        },
        'getCursorScreenPoint' => origin,
        _ => throw MissingPluginException(call.method),
      },
    );
    addTearDown(() => messenger.setMockMethodCallHandler(screen, null));

    List<dynamic> values(String method, String key) => requests
        .where((call) => call.method == method)
        .map((call) => call.arguments[key])
        .toList();

    await prepareLauncherWindow();
    expect(calls, isNot(contains('show')));
    expect(values('setOpacity', 'opacity'), [0.0]);

    final reveal = revealLauncherWindow(reduceMotion: false);
    await tester.pump();
    for (var frame = 0; frame < 9; frame++) {
      await tester.pump(const Duration(milliseconds: 16));
    }
    await reveal;

    final opacity = values('setOpacity', 'opacity');
    expect(opacity, hasLength(greaterThan(2)));
    expect(opacity, orderedEquals([...opacity]..sort()));
    expect(opacity.last, 1.0);
    expect(
      calls,
      containsAllInOrder(['setOpacity', 'show', 'focus', 'setOpacity']),
    );

    await resizeDesktopWindow(launcher: false, player: true);
    await resizeDesktopWindow(launcher: true);
    await resizeDesktopWindow(launcher: false);
    expect(values('setResizable', 'isResizable'), [false, true, false, true]);
    expect(values('setAlwaysOnTop', 'isAlwaysOnTop'), [
      true,
      false,
      true,
      false,
    ]);
    expect(values('setSkipTaskbar', 'isSkipTaskbar'), [
      true,
      false,
      true,
      false,
    ]);
    expect(values('setBounds', 'width').whereType<double>(), [
      launcherSize.width,
      launcherPlayerSize.width,
      launcherSize.width,
      fullAppSize.width,
    ]);

    requests.clear();
    await revealLauncherWindow(reduceMotion: true);
    expect(values('setOpacity', 'opacity'), [1.0]);
  });

  testWidgets('window buttons minimise, maximise and close', (tester) async {
    var closed = 0;
    await tester.pumpWidget(frame(onClose: () async => closed++));
    await tester.pump();
    expect(find.byKey(const Key('window-minimize')), findsOneWidget);

    await tester.tap(find.byKey(const Key('window-minimize')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('window-maximize')));
    await tester.pumpAndSettle();
    expect(calls, containsAllInOrder(['minimize', 'isMaximized', 'maximize']));

    await tester.tap(find.byKey(const Key('window-maximize')));
    await tester.pumpAndSettle();
    expect(calls.last, 'unmaximize');

    await tester.tap(find.byKey(const Key('window-close')));
    expect(closed, 1);
    expect(calls, isNot(contains('close')));
  });

  test('tiling window managers are spotted from the environment', () {
    expect(isTilingWindowManager({'HYPRLAND_INSTANCE_SIGNATURE': 'x'}), isTrue);
    expect(isTilingWindowManager({'SWAYSOCK': '/run/sway'}), isTrue);
    expect(isTilingWindowManager({'XDG_CURRENT_DESKTOP': 'niri'}), isTrue);
    expect(isTilingWindowManager({'DESKTOP_SESSION': 'i3'}), isTrue);
    expect(isTilingWindowManager({'XDG_CURRENT_DESKTOP': 'KDE'}), isFalse);
    expect(
      isTilingWindowManager({'XDG_CURRENT_DESKTOP': 'ubuntu:GNOME'}),
      isFalse,
    );
    expect(isTilingWindowManager({}), isFalse);
  });

  test('window buttons follow the system', () {
    expect(
      windowButtonStyleFor(windows: true, env: {}),
      WindowButtonStyle.windows,
    );
    expect(
      windowButtonStyleFor(windows: false, env: {'XDG_CURRENT_DESKTOP': 'KDE'}),
      WindowButtonStyle.kde,
    );
    expect(
      windowButtonStyleFor(
        windows: false,
        env: {'XDG_CURRENT_DESKTOP': 'ubuntu:GNOME'},
      ),
      WindowButtonStyle.gnome,
    );
    expect(
      windowButtonStyleFor(
        windows: false,
        env: {'HYPRLAND_INSTANCE_SIGNATURE': 'x'},
      ),
      WindowButtonStyle.tiling,
    );
  });

  testWidgets('tiling window managers only get close', (tester) async {
    debugTilingOverride = true;
    addTearDown(() => debugTilingOverride = null);
    await tester.pumpWidget(frame());
    await tester.pump();
    expect(find.byKey(const Key('window-minimize')), findsNothing);
    expect(find.byKey(const Key('window-maximize')), findsNothing);
    expect(find.byKey(const Key('window-close')), findsOneWidget);
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

  testWidgets('Esc goes back one page on desktop', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => DesktopFrame(child: child!),
        home: Builder(
          builder: (context) => TextButton(
            autofocus: true,
            onPressed: () => Navigator.push(
              context,
              MaterialPageRoute<void>(
                builder: (_) => const Scaffold(
                  body: Focus(autofocus: true, child: Text('show page')),
                ),
              ),
            ),
            child: const Text('open'),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    expect(find.text('show page'), findsOneWidget);
    await tester.sendKeyEvent(LogicalKeyboardKey.escape);
    await tester.pumpAndSettle();
    expect(find.text('show page'), findsNothing);
    expect(find.text('open'), findsOneWidget);
  });
}
