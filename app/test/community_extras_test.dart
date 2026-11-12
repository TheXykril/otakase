import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/community_extras.dart';
import 'package:otakase/data/discord.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/host.dart';
import 'package:otakase/desktop/launcher_shortcut.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_test.dart' show MemoryTokens, json;

final date = DateTime(2026, 11, 1, 12);

Future<(CommunityExtras, List<http.Request>)> fixture(
  Future<http.Response> Function(http.Request) bot,
) async {
  final tokens = MemoryTokens();
  tokens.values['discord.link'] = jsonEncode({
    'token': 'app-secret',
    'discord_name': 'Reader',
  });
  final store = AppStore(await SharedPreferences.getInstance());
  final calls = <http.Request>[];
  final discord = DiscordService(
    client: MockClient((r) async {
      calls.add(r);
      return bot(r);
    }),
    storage: tokens,
    store: store,
    now: () => date,
  );
  await discord.initialize(background: false);
  final x = CommunityExtras(discord: discord, store: store, now: () => date);
  addTearDown(() async {
    x.dispose();
    await discord.close();
  });
  return (x, calls);
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('the club is cached and only shown while it runs', () async {
    final (x, calls) = await fixture(
      (_) async => json({
        'club': {
          'anilist_id': 154587,
          'title': 'Frieren',
          'episode': 12,
          'deadline': '2026-11-03T18:00:00Z',
          'caught_up': 3,
          'joined': 5,
        },
      }),
    );
    expect(x.club, isNull);
    await x.refreshClub();
    expect(calls.single.headers['Authorization'], isNull);
    expect(x.club!.title, 'Frieren');
    expect(x.club!.episode, 12);
    await x.refreshClub();
    expect(calls, hasLength(1), reason: 'fetched at most every 3 hours');
  });

  test(
    'backups leave private keys out and restore in the right types',
    () async {
      final (x, calls) = await fixture((_) async => json({'at': 'now'}));
      await x.store.set('SkipOp', false);
      await x.store.set('SubtitleSize', 54);
      await x.store.set('ProviderOrder', ['megavid', 'anikoto']);
      final n = await x.backUp();
      final sent = jsonDecode(calls.single.body)['config'] as String;
      expect(sent, contains('SkipOp=false'));
      expect(sent, contains('SubtitleSize=54'));
      expect(sent, contains('ProviderOrder=megavid,anikoto'));
      expect(n, sent.split('\n').length);
      expect(backupKeyKept('MALClientID'), isFalse);
      expect(backupKeyKept('AniListToken'), isFalse);
      expect(backupKeyKept('AdultContent'), isFalse);
      final values = backupSettings(
        'SkipOp=true\nSubtitleSize=40\nProviderOrder=animex, nyaa\n'
        'DiscordToken=x\nUnknownKey=1\nSubtitleSize2=4\n# SkipEd=false\n'
        'NormalizeVolume=maybe',
      );
      expect(values, {
        'SkipOp': true,
        'SubtitleSize': 40,
        'ProviderOrder': ['animex', 'nyaa'],
      });
      expect(await x.restore('SkipOp=true\nSubtitleSize=40'), 2);
      expect(x.store.flag('SkipOp'), isTrue);
      expect(x.store.integer('SubtitleSize'), 40);
    },
  );

  test('no backup is a null, not an error', () async {
    final (x, _) = await fixture((_) async => json({'error': 'none'}, 404));
    expect(await x.fetchBackup(), isNull);
  });

  test('a backup names the system it was made on', () async {
    final (x, calls) = await fixture(
      (r) async => r.method == 'POST'
          ? json({'at': '2026-11-09T12:00:00Z'})
          : json({
              'config': 'Player=mpv',
              'at': '2026-11-09T12:00:00Z',
              'from_os': 'windows',
            }),
    );
    await x.backUp();
    final sent = jsonDecode(
      calls.lastWhere((r) => r.method == 'POST').body,
    ) as Map<String, dynamic>;
    expect(sent['os'], Host.goos);
    expect(sent['tv'], isFalse);
    expect((await x.fetchBackup())!.fromOS, 'Windows');
    expect(systemLabel('android', tv: true), 'Android TV');
    expect(systemLabel('android'), 'Android');
    expect(systemLabel('darwin'), 'macOS');
    expect(systemLabel('linux'), 'Linux');
  });

  test('reports are multipart, as the linked member, with details', () async {
    late http.Request seen;
    final tokens = MemoryTokens();
    tokens.values['discord.link'] = jsonEncode({'token': 'app-secret'});
    final store = AppStore(await SharedPreferences.getInstance());
    final discord = DiscordService(
      client: MockClient((r) async {
        seen = r;
        return json({'url': 'https://discord.com/channels/1/2'});
      }),
      storage: tokens,
      store: store,
    );
    await discord.initialize(background: false);
    final x = CommunityExtras(discord: discord, store: store);
    addTearDown(() async {
      x.dispose();
      await discord.close();
    });
    store.health('megavid', 'Working');
    store.health('sukebei', 'Unavailable');
    store.health('movie:8filmai', 'No match');
    final diag = reportDiagnostics(store);
    expect(diag, contains('Otakase app'));
    expect(diag, contains('Source 2 Working'));
    expect(diag, contains('Movie source 2 No match'));
    for (final name in [...sourceNames, 'sukebei', '8filmai', 'vidsrc']) {
      expect(diag.toLowerCase(), isNot(contains(name)));
    }
    final url = await x.report('It broke', 'Details', diag);
    expect(url, 'https://discord.com/channels/1/2');
    expect(seen.url.path, '/v1/report');
    expect(seen.headers['Authorization'], 'Bearer app-secret');
    expect(seen.headers['content-type'], startsWith('multipart/form-data'));
    expect(seen.body, contains('name="title"'));
    expect(seen.body, contains('It broke'));
  });

  test('launcher shortcut: Hyprland block is added once and removed', () {
    const conf = HyprlandConfig('/x/hyprland.conf', lua: false);
    const cmd = '/opt/otakase/otakase --launcher';
    final added = withLauncherBinding(
      'bind = SUPER, Q, killactive\n',
      conf,
      cmd,
    );
    expect(added, contains('bind = SUPER SHIFT, A, exec, $cmd'));
    expect(withLauncherBinding(added, conf, cmd), added);
    expect(
      withLauncherBinding(added, conf, '/new --launcher'),
      contains('exec, /new --launcher'),
    );
    expect(
      withoutLauncherBinding(added, conf).trim(),
      'bind = SUPER, Q, killactive',
    );
    expect(
      () =>
          withLauncherBinding('bind = SUPER SHIFT, A, exec, foo\n', conf, cmd),
      throwsStateError,
    );
    const cli =
        '# >>> otakase keybinding (managed; remove with -remove-keybind) >>>\n'
        'bind = SUPER SHIFT, A, exec, otakase -rofi -image-preview\n'
        '# <<< otakase keybinding <<<\n';
    final replaced = withLauncherBinding(cli, conf, cmd);
    expect(replaced, isNot(contains('-rofi')));
    expect(replaced, contains(cmd));
    const lua = HyprlandConfig('/x/bindings.lua', lua: true);
    expect(
      withLauncherBinding('', lua, cmd),
      contains('o.bind("SUPER + SHIFT + A", "Otakase", "exec $cmd")'),
    );
    expect(
      launcherCommand({'APPIMAGE': '/home/me/Otakase.AppImage'}),
      '/home/me/Otakase.AppImage --launcher',
    );
    expect(
      launcherCommand({}, r'C:\Program Files\Otakase\otakase.exe'),
      r'"C:\Program Files\Otakase\otakase.exe" --launcher',
    );
  });

  test('the Hyprland config is found where the CLI looked', () async {
    final home = await Directory.systemTemp.createTemp('hypr');
    addTearDown(() => home.delete(recursive: true));
    expect(findHyprlandConfig(home.path), isNull);
    await Directory('${home.path}/.config/hypr').create(recursive: true);
    await File('${home.path}/.config/hypr/hyprland.conf').writeAsString('');
    expect(findHyprlandConfig(home.path)!.lua, isFalse);
    await File('${home.path}/.config/hypr/bindings.lua').writeAsString('');
    expect(findHyprlandConfig(home.path)!.lua, isTrue);
    final path = '${home.path}/.config/hypr/hyprland.conf';
    final backup = await writeWithBackup(path, 'new');
    expect(await File(path).readAsString(), 'new');
    expect(await File(backup).readAsString(), '');
  });
}
