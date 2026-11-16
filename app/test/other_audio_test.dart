import 'package:flutter_test/flutter_test.dart';
import 'package:media_kit/media_kit.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/movies.dart';
import 'package:otakase/player/other_audio.dart';
import 'package:otakase/ui/movies_pages.dart';

import 'movies_test.dart'
    show MovieCore, film, freshStore, offlineTrakt, series;

void main() {
  test(
    'other languages list sources that match, never the playing one',
    () async {
      final store = await freshStore();
      final core = MovieCore();
      final movies = MovieService(
        trakt: offlineTrakt(),
        core: core,
        store: store,
      );
      expect(
        await movies.otherLanguages(
          film,
          playing: 'english',
          scope: RequestScope(),
        ),
        ['lithuanian'],
      );
      // Only matches are looked up; no stream is asked for.
      expect(
        core.calls.where((c) => c['operation'] == 'movie_resolve'),
        isEmpty,
      );
      expect(
        core.calls
            .where((c) => c['operation'] == 'movie_match')
            .map((c) => c['provider']),
        ['8filmai'],
      );
      expect(
        await movies.otherLanguages(
          film,
          playing: 'lithuanian',
          scope: RequestScope(),
        ),
        ['english'],
      );
    },
  );

  test('a language no source carries is left out', () async {
    final store = await freshStore();
    final core = MovieCore()..missing.addAll({'8filmai', 'filmukas'});
    final movies = MovieService(
      trakt: offlineTrakt(),
      core: core,
      store: store,
    );
    expect(
      await movies.otherLanguages(
        series,
        season: 2,
        playing: 'english',
        scope: RequestScope(),
      ),
      isEmpty,
    );
  });

  test("switching to a language tries only that language's sources", () async {
    final store = await freshStore();
    final core = MovieCore();
    final movies = MovieService(
      trakt: offlineTrakt(),
      core: core,
      store: store,
    );
    final p = await movies.resolve(
      film,
      scope: RequestScope(),
      status: (_) {},
      language: 'lithuanian',
    );
    expect(p.source.name, '8filmai');
    final m = playableOf(p);
    expect(m.language, 'lithuanian');
    expect(m.copy, '8filmai:server 3');
    // The source that played is tried first next time, so the next
    // episode stays in the picked language.
    expect((await movies.sourcesFor(film)).first.name, '8filmai');
    core.missing.addAll({'8filmai', 'filmukas'});
    await expectLater(
      movies.resolve(
        series,
        season: 1,
        episode: 1,
        scope: RequestScope(),
        status: (_) {},
        language: 'lithuanian',
      ),
      throwsA(isA<CoreFailure>()),
    );
    expect(
      core.calls.where(
        (c) => c['operation'] == 'movie_match' && c['provider'] == 'vidsrc',
      ),
      isEmpty,
    );
  });

  test('copies whose language a track already has are not offered', () {
    const found = [
      OtherAudio(language: 'lithuanian'),
      OtherAudio(language: 'english'),
      OtherAudio(language: 'english'),
    ];
    final offered = offeredAudio(found, [
      AudioTrack.auto(),
      AudioTrack('1', 'English', 'eng'),
    ]);
    expect(offered.map((o) => o.language), ['lithuanian']);
    expect(offered.single.label, 'Lithuanian (other copy)');
    expect(offeredAudio(found, const []).map((o) => o.language), [
      'lithuanian',
      'english',
    ]);
    expect(animeAudioLanguage('dub'), 'english');
    expect(animeAudioLanguage('sub'), 'japanese');
  });
}
