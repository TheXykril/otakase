import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/external_subtitles.dart';

void main() {
  List<String> order(List<String> labels) => dialogueSubtitleOrder(
    labels,
    language: (_) => 'en',
    label: (label) => label,
  ).toList();

  test('episode 9 track ordering selects full dialogue ahead of forced and CC', () {
    // Labels observed in the reported episode; no remote URLs or subtitle text.
    expect(
      order(['English (Forced)', 'English (Track 4 (ENG))', 'English (CC)']),
      ['English (Track 4 (ENG))', 'English (CC)', 'English (Forced)'],
    );
  });
  test('a full external track outranks an embedded forced track', () {
    final tracks = [
      (label: 'Signs & Songs', embedded: true),
      (label: 'English', embedded: false),
    ];
    expect(
      dialogueSubtitleOrder(
        tracks,
        language: (_) => 'eng',
        label: (t) => t.label,
      ).first.embedded,
      isFalse,
    );
  });
  test('equal dialogue tracks retain source order and signs-only remains a fallback', () {
    expect(
      order(['English SDH', 'English', 'English full', 'English Forced']),
      ['English', 'English full', 'English SDH', 'English Forced'],
    );
    expect(order(['English Signs and Songs']), ['English Signs and Songs']);
    expect(order([]), isEmpty);
  });
  test('classification uses both language and title metadata', () {
    final tracks = [
      (language: 'English (Forced)', label: 'Track 1'),
      (language: 'eng', label: 'English'),
    ];
    expect(
      dialogueSubtitleOrder(
        tracks,
        language: (t) => t.language,
        label: (t) => t.label,
      ).first.label,
      'English',
    );
  });
}
