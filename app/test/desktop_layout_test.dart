import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/ui/components.dart';

void main() {
  test('desktop shelf cards fill the row with no space left over', () {
    for (final width in [1100.0, 1440.0, 1920.0, 2560.0]) {
      final card = fittedCardWidth(width, 200, padding: 41, gap: 26);
      expect(card, greaterThanOrEqualTo(200));
      final count = ((width - 82 + 26) / (card + 26)).round();
      expect(count * card + (count - 1) * 26, closeTo(width - 82, .001));
    }
  });

  test('a narrow row still shows one card at full width', () {
    expect(fittedCardWidth(240, 200, padding: 20, gap: 12), 200);
    expect(fittedCardWidth(150, 200, padding: 20, gap: 12), 110);
  });
}
