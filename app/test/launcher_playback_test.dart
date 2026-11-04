import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/trakt.dart';
import 'package:otakase/desktop/launcher_playback.dart';

void main() {
  test('the player consumes a scoped, published handoff', () async {
    final dir = await Directory.systemTemp.createTemp('otakase-play-');
    addTearDown(() async {
      if (await dir.exists()) await dir.delete(recursive: true);
    });
    final file = File('${dir.path}${Platform.pathSeparator}request.json');
    const title = MediaTitle(kind: 'show', trakt: 3, title: 'Dark');
    const episode = TitleEpisode(season: 2, number: 3, title: 'Ghosts');
    final request = LauncherPlayback.title('2', title, episode, [episode]);
    await File('${file.path}.tmp').writeAsString(
      jsonEncode({'profile': request.profileId, ...request.data}),
    );
    final read = LauncherPlayback.read(file.path);
    await request.handOff(file);
    final received = await read;
    expect(received.profileId, '2');
    expect(MediaTitle.fromJson(received.data['title'] as Json).key, title.key);
    expect(received.data['episode'], {
      'season': 2,
      'number': 3,
      'title': 'Ghosts',
    });
    expect(received.data['episodes'], [received.data['episode']]);
    expect(await dir.exists(), isFalse);
  });

  test('the full app handoff retains its profile and window kind', () async {
    final dir = await Directory.systemTemp.createTemp('otakase-play-');
    addTearDown(() async {
      if (await dir.exists()) await dir.delete(recursive: true);
    });
    final file = File('${dir.path}${Platform.pathSeparator}request.json');
    final request = LauncherPlayback.fullApp('3');
    await file.writeAsString(
      jsonEncode({'profile': request.profileId, ...request.data}),
    );
    final received = await LauncherPlayback.read(file.path);
    expect(received.profileId, '3');
    expect(received.opensFullApp, isTrue);
  });

  test('unrelated files are rejected and kept', () async {
    final dir = await Directory.systemTemp.createTemp('unrelated-');
    addTearDown(() => dir.delete(recursive: true));
    final file = await File('${dir.path}${Platform.pathSeparator}request.json')
        .writeAsString('{"profile":"1"}');
    await expectLater(LauncherPlayback.read(file.path), throwsFormatException);
    expect(await file.exists(), isTrue);
  });
}
