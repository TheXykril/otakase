import 'dart:io';
import 'dart:ui';

import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/desktop/tiling.dart';

void main() {
  const size = Size(680, 560);

  test('finds the tiling compositor from the environment', () {
    expect(
      tilerFrom({'HYPRLAND_INSTANCE_SIGNATURE': 'abc'}, linux: true),
      Tiler.hyprland,
    );
    expect(tilerFrom({'SWAYSOCK': '/run/sway'}, linux: true), Tiler.sway);
    expect(tilerFrom({'I3SOCK': '/run/i3'}, linux: true), Tiler.i3);
    expect(tilerFrom({}, linux: true), isNull);
    expect(
      tilerFrom({'HYPRLAND_INSTANCE_SIGNATURE': 'abc'}, linux: false),
      isNull,
    );
  });

  test('Hyprland commands target this window by process id', () {
    expect(hyprlandLuaDispatches(42, size: size), [
      'hl.dsp.window.float({ action = "set", window = "pid:42" })',
      'hl.dsp.window.resize({ x = 680, y = 560, window = "pid:42" })',
      'hl.dsp.window.center({ window = "pid:42" })',
      'hl.dsp.focus({ window = "pid:42" })',
    ]);
    expect(hyprlandLuaDispatches(42, size: null), [
      'hl.dsp.window.float({ action = "unset", window = "pid:42" })',
    ]);
    expect(
      hyprlandLegacyBatch(42, size: size),
      'dispatch setfloating pid:42 ; '
      'dispatch resizewindowpixel exact 680 560,pid:42 ; '
      'dispatch focuswindow pid:42 ; dispatch centerwindow',
    );
    expect(hyprlandLegacyBatch(42, size: null), 'dispatch settiled pid:42');
    expect(
      swayCommand(42, size: size),
      '[pid=42] floating enable, resize set 680 560, move position center, focus',
    );
  });

  test('Hyprland raises the popup above other floating windows', () {
    expect(
      hyprlandLuaDispatches(42, size: size, raise: true).last,
      'hl.dsp.window.alter_zorder({ mode = "top", window = "pid:42" })',
    );
    expect(
      hyprlandLegacyBatch(42, size: size, raise: true),
      endsWith('dispatch alterzorder top,pid:42'),
    );
  });

  test(
    'captures the focused workspace and moves the popup before centering',
    () async {
      Future<ProcessResult> respond(
        String command,
        List<String> args,
      ) async => ProcessResult(
        0,
        0,
        command == 'hyprctl'
            ? '{"id": 4}'
            : '[{"name":"one","focused":false},{"name":"work","focused":true}]',
        '',
      );
      final hypr = await focusedTilerWorkspace(
        env: {'HYPRLAND_INSTANCE_SIGNATURE': 'x'},
        linux: true,
        run: respond,
      );
      final sway = await focusedTilerWorkspace(
        env: {'SWAYSOCK': 'x'},
        linux: true,
        run: respond,
      );
      expect(hypr, '4');
      expect(sway, 'work');
      expect(
        hyprlandLuaDispatches(42, size: size, workspace: hypr)[1],
        contains('workspace = "4"'),
      );
      expect(
        hyprlandLegacyBatch(42, size: size, workspace: hypr),
        contains('movetoworkspacesilent 4,pid:42'),
      );
      expect(
        swayCommand(42, size: size, workspace: sway),
        contains('move container to workspace "work", resize set'),
      );
    },
  );

  group('placeOnTiler', () {
    final calls = <List<String>>[];
    setUp(calls.clear);
    CommandRunner answer(String Function(List<String> args) out) =>
        (exe, args) async {
          calls.add([exe, ...args]);
          return ProcessResult(0, 0, out(args), '');
        };

    test('Lua Hyprland gets one dispatch per step', () async {
      final ok = await placeOnTiler(
        size: size,
        env: {'HYPRLAND_INSTANCE_SIGNATURE': 'x'},
        linux: true,
        processId: 7,
        run: answer((_) => 'ok'),
      );
      expect(ok, isTrue);
      expect(calls, hasLength(4));
      expect(
        calls.every((c) => c[0] == 'hyprctl' && c[1] == 'dispatch'),
        isTrue,
      );
    });

    test('older Hyprland falls back to the classic dispatchers', () async {
      final ok = await placeOnTiler(
        size: size,
        env: {'HYPRLAND_INSTANCE_SIGNATURE': 'x'},
        linux: true,
        processId: 7,
        run: answer(
          (args) => args.first == 'dispatch' ? 'Invalid dispatcher' : 'ok\nok',
        ),
      );
      expect(ok, isTrue);
      expect(calls.last, [
        'hyprctl',
        '--batch',
        hyprlandLegacyBatch(7, size: size),
      ]);
    });

    test('tries again while the window is not mapped yet', () async {
      var tries = 0;
      final ok = await placeOnTiler(
        size: size,
        env: {'HYPRLAND_INSTANCE_SIGNATURE': 'x'},
        linux: true,
        processId: 7,
        wait: Duration.zero,
        run: (exe, args) async {
          calls.add(args);
          tries++;
          return ProcessResult(0, 0, tries < 3 ? 'window not found' : 'ok', '');
        },
      );
      expect(ok, isTrue);
      expect(tries, 3 + 3); // two misses, then the four steps
    });

    test('does nothing outside a tiling compositor', () async {
      final ok = await placeOnTiler(
        size: size,
        env: const {},
        linux: true,
        run: answer((_) => 'ok'),
      );
      expect(ok, isFalse);
      expect(calls, isEmpty);
    });

    test('a missing hyprctl is not an error', () async {
      final ok = await placeOnTiler(
        size: size,
        env: {'HYPRLAND_INSTANCE_SIGNATURE': 'x'},
        linux: true,
        run: (exe, args) => throw const ProcessException('hyprctl', []),
      );
      expect(ok, isFalse);
    });

    test('sway floats by criteria', () async {
      await placeOnTiler(
        size: size,
        env: {'SWAYSOCK': 's'},
        linux: true,
        processId: 7,
        run: answer((_) => '[ { "success": true } ]'),
      );
      expect(calls.single, ['swaymsg', swayCommand(7, size: size)]);
    });
  });
}
