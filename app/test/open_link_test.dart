import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/desktop/open_link.dart';

void main() {
  final runs = <List<String>>[];
  setUp(() {
    runs.clear();
    debugLinkOpener = (executable, arguments) async {
      runs.add([executable, ...arguments]);
      return ProcessResult(1, 0, '', '');
    };
  });
  tearDown(() => debugLinkOpener = Process.run);

  test('web links open in the default browser', () async {
    expect(await openInBrowser(Uri.parse('https://anilist.co/x?a=1')), isTrue);
    expect(runs.single.last, 'https://anilist.co/x?a=1');
  });

  test('other schemes are never handed to the system', () async {
    for (final link in ['file:///etc/passwd', 'otakase://login', 'smb://x']) {
      expect(await openInBrowser(Uri.parse(link)), isFalse);
    }
    expect(runs, isEmpty);
  });

  test('a missing opener reports failure', () async {
    debugLinkOpener = (executable, arguments) =>
        throw ProcessException(executable, arguments, 'not found');
    expect(await openInBrowser(Uri.parse('https://anilist.co')), isFalse);
  });
}
