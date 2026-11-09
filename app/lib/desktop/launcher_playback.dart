import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:window_manager/window_manager.dart';

import '../data/models.dart';
import '../data/trakt.dart';
import '../services.dart';
import '../ui/components.dart';
import '../ui/movie_player.dart';
import '../ui/movies_pages.dart';
import 'external_mpv.dart';
import 'window_frame.dart';
import '../host.dart';

/// The media and profile selected in the popup. The player opens in its own
/// process and loads the normal profile settings and progress there.
class LauncherPlayback {
  LauncherPlayback.anime(this.profileId, Anime anime, int episode)
    : data = {'anime': anime.toJson(), 'number': episode};

  LauncherPlayback.title(
    this.profileId,
    MediaTitle title,
    TitleEpisode? episode,
    List<TitleEpisode> episodes,
  ) : data = {
        'title': title.toJson(),
        if (episode != null) 'episode': _episodeJson(episode),
        'episodes': episodes.map(_episodeJson).toList(),
      };

  LauncherPlayback.fullApp(this.profileId) : data = const {'fullApp': true};

  LauncherPlayback._(this.profileId, this.data);

  bool get opensFullApp => data['fullApp'] == true;

  final String profileId;
  final Json data;
  static const argument = '--launcher-playback';

  static Json _episodeJson(TitleEpisode episode) => {
    'season': episode.season,
    'number': episode.number,
    'title': episode.title,
  };

  static TitleEpisode _episode(Json json) => TitleEpisode(
    season: json['season'] as int,
    number: json['number'] as int,
    title: json['title'] as String,
  );

  /// The child waits for the launcher's profile session to close before
  /// opening its own. A file avoids command-line size limits for long series.
  Future<File> start() async {
    final dir = await Directory.systemTemp.createTemp('otakase-play-');
    final file = File('${dir.path}${Host.pathSeparator}request.json');
    final executable = Host.environment['APPIMAGE'] ?? Host.executable;
    try {
      await File('${file.path}.tmp')
          .writeAsString(jsonEncode({'profile': profileId, ...data}));
      await Process.start(
        executable,
        [argument, file.path],
        workingDirectory: Host.windows ? File(executable).parent.path : null,
        mode: ProcessStartMode.detached,
      );
      return file;
    } catch (_) {
      await dir.delete(recursive: true);
      rethrow;
    }
  }

  Future<void> handOff(File file) async {
    await File('${file.path}.tmp').rename(file.path);
  }

  static Future<LauncherPlayback> read(String path) async {
    final file = File(path).absolute;
    final directory = file.parent;
    final directoryName = directory.path.split(Host.pathSeparator).last;
    if (file.uri.pathSegments.last != 'request.json' ||
        !directoryName.startsWith('otakase-play-') ||
        await directory.parent.resolveSymbolicLinks() !=
            await Directory.systemTemp.resolveSymbolicLinks() ||
        await FileSystemEntity.type(directory.path, followLinks: false) !=
            FileSystemEntityType.directory) {
      throw const FormatException('Invalid player handoff path.');
    }
    for (var attempt = 0; attempt < 100; attempt++) {
      if (await FileSystemEntity.type(file.path, followLinks: false) ==
          FileSystemEntityType.file) {
        final data = jsonDecode(await file.readAsString()) as Json;
        await file.delete();
        try {
          await directory.delete();
        } on FileSystemException {
          // The media request is already in memory.
        }
        return LauncherPlayback._(data['profile'] as String, data);
      }
      await Future<void>.delayed(const Duration(milliseconds: 100));
    }
    throw StateError('The launcher did not finish opening playback.');
  }

  Widget player(AppServices services) {
    if (data['anime'] case final Json anime) {
      return playerFor(
        services: services,
        anime: Anime.fromJson(anime),
        episode: data['number'] as int,
        isTV: true,
        mode: services.store.text('SubOrDub'),
      );
    }
    final title = MediaTitle.fromJson(data['title'] as Json);
    return MoviePlayerPage(
      services: services,
      isTV: true,
      request: titlePlay(
        services,
        title,
        episode: data['episode'] == null
            ? null
            : _episode(data['episode'] as Json),
        episodes: (data['episodes'] as List)
            .cast<Json>()
            .map(_episode)
            .toList(),
      ),
    );
  }
}

class LauncherPlayerWindow extends StatefulWidget {
  const LauncherPlayerWindow({
    super.key,
    required this.request,
    required this.services,
  });

  final LauncherPlayback request;
  final AppServices services;

  @override
  State<LauncherPlayerWindow> createState() => _LauncherPlayerWindowState();
}

class _LauncherPlayerWindowState extends State<LauncherPlayerWindow> {
  bool _closing = false;

  @override
  void initState() {
    super.initState();
    desktopCloseHook = close;
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      if (!mounted || widget.services.profileId != widget.request.profileId) {
        return;
      }
      await Navigator.of(context).push(
        OtakaseRoute<void>(
          builder: (_) => widget.request.player(widget.services),
        ),
      );
      if (mounted) await close();
    });
  }

  @override
  void dispose() {
    if (desktopCloseHook == close) desktopCloseHook = null;
    super.dispose();
  }

  Future<void> close() async {
    if (_closing) return;
    _closing = true;
    await windowManager.hide();
    await widget.services.close().timeout(
      const Duration(seconds: 5),
      onTimeout: () {},
    );
    await closeDesktopWindow();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    body: Center(
      child: widget.services.profileId == widget.request.profileId
          ? const CircularProgressIndicator()
          : TextButton(
              onPressed: close,
              child: const Text('Close player. The selected profile changed.'),
            ),
    ),
  );
}
