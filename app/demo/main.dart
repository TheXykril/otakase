// The website's live demo: the real app built for the web, with the shows in
// test/fixtures/site and no accounts. `?device=computer|phone|tv` picks the
// layout. Built by the Site workflow (flutter build web -t demo/main.dart).
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:media_kit/media_kit.dart';
import 'package:otakase/data/app_updates.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/desktop/desktop_core.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'binding.dart';
import 'catalog.dart';
import 'core.dart';

Future<void> main() async {
  final device = Uri.base.queryParameters['device'] ?? 'computer';
  DemoBinding.ensureInitialized(device);
  MediaKit.ensureInitialized();
  debugDesktopOverride = device == 'computer';
  final art = Uri.base.resolve('art/');
  final list =
      jsonDecode((await http.get(art.resolve('shows.json'))).body) as List;
  final shows = [
    for (final j in list) showFromJson(j as Map, (f) => '${art.resolve(f)}'),
  ];
  final main = shows.first;
  // The demo talks to nobody: sign-ins, lists and the Discord bot all
  // answer as if offline.
  final offline = MockClient((_) async => http.Response('offline demo', 503));
  // Nothing is kept between visits: every visitor starts from the same app.
  // ignore: invalid_use_of_visible_for_testing_member
  SharedPreferences.setMockInitialValues({});
  final store = AppStore(await SharedPreferences.getInstance());
  var n = 0;
  for (final a in shows.where((s) => s.id != main.id).take(4)) {
    n++;
    await store.progress(a, 1 + n, 200 + n * 180, 1440);
    await Future<void>.delayed(const Duration(milliseconds: 5));
  }
  await Future<void>.delayed(const Duration(milliseconds: 20));
  await store.progress(main, resumeAt, 426, 1440);
  runApp(
    OtakaseApp(
      services: AppServices(
        catalog: DemoCatalog(shows, main, client: offline),
        appUpdates: AppUpdates(client: () => offline),
        store: store,
        core: DemoCore(Uri.base.resolve('demo.mp4'), (id) {
          final show = shows.where((s) => '${s.id}' == id).firstOrNull;
          return show == null ? 12 : show.airedEpisodes.clamp(1, 24);
        }),
      ),
      isTV: device != 'phone',
    ),
  );
}
