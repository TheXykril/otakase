import 'dart:async';

import 'package:flutter/material.dart';

import '../core.dart';
import '../data/downloads.dart';
import '../data/models.dart';
import '../data/trakt.dart';
import '../desktop/desktop_core.dart';
import '../services.dart';
import 'components.dart';
import 'device_status.dart';
import '../data/play_request.dart';
import '../player.dart';
import 'movies_pages.dart';

Future<void> openDownloads(
  BuildContext context,
  AppServices services,
  bool isTV,
) => Navigator.push(
  context,
  OtakaseRoute<void>(
    builder: (_) => DownloadsPage(services: services, isTV: isTV),
  ),
);

void _notice(BuildContext context, String text) {
  if (!context.mounted) return;
  ScaffoldMessenger.of(context)
      .showSnackBar(placeNote(context, SnackBar(content: Text(text))));
}

int _quality(AppServices services) =>
    int.tryParse(services.store.text('Quality')) ?? 0;

/// Finds a copy of a film or episode and saves it.
Future<void> downloadTitle(
  BuildContext context,
  AppServices services,
  MediaTitle title, {
  TitleEpisode? episode,
}) async {
  final label = episode?.code ?? (title.year > 0 ? '${title.year}' : '');
  _notice(context, 'Finding a copy to download…');
  try {
    final p = await services.movies.resolve(
      title,
      season: episode?.season ?? 0,
      episode: episode?.number ?? 0,
      scope: RequestScope(),
      status: (_) {},
    );
    await services.downloads.start(
      id: 'title:${title.key}:${episode?.code ?? ''}',
      kind: title.kind,
      title: title.title,
      label: label,
      stream: p.stream,
      poster: title.poster,
      quality: _quality(services),
      meta: {
        'title': title.toJson(),
        'season': episode?.season ?? 0,
        'episode': episode?.number ?? 0,
        'runtime': episode != null && episode.runtime > 0
            ? episode.runtime
            : title.runtime,
      },
    );
    if (context.mounted) {
      _notice(
        context,
        'Downloading ${title.title}${label.isEmpty ? '' : ' $label'}.',
      );
    }
  } on CoreFailure catch (e) {
    if (context.mounted) _notice(context, e.message);
  } catch (_) {
    if (context.mounted) _notice(context, 'The download could not start.');
  }
}

/// Asks which episode to save and saves it, from the same sources that play
/// it.
Future<void> downloadAnimeEpisode(
  BuildContext context,
  AppServices services,
  Anime anime,
  int suggested,
  String mode,
) async {
  final last = anime.airedEpisodes > 0 ? anime.airedEpisodes : suggested;
  final choices = [
    for (
      var ep = suggested.clamp(1, last);
      ep <= last && ep < suggested + 24;
      ep++
    )
      'Episode $ep',
  ];
  final picked = await chooseValue(
    context,
    'Download',
    choices,
    selected: choices.firstOrNull ?? '',
  );
  final episode = int.tryParse(picked?.replaceFirst('Episode ', '') ?? '');
  if (episode == null || !context.mounted) return;
  _notice(context, 'Finding episode $episode…');
  try {
    final source = await services.resolver.resolve(
      anime,
      episode,
      scope: RequestScope(),
      status: (_) {},
      mode: mode,
      exclude: torrentSources,
    );
    await services.downloads.start(
      id: 'anime:${anime.key}:$episode:${source.mode}',
      kind: 'anime',
      title: anime.title,
      label: 'Episode $episode',
      stream: source.streams.first,
      poster: anime.cover,
      quality: _quality(services),
      meta: {'anime': anime.toJson(), 'episode': episode, 'mode': source.mode},
    );
    if (context.mounted) {
      _notice(context, 'Downloading ${anime.title} episode $episode.');
    }
  } on CoreFailure catch (e) {
    if (context.mounted) _notice(context, e.message);
  } catch (_) {
    if (context.mounted) _notice(context, 'The download could not start.');
  }
}

String _size(int bytes) {
  if (bytes <= 0) return '';
  const units = ['B', 'KB', 'MB', 'GB'];
  var value = bytes.toDouble();
  var unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return '${value.toStringAsFixed(unit > 1 ? 1 : 0)} ${units[unit]}';
}

class DownloadsPage extends StatelessWidget {
  const DownloadsPage({super.key, required this.services, required this.isTV});
  final AppServices services;
  final bool isTV;

  String status(DownloadItem d) => switch (d.state) {
    'done' => _size(d.bytes),
    'failed' => d.error.isEmpty ? 'Failed' : d.error,
    'cancelled' => 'Cancelled',
    _ => [
      if (d.progress > 0) '${(d.progress * 100).round()}%' else 'Downloading',
      if (d.partsTotal <= 0 && d.total > 0 && d.bytes > 0)
        '${_size(d.bytes)} of ${_size(d.total)}'
      else if (d.bytes > 0)
        _size(d.bytes),
    ].join(' · '),
  };

  void play(BuildContext context, DownloadItem d) => Navigator.push(
    context,
    OtakaseRoute<void>(
      builder: (_) => PlayerPage.title(
        services: services,
        isTV: isTV,
        request: downloadPlay(services, d),
      ),
    ),
  );

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      leading: IconButton(
        tooltip: 'Back',
        autofocus: true,
        onPressed: () => Navigator.pop(context),
        icon: const Icon(LucideIcons.arrowLeft),
      ),
      title: const Text('Downloads'),
      actions: isDesktop ? const [TopBarEnd(), SizedBox(width: 16)] : null,
    ),
    body: ListenableBuilder(
      listenable: services.downloads,
      builder: (context, _) {
        final items = services.downloads.items;
        if (items.isEmpty) {
          return const Center(
            child: SingleChildScrollView(
              child: EmptyState(
                message: 'Nothing downloaded yet. Use Download on a show, film or episode to watch it offline.',
              ),
            ),
          );
        }
        return ListView.separated(
          padding: EdgeInsets.fromLTRB(
            isTV ? 48 : 16,
            8,
            isTV ? 48 : 16,
            contentBottom(context),
          ),
          itemCount: items.length,
          separatorBuilder: (_, _) => const Divider(height: 1),
          itemBuilder: (context, i) {
            final d = items[i];
            return Row(
              children: [
                Expanded(
                  child: FocusBox(
                    key: ValueKey('download-${d.id}'),
                    isTV: isTV,
                    radius: KageRadius.tile,
                    padding: const EdgeInsets.all(10),
                    onPressed: d.done ? () => play(context, d) : null,
                    child: Row(
                      children: [
                        ClipRRect(
                          borderRadius: BorderRadius.circular(KageRadius.thumb),
                          child: CoverImage(d.poster, width: 46, height: 69),
                        ),
                        const SizedBox(width: 14),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                d.title,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: uiStyle(
                                  context,
                                  weight: FontWeight.w600,
                                ),
                              ),
                              if (d.label.isNotEmpty)
                                Text(
                                  d.label,
                                  style: uiStyle(context, size: 15, color: fg),
                                ),
                              const SizedBox(height: 4),
                              Text(
                                status(d),
                                maxLines: 2,
                                overflow: TextOverflow.ellipsis,
                                style: uiStyle(
                                  context,
                                  size: 15,
                                  color: d.state == 'failed' ? warn : muted,
                                ),
                              ),
                              if (d.running) ...[
                                const SizedBox(height: 6),
                                LinearProgressIndicator(
                                  value: d.progress > 0 ? d.progress : null,
                                  color: shu,
                                  backgroundColor: line,
                                ),
                              ],
                            ],
                          ),
                        ),
                        if (d.done)
                          const Icon(LucideIcons.play, color: accentText),
                      ],
                    ),
                  ),
                ),
                IconButton(
                  tooltip: d.running ? 'Cancel' : 'Delete',
                  onPressed: () => d.running
                      ? services.downloads.cancel(d)
                      : _confirmDelete(context, d),
                  icon: Icon(
                    d.running ? LucideIcons.x : LucideIcons.trash2,
                    color: muted,
                  ),
                ),
              ],
            );
          },
        );
      },
    ),
  );

  Future<void> _confirmDelete(BuildContext context, DownloadItem d) async {
    final yes = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: Text(
          'Delete ${d.title}${d.label.isEmpty ? '' : ' ${d.label}'}?',
        ),
        content: const Text('The file is removed from this device.'),
        actions: [
          TextButton(
            autofocus: true,
            onPressed: () => Navigator.pop(c, false),
            child: const Text('Keep'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(c, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (yes == true) await services.downloads.remove(d);
  }
}

/// Plays a saved file. A download that knows what it is counts on AniList,
/// MyAnimeList and Trakt and in Continue watching like a streamed one; the
/// spot is also kept with the file.
PlayRequest downloadPlay(AppServices services, DownloadItem d) {
  final file = filePlay(
    d.title,
    d.label,
    d.path,
    d.subtitles,
    store: services.store,
    id: d.id,
  );
  final meta = d.meta;
  final animeJson = meta['anime'];
  final episode = number(meta['episode']);
  if (d.kind == 'anime' && animeJson is Map && episode > 0) {
    final anime = Anime.fromJson(animeJson.cast<String, dynamic>());
    services.tracking.remember(anime);
    final pusher = services.tracking.pusher();
    final mode = meta['mode'] as String? ?? d.id.split(':').last;
    return PlayRequest(
      title: file.title,
      subtitle: file.subtitle,
      open: file.open,
      cover: d.poster,
      // Another Otakase streams the same episode.
      remote: anime.id > 0
          ? {
              'anilist_id': anime.id,
              'episode': episode,
              'mode': mode == 'dub' ? 'dub' : 'sub',
            }
          : null,
      // Where this file was left, else where the episode was left while
      // streaming it, as a streamed episode resumes.
      resumeAt: (duration) {
        final spot = file.resumeAt?.call(duration) ?? 0;
        if (spot > 0) return spot;
        final e = services.store.entry(anime);
        if (e == null || e.watched.contains(episode)) return 0;
        return e.resumeFor(episode);
      },
      length: anime.duration * 60 * 2 ~/ 3,
      next: () => _downloaded(services, d, 1),
      previous: () => _downloaded(services, d, -1),
      onProgress: (position, duration, completed) async {
        await file.onProgress?.call(position, duration, completed);
        await services.store.progress(
          anime,
          episode,
          position,
          duration,
          completed: completed,
        );
        if (completed) {
          await pusher.episodeDone(anime.key, episode, anime.episodes);
        } else {
          pusher.note(anime.key, episode, position);
        }
      },
      onClose: pusher.flush,
    );
  }
  final titleJson = meta['title'];
  if (titleJson is Map) {
    final title = MediaTitle.fromJson(titleJson.cast<String, dynamic>());
    final season = number(meta['season']);
    final movies = services.movies;
    return PlayRequest(
      title: file.title,
      subtitle: file.subtitle,
      open: file.open,
      cover: d.poster,
      // Another Otakase streams the same film or episode.
      remote: title.trakt > 0
          ? {'title': title.remote, 'season': season, 'episode': episode}
          : null,
      next: title.isShow ? () => _downloaded(services, d, 1) : null,
      previous: title.isShow ? () => _downloaded(services, d, -1) : null,
      length: number(meta['runtime']) * 60,
      resumeAt: (duration) {
        final spot = file.resumeAt?.call(duration) ?? 0;
        if (spot > 0) return spot;
        final e = movies.entry(title);
        if (e == null) return 0;
        if (title.isShow && (e.season != season || e.episode != episode)) {
          return 0;
        }
        return e.resumeAt(duration);
      },
      onProgress: (position, duration, completed) async {
        await file.onProgress?.call(position, duration, completed);
        await movies.progress(
          title,
          position,
          duration,
          season: season,
          episode: episode,
          completed: completed,
        );
      },
      onScrobble: (action, percent) => unawaited(
        movies.scrobble(
          action,
          title,
          percent,
          season: season,
          episode: episode,
        ),
      ),
    );
  }
  return file;
}

/// The finished download of the same show [step] episodes on from [d] (1
/// the next one saved, -1 the one before), so saved episodes play on one
/// after another like streamed ones.
PlayRequest? _downloaded(AppServices services, DownloadItem d, int step) {
  final from = _downloadOrder(d);
  if (from == null) return null;
  DownloadItem? best;
  (String, int)? bestAt;
  for (final other in services.downloads.items) {
    if (!other.done || other.id == d.id) continue;
    final at = _downloadOrder(other);
    if (at == null || at.$1 != from.$1) continue;
    final gap = (at.$2 - from.$2) * step;
    if (gap <= 0) continue;
    if (bestAt == null || gap < (bestAt.$2 - from.$2) * step) {
      best = other;
      bestAt = at;
    }
  }
  return best == null ? null : downloadPlay(services, best);
}

/// Which show a download belongs to and its place in it, or null for a
/// film or a download that doesn't know.
(String, int)? _downloadOrder(DownloadItem d) {
  final meta = d.meta;
  final episode = number(meta['episode']);
  if (episode <= 0) return null;
  if (meta['anime'] case final Map a) {
    final anime = Anime.fromJson(a.cast<String, dynamic>());
    return ('anime:${anime.key}', episode);
  }
  if (meta['title'] case final Map t) {
    final title = MediaTitle.fromJson(t.cast<String, dynamic>());
    if (!title.isShow) return null;
    return (title.key, number(meta['season']) * 100000 + episode);
  }
  return null;
}
