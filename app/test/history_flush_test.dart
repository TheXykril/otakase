import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/store.dart';
import 'package:shared_preferences/shared_preferences.dart';

final show = Anime.fromJson({
  'id': 1,
  'idMal': 1,
  'title': {'romaji': 'Show 1'},
  'episodes': 12,
  'duration': 24,
});

Future<AppStore> freshStore() async {
  SharedPreferences.setMockInitialValues({});
  return AppStore(await SharedPreferences.getInstance());
}

String? written(AppStore store) => store.preferences.getString('history.v1');

void main() {
  testWidgets('quiet saves during playback leave the rest of the app alone', (
    tester,
  ) async {
    final store = await tester.runAsync(freshStore);
    var pageBuilds = 0, spotBuilds = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Column(
          children: [
            // Stands in for the shell and Home under the player.
            ListenableBuilder(
              listenable: store!,
              builder: (_, _) {
                pageBuilds++;
                return const SizedBox();
              },
            ),
            ValueListenableBuilder(
              valueListenable: store.historyProgress,
              builder: (_, _, _) {
                spotBuilds++;
                return const SizedBox();
              },
            ),
          ],
        ),
      ),
    );
    expect(pageBuilds, 1);
    for (var i = 1; i <= 5; i++) {
      await store.progress(show, 3, i * 5, 1440, quiet: true);
      await tester.pump(const Duration(seconds: 5));
    }
    expect(pageBuilds, 1);
    expect(spotBuilds, 6);
    expect(store.entry(show)!.position, 25);
    // Nothing written yet: the spot waits in memory.
    expect(written(store), isNull);

    // The write lands on its own once the delay has passed, still without
    // rebuilding the app.
    await tester.pump(AppStore.historyWriteDelay);
    expect(written(store), contains('"position":25'));
    expect(pageBuilds, 1);

    // Pausing or leaving announces it once.
    await store.flushHistory();
    await tester.pump();
    expect(pageBuilds, 2);
    await store.flushHistory();
    await tester.pump();
    expect(pageBuilds, 2);
  });

  testWidgets('leaving the player writes the latest spot at once', (
    tester,
  ) async {
    final store = await tester.runAsync(freshStore);
    var notified = 0;
    store!.addListener(() => notified++);
    await store.progress(show, 4, 30, 1440, quiet: true);
    await store.progress(show, 4, 35, 1440, quiet: true);
    expect(written(store), isNull);
    await store.flushHistory();
    expect(notified, 1);
    expect(written(store), contains('"position":35'));
    final reloaded = AppStore(store.preferences);
    expect(reloaded.entry(show)!.position, 35);
    expect(reloaded.entry(show)!.episode, 4);
    await tester.pump(AppStore.historyWriteDelay);
  });

  testWidgets('a normal save writes quiet spots along with it', (tester) async {
    final store = await tester.runAsync(freshStore);
    await store!.progress(show, 2, 40, 1440, quiet: true);
    await store.progress(show, 2, 1440, 1440, completed: true);
    expect(store.entry(show)!.watched, [2]);
    final reloaded = AppStore(store.preferences);
    expect(reloaded.entry(show)!.watched, [2]);
  });

  testWidgets('closing a profile writes a spot still waiting', (tester) async {
    final store = await tester.runAsync(freshStore);
    await store!.progress(show, 5, 60, 1440, quiet: true);
    store.dispose();
    await tester.pump();
    expect(AppStore(store.preferences).entry(show)!.position, 60);
  });
}
