import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

// Corner radii come from KageRadius in lib/ui/tokens.dart. A number typed
// straight into a screen is how the app ended up with seventeen radii.
void main() {
  test('corner radii come from the tokens', () {
    final literal = RegExp(r'circular\(\s*\d');
    final focusRadius = RegExp(r'^\s*radius: \d');
    final found = <String>[];
    for (final file in Directory('lib').listSync(recursive: true)) {
      if (file is! File || !file.path.endsWith('.dart')) continue;
      if (file.path.endsWith('tokens.dart')) continue;
      final lines = file.readAsLinesSync();
      for (var i = 0; i < lines.length; i++) {
        final line = lines[i];
        final avatar = i > 0 && lines[i - 1].contains('CircleAvatar(');
        if (literal.hasMatch(line) || (focusRadius.hasMatch(line) && !avatar)) {
          found.add('${file.path}:${i + 1}: ${line.trim()}');
        }
      }
    }
    expect(found, isEmpty, reason: 'use a KageRadius role instead');
  });
}
