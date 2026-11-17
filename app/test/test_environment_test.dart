import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/desktop/window_frame.dart';
import 'package:otakase/host.dart';

import 'test_environment.dart';

void main() {
  test('a tiling desktop session is taken out of the test environment', () {
    final env = testEnvironment({
      'HOME': '/home/viewer',
      'FLUTTER_TEST': 'true',
      'HYPRLAND_INSTANCE_SIGNATURE': 'abc',
      'XDG_SESSION_DESKTOP': 'Hyprland',
      'XDG_CURRENT_DESKTOP': 'Hyprland',
      'WAYLAND_DISPLAY': 'wayland-1',
      'OTAKASE_CORE': '/tmp/core',
    });
    expect(env, {'HOME': '/home/viewer', 'FLUTTER_TEST': 'true'});
    expect(isTilingWindowManager(env), isFalse);
  });

  test('every test file runs with the pinned environment and time zone', () {
    for (final key in sessionVariables) {
      expect(Host.environment.containsKey(key), isFalse, reason: key);
    }
    expect(Host.timeZoneName, 'UTC');
  });
}
