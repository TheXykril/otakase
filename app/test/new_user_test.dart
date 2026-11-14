import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/movies.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/data/trakt.dart';
import 'package:otakase/ui/show_page.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  test('episode names drop a repeated episode number', () {
    expect(
      episodeName("Episode 1 - The Journey's End", 1),
      "The Journey's End",
    );
    expect(episodeName('EP 03: Killing Magic', 3), 'Killing Magic');
    expect(episodeName('Episode 12', 12), '');
    // Another number is part of the name and stays.
    expect(episodeName('Episode 2 - Part Two', 3), 'Episode 2 - Part Two');
    expect(
      episodeName('I Tried Using the Internet!', 1),
      'I Tried Using the Internet!',
    );
  });

  test('a failed film says why', () {
    expect(noCopyMessage(0, 0), contains('languages'));
    expect(noCopyMessage(3, 3), contains("Couldn't reach"));
    expect(noCopyMessage(3, 1), contains("didn't answer"));
    expect(noCopyMessage(3, 0), startsWith('No source has this yet'));
    expect(
      noCopyMessage(
        2,
        2,
        reasons: ['English: no server played', 'Lithuanian: timed out'],
      ),
      endsWith('\nEnglish: no server played\nLithuanian: timed out'),
    );
  });

  test('a failed source says why in a few words', () {
    expect(failureReason(const CoreFailure('timeout', 'x')), 'timed out');
    expect(
      failureReason(const CoreFailure('no_stream', 'x')),
      'no server played',
    );
    expect(
      failureReason(
        const CoreFailure(
          'provider_error',
          'Source request failed (certificate not trusted by this device).',
        ),
      ),
      'certificate not trusted by this device',
    );
    expect(failureReason(const CoreFailure('internal', 'Broke.')), 'failed');
  });

  test('errors read without Dart prefixes', () {
    expect(errorMessage(StateError('Bad state: Connect it.')), 'Connect it.');
    expect(errorMessage(StateError('Connect it.')), 'Connect it.');
    expect(errorMessage(Exception('Gone.')), 'Gone.');
    expect(
      errorMessage(const CoreFailure('no_stream', 'Nothing.')),
      'Nothing.',
    );
  });

  test('language lists read as names', () {
    expect(
      languageListLabel('japanese,chinese,english'),
      'Japanese, Chinese, English',
    );
    expect(languageListLabel(''), 'Off');
  });

  test('Japanese animation counts as anime on Trakt', () {
    expect(
      const MediaTitle(
        kind: 'show',
        trakt: 1,
        title: 'A',
        genres: ['anime'],
      ).isAnime,
      isTrue,
    );
    expect(
      const MediaTitle(
        kind: 'movie',
        trakt: 2,
        title: 'B',
        genres: ['animation'],
        language: 'ja',
      ).isAnime,
      isTrue,
    );
    expect(
      const MediaTitle(
        kind: 'movie',
        trakt: 3,
        title: 'C',
        genres: ['animation'],
        language: 'en',
      ).isAnime,
      isFalse,
    );
  });

  test('a profile without accounts keeps its own list', () async {
    SharedPreferences.setMockInitialValues({});
    final store = AppStore(await SharedPreferences.getInstance());
    const a = Anime(id: 1, title: 'First');
    const b = Anime(id: 2, title: 'Second');
    expect(store.isSet('Content'), isFalse);
    await store.setLocalStatus(a, 'PLANNING');
    await store.setLocalStatus(b, 'PLANNING');
    await store.setLocalStatus(a, 'CURRENT');
    expect(store.localList('PLANNING').map((s) => s.id), [2]);
    expect(store.localList('CURRENT').map((s) => s.id), [1]);
    expect(store.text('LocalStatus.${a.key}'), 'CURRENT');
    await store.set('Content', 'both');
    expect(store.isSet('Content'), isTrue);
  });
}
