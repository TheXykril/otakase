import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/ui/tokens.dart';

// Every named text size stays at or above its screen's legibility floor;
// short phone kickers are the one allowed exception.
void main() {
  test('text roles respect each screen floor', () {
    for (final role in TextRole.values) {
      for (final screen in TypeScreen.values) {
        final size = KageType.size(role, screen);
        final floor = role == TextRole.kicker && screen == TypeScreen.phone
            ? KageType.phoneKicker
            : KageType.floor[screen]!;
        expect(size, greaterThanOrEqualTo(floor), reason: '$role on $screen');
      }
    }
  });

  test('desktop reads smaller than a TV', () {
    expect(
      KageType.floor[TypeScreen.desktop],
      lessThan(KageType.floor[TypeScreen.tv]!),
    );
    expect(KageType.launcher, lessThan(KageType.floor[TypeScreen.desktop]!));
  });
}
