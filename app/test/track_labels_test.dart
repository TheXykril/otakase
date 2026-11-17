import 'package:flutter/material.dart';

import 'dart:ui' show Tristate;

import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/player/track_labels.dart';
import 'package:otakase/ui/components.dart';

// The Subtitles & audio menu: every row readable, no two alike, the playing
// track marked.
void main() {
  test('special and unlabelled tracks read as words', () {
    expect(trackLabel('auto', null, null, kind: 'Audio'), 'Automatic');
    expect(trackLabel('no', null, null, kind: 'Audio'), 'Off');
    expect(trackLabel('1', null, null, kind: 'Audio'), 'Audio 1');
    // A dub's lone audio track that names nothing is still English.
    expect(
      trackLabel('1', null, null, kind: 'Audio', fallback: 'english'),
      'English',
    );
    expect(trackLabel('', null, null, kind: 'Subtitles'), 'Subtitles');
  });

  test('language codes become names, titles keep their language', () {
    expect(trackLabel('2', null, 'eng', kind: 'Audio'), 'English');
    expect(trackLabel('2', null, 'jpn', kind: 'Audio'), 'Japanese');
    expect(trackLabel('3', 'English', 'eng', kind: 'Subtitles'), 'English');
    expect(
      trackLabel('3', 'Signs & Songs', 'eng', kind: 'Subtitles'),
      'Signs & Songs · English',
    );
    expect(
      trackLabel('3', 'English (Forced)', 'en', kind: 'Subtitles'),
      'English (Forced)',
    );
    expect(languageLabel('lithuanian'), 'Lithuanian');
    expect(languageLabel('pt-BR'), 'Portuguese');
  });

  test('rows that would read the same are numbered', () {
    expect(distinctLabels(['Off', 'English', 'English', 'Japanese']), [
      'Off',
      'English',
      'English 2',
      'Japanese',
    ]);
  });

  testWidgets('the chosen row carries a check and says it is selected', (
    tester,
  ) async {
    final handle = tester.ensureSemantics();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Column(
            children: [
              ChoiceRow(title: const Text('Off'), onTap: () {}),
              ChoiceRow(
                title: const Text('English'),
                onTap: () {},
                selected: true,
              ),
            ],
          ),
        ),
      ),
    );
    expect(find.byIcon(LucideIcons.check), findsOneWidget);
    expect(
      tester.getSemantics(find.text('English')).flagsCollection.isSelected,
      Tristate.isTrue,
    );
    expect(
      find.descendant(
        of: find.widgetWithText(ChoiceRow, 'Off'),
        matching: find.byIcon(LucideIcons.check),
      ),
      findsNothing,
    );
    handle.dispose();
  });

  test('quality choices read as words and map back to values', () async {
    expect(qualityLabel('best'), 'Best available');
    expect(qualityLabel('worst'), 'Lowest');
    expect(qualityLabel('1080'), '1080p');
    List<String>? shown;
    String? marked;
    final picked = await chooseQuality(
      (labels, selected) async {
        shown = labels;
        marked = selected;
        return '720p';
      },
      ['best', '1080', '720', 'worst', 'Try another copy'],
      'best',
    );
    expect(shown, [
      'Best available',
      '1080p',
      '720p',
      'Lowest',
      'Try another copy',
    ]);
    expect(marked, 'Best available');
    expect(picked, '720');
  });
}
