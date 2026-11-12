import 'dart:async';
import 'dart:math' as math;
import 'dart:ui' show ImageFilter;

import 'package:flutter/material.dart';

import '../core.dart';
import '../data/models.dart';
import '../data/tv_remote.dart';
import '../services.dart';
import 'components.dart';
import 'screen_orientation.dart';
import 'skip_marker.dart';

/// Whether the phone may show what the TV plays: 18+ titles stay hidden
/// on a phone whose AniList has adult content off.
bool remoteVisible(AppServices services, Json status) =>
    status['adult'] != true || services.store.adultAllowed;

String remoteStateText(RemoteLink link) => switch (link.state) {
  LinkState.live || LinkState.polling => 'Connected',
  LinkState.away => 'Away, through Discord',
  LinkState.lost => 'Reconnecting…',
  _ => 'Connecting…',
};

/// The phone's full remote for one TV: what plays there, a scrubber with
/// the opening and ending marked, skip, episodes, tracks, quality,
/// volume and stop.
class RemoteNowPlaying extends StatefulWidget {
  const RemoteNowPlaying({super.key, required this.services});
  final AppServices services;
  @override
  State<RemoteNowPlaying> createState() => _RemoteNowPlayingState();
}

class _RemoteNowPlayingState extends State<RemoteNowPlaying> {
  double? dragging, volume;
  String? error;

  RemoteLink? get link => widget.services.remoteLink.value;

  @override
  void initState() {
    super.initState();
    // The remote is held like a phone, never turned sideways.
    ScreenOrientation.lockUpright();
  }

  @override
  void dispose() {
    ScreenOrientation.release();
    super.dispose();
  }

  Future<void> send(String action, {Json args = const {}}) async {
    final current = link;
    if (current == null) return;
    try {
      await current.send(action, args: args);
      if (mounted && error != null) setState(() => error = null);
    } catch (e) {
      if (mounted) setState(() => error = errorMessage(e));
    }
  }

  Future<void> pick(
    String title,
    List<Json> choices,
    String action,
    String Function(Json) value,
  ) async {
    if (choices.isEmpty) return;
    final chosen = await showModalBottomSheet<Json>(
      context: context,
      backgroundColor: panel,
      showDragHandle: true,
      builder: (sheet) => SafeArea(
        child: ListView(
          shrinkWrap: true,
          padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(8, 0, 8, 8),
              child: Text(
                title,
                style: uiStyle(sheet, size: 20, weight: FontWeight.w700),
              ),
            ),
            for (final c in choices)
              ListTile(
                key: Key('remote-choice-${value(c)}'),
                title: Text('${c['label']}', style: uiStyle(sheet)),
                trailing: c['on'] == true
                    ? const Icon(LucideIcons.check, color: shu)
                    : null,
                onTap: () => Navigator.pop(sheet, c),
              ),
          ],
        ),
      ),
    );
    if (chosen != null) await send(action, args: {'id': value(chosen)});
  }

  Future<void> quality(Json status) async {
    final values = [
      for (final q in status['qualities'] as List? ?? const []) '$q',
    ];
    if (values.isEmpty) return;
    final chosen = await chooseValue(
      context,
      'Quality',
      values,
      selected: '${status['quality'] ?? ''}',
    );
    if (chosen != null) await send('quality', args: {'value': chosen});
  }

  Future<void> episodes(Json status) async {
    final ep = await chooseValue(
      context,
      'Episode',
      List.generate(
        number(status['episodes']).clamp(1, 10000),
        (i) => '${i + 1}',
      ),
      selected: '${number(status['episode'])}',
    );
    if (ep != null) await send('episode', args: {'episode': int.parse(ep)});
  }

  Future<void> menu(RemoteLink current) async {
    final choice = await showModalBottomSheet<String>(
      context: context,
      backgroundColor: panel,
      showDragHandle: true,
      builder: (sheet) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              key: const Key('remote-disconnect'),
              leading: const Icon(LucideIcons.unplug),
              title: const Text('Disconnect'),
              onTap: () => Navigator.pop(sheet, 'leave'),
            ),
            ListTile(
              key: const Key('remote-forget'),
              leading: const Icon(LucideIcons.trash2),
              title: Text('Forget ${current.tv.name}'),
              onTap: () => Navigator.pop(sheet, 'forget'),
            ),
          ],
        ),
      ),
    );
    if (choice == null || !mounted) return;
    if (choice == 'forget') {
      await widget.services.tvRemote.forget(current.tv.id);
    }
    await widget.services.leaveTV();
    if (mounted) Navigator.maybePop(context);
  }

  @override
  Widget build(BuildContext context) => ValueListenableBuilder(
    valueListenable: widget.services.remoteLink,
    builder: (context, current, _) {
      if (current == null) {
        return Scaffold(
          appBar: AppBar(),
          body: Center(
            child: Text('Not controlling a TV', style: uiStyle(context)),
          ),
        );
      }
      return ListenableBuilder(
        listenable: current,
        builder: (context, _) => page(current, current.status),
      );
    },
  );

  Widget page(RemoteLink current, Json status) {
    final visible = remoteVisible(widget.services, status),
        playing = status['has_player'] == true,
        cover = visible ? '${status['cover'] ?? ''}' : '',
        duration = number(status['duration']).toDouble(),
        position = dragging ?? number(status['position']).toDouble(),
        problem = error ?? current.error;
    return Scaffold(
      backgroundColor: bgDeep,
      body: Stack(
        fit: StackFit.expand,
        children: [
          if (cover.isNotEmpty)
            ImageFiltered(
              imageFilter: ImageFilter.blur(sigmaX: 40, sigmaY: 40),
              child: Opacity(
                opacity: .35,
                child: CoverImage(cover, width: 400, height: 800),
              ),
            ),
          SafeArea(
            child: LayoutBuilder(
              builder: (context, box) => SingleChildScrollView(
                padding: const EdgeInsets.fromLTRB(20, 8, 20, 20),
                child: ConstrainedBox(
                  constraints: BoxConstraints(minHeight: box.maxHeight - 28),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      header(current),
                      if (!playing)
                        idle(current)
                      else ...[
                        const SizedBox(height: 20),
                        Center(
                          child: ClipRRect(
                            borderRadius: BorderRadius.circular(12),
                            child: SizedBox(
                              width: math.min(220, box.maxWidth * .56),
                              height: math.min(310, box.maxWidth * .79),
                              child: cover.isEmpty
                                  ? const ColoredBox(
                                      color: panelRaised,
                                      child: Icon(
                                        LucideIcons.tv,
                                        size: 48,
                                        color: muted,
                                      ),
                                    )
                                  : CoverImage(
                                      cover,
                                      width: math.min(220, box.maxWidth * .56),
                                      height: math.min(310, box.maxWidth * .79),
                                    ),
                            ),
                          ),
                        ),
                        const SizedBox(height: 20),
                        Text(
                          visible
                              ? '${status['title'] ?? ''}'
                              : 'Playing on ${current.tv.name}',
                          key: const Key('remote-title'),
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: uiStyle(
                            context,
                            size: 24,
                            weight: FontWeight.w700,
                            color: fgBright,
                          ),
                        ),
                        if (visible && number(status['episode']) > 0)
                          Padding(
                            padding: const EdgeInsets.only(top: 4),
                            child: Text(
                              'EPISODE ${number(status['episode'])}',
                              style: mono.copyWith(letterSpacing: 1.2),
                            ),
                          ),
                        const SizedBox(height: 14),
                        scrubber(status, duration, position),
                        Row(
                          children: [
                            Text(clock(position), style: mono),
                            const Spacer(),
                            Text(
                              '−${clock(math.max(0, duration - position))}',
                              style: mono,
                            ),
                          ],
                        ),
                        SizedBox(
                          height: 52,
                          child: Align(
                            alignment: Alignment.centerRight,
                            child: status['skip'] is String
                                ? OutlinedButton.icon(
                                    key: const Key('remote-skip'),
                                    style: OutlinedButton.styleFrom(
                                      foregroundColor: fgBright,
                                      backgroundColor: bgDeep.withValues(
                                        alpha: .8,
                                      ),
                                      side: const BorderSide(color: shu),
                                      shape: RoundedRectangleBorder(
                                        borderRadius: BorderRadius.circular(10),
                                      ),
                                    ),
                                    onPressed: () => send('party_skip'),
                                    icon: const Icon(LucideIcons.fastForward),
                                    label: Text(
                                      '${status['skip']}',
                                      style: uiStyle(
                                        context,
                                        size: 14,
                                        weight: FontWeight.w600,
                                        color: fgBright,
                                      ),
                                    ),
                                  )
                                : null,
                          ),
                        ),
                        transport(status),
                        const SizedBox(height: 18),
                        volumeRow(status),
                        const SizedBox(height: 18),
                        const Divider(color: line, height: 1),
                        const SizedBox(height: 12),
                        tools(status, visible),
                      ],
                      if (problem != null)
                        Padding(
                          padding: const EdgeInsets.only(top: 16),
                          child: Text(
                            problem,
                            textAlign: TextAlign.center,
                            style: uiStyle(context, color: warn),
                          ),
                        ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget header(RemoteLink current) => Row(
    children: [
      IconButton(
        tooltip: 'Close',
        icon: const Icon(LucideIcons.chevronDown, color: fgBright),
        onPressed: () => Navigator.maybePop(context),
      ),
      Expanded(
        child: Center(
          child: Container(
            padding: const EdgeInsets.fromLTRB(12, 8, 14, 8),
            decoration: BoxDecoration(
              color: panel.withValues(alpha: .85),
              borderRadius: BorderRadius.circular(999),
            ),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Icon(LucideIcons.tv, size: 18, color: shu),
                const SizedBox(width: 8),
                Flexible(
                  child: Text(
                    current.tv.name,
                    overflow: TextOverflow.ellipsis,
                    style: uiStyle(
                      context,
                      weight: FontWeight.w600,
                      color: fgBright,
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                Tooltip(
                  message: remoteStateText(current),
                  child: Container(
                    key: const Key('remote-link-dot'),
                    width: 8,
                    height: 8,
                    decoration: BoxDecoration(
                      shape: BoxShape.circle,
                      color: current.live && !current.away ? ok : warn,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
      IconButton(
        key: const Key('remote-menu'),
        tooltip: 'More',
        icon: const Icon(LucideIcons.ellipsis, color: fgBright),
        onPressed: () => menu(current),
      ),
    ],
  );

  Widget idle(RemoteLink current) => Padding(
    padding: const EdgeInsets.only(top: 120),
    child: Column(
      children: [
        Icon(
          current.live ? LucideIcons.tv : LucideIcons.wifiOff,
          size: 48,
          color: muted,
        ),
        const SizedBox(height: 16),
        Text(
          current.live
              ? 'Nothing is playing on ${current.tv.name}'
              : remoteStateText(current),
          textAlign: TextAlign.center,
          style: uiStyle(context, size: 20, weight: FontWeight.w600),
        ),
        const SizedBox(height: 8),
        Text(
          'Open an episode on the TV and its controls show up here.',
          textAlign: TextAlign.center,
          style: uiStyle(context, color: muted),
        ),
      ],
    ),
  );

  Widget scrubber(Json status, double duration, double position) {
    final spans = [
      for (final s in status['spans'] as List? ?? const [])
        if (s is Map) (number(s['start']), number(s['end'])),
    ];
    return SizedBox(
      height: 36,
      child: LayoutBuilder(
        builder: (context, box) => Stack(
          alignment: Alignment.center,
          children: [
            if (duration > 0)
              for (final (start, end) in spans)
                Positioned(
                  left: 12 + (box.maxWidth - 24) * (start / duration),
                  width: math.max(
                    2,
                    (box.maxWidth - 24) * ((end - start) / duration),
                  ),
                  top: 25,
                  height: 3,
                  child: const ColoredBox(color: fg),
                ),
            SliderTheme(
              data: SliderTheme.of(context).copyWith(
                trackHeight: 5,
                activeTrackColor: shu,
                inactiveTrackColor: const Color(0xff5a5148),
                thumbColor: fgBright,
                overlayShape: SliderComponentShape.noOverlay,
              ),
              child: Slider(
                key: const Key('remote-scrubber'),
                value: duration <= 0 ? 0 : position.clamp(0, duration),
                max: duration <= 0 ? 1 : duration,
                onChanged: duration <= 0
                    ? null
                    : (v) => setState(() => dragging = v),
                onChangeEnd: duration <= 0
                    ? null
                    : (v) {
                        unawaited(
                          send('seek_to', args: {'position': v.round()}),
                        );
                        // Hold the dragged spot until the TV says so too.
                        Timer(const Duration(seconds: 1), () {
                          if (mounted) setState(() => dragging = null);
                        });
                      },
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget round(
    IconData icon,
    String tip,
    VoidCallback? tap, {
    double size = 52,
    Color fill = panel,
    Color color = fgBright,
    Key? key,
  }) => Tooltip(
    message: tip,
    child: Material(
      color: fill,
      shape: const CircleBorder(),
      child: InkWell(
        key: key,
        customBorder: const CircleBorder(),
        onTap: tap,
        child: SizedBox(
          width: size,
          height: size,
          child: Icon(
            icon,
            size: size * .42,
            color: tap == null ? disabled : color,
          ),
        ),
      ),
    ),
  );

  Widget transport(Json status) {
    final playing = status['playing'] == true;
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        round(
          LucideIcons.skipBack,
          'Previous episode',
          status['can_previous'] == true ? () => send('previous') : null,
          fill: Colors.transparent,
          key: const Key('remote-previous'),
        ),
        round(
          LucideIcons.rotateCcw,
          'Back 10 seconds',
          () => send('seek', args: {'seconds': -10}),
          key: const Key('remote-back'),
        ),
        round(
          playing ? LucideIcons.pause : LucideIcons.play,
          playing ? 'Pause' : 'Play',
          () => send(playing ? 'pause' : 'play'),
          size: 76,
          fill: shu,
          color: bgDeep,
          key: const Key('remote-play'),
        ),
        round(
          LucideIcons.rotateCw,
          'Forward 10 seconds',
          () => send('seek', args: {'seconds': 10}),
          key: const Key('remote-forward'),
        ),
        round(
          LucideIcons.skipForward,
          'Next episode',
          status['can_next'] != false ? () => send('next') : null,
          fill: Colors.transparent,
          key: const Key('remote-next'),
        ),
      ],
    );
  }

  Widget volumeRow(Json status) {
    final level = volume ?? number(status['volume']).toDouble().clamp(0, 100);
    return Row(
      children: [
        const Icon(LucideIcons.volume2, size: 18, color: muted),
        Expanded(
          child: SliderTheme(
            data: SliderTheme.of(context).copyWith(
              trackHeight: 4,
              activeTrackColor: shu,
              inactiveTrackColor: const Color(0xff5a5148),
              thumbColor: fgBright,
              overlayShape: SliderComponentShape.noOverlay,
            ),
            child: Slider(
              key: const Key('remote-volume'),
              value: level.toDouble(),
              max: 100,
              onChanged: (v) => setState(() => volume = v),
              onChangeEnd: (v) {
                unawaited(send('volume', args: {'volume': v.round()}));
                Timer(const Duration(seconds: 1), () {
                  if (mounted) setState(() => volume = null);
                });
              },
            ),
          ),
        ),
        SizedBox(
          width: 32,
          child: Text(
            '${level.round()}',
            textAlign: TextAlign.right,
            style: mono,
          ),
        ),
      ],
    );
  }

  Widget tools(Json status, bool visible) {
    List<Json> list(String key) => [
      for (final t in status[key] as List? ?? const [])
        if (t is Map) t.cast<String, dynamic>(),
    ];
    final subtitles = list('subtitles'), audio = list('audio');
    final canEpisodes =
        visible && status['room'] != true && number(status['episodes']) > 0;
    Widget tool(String label, IconData icon, VoidCallback? tap) => Expanded(
      child: InkWell(
        key: Key('remote-tool-$label'),
        borderRadius: BorderRadius.circular(12),
        onTap: tap,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 8),
          child: Column(
            children: [
              Icon(icon, color: tap == null ? disabled : fg),
              const SizedBox(height: 6),
              Text(
                label,
                style: uiStyle(
                  context,
                  size: 12,
                  color: tap == null ? disabled : muted,
                ),
              ),
            ],
          ),
        ),
      ),
    );
    return Row(
      children: [
        tool(
          'Episodes',
          LucideIcons.list,
          canEpisodes ? () => episodes(status) : null,
        ),
        tool(
          'Subtitles',
          LucideIcons.captions,
          subtitles.isEmpty
              ? null
              : () => pick(
                  'Subtitles',
                  subtitles,
                  'subtitle',
                  (c) => '${c['id']}',
                ),
        ),
        tool(
          'Audio',
          LucideIcons.audioLines,
          audio.length < 2
              ? null
              : () => pick('Audio', audio, 'audio', (c) => '${c['id']}'),
        ),
        tool(
          'Quality',
          LucideIcons.gauge,
          (status['qualities'] as List? ?? const []).length < 2
              ? null
              : () => quality(status),
        ),
        tool('Stop', LucideIcons.square, () => send('stop')),
      ],
    );
  }
}

/// The bar above the phone's tabs while a TV this phone controls plays
/// something: art, title, connection and play/pause. Tap opens the full
/// remote.
class RemoteMiniBar extends StatelessWidget {
  const RemoteMiniBar({super.key, required this.services, required this.link});
  final AppServices services;
  final RemoteLink link;

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: link,
    builder: (context, _) {
      final status = link.status,
          visible = remoteVisible(services, status),
          cover = visible ? '${status['cover'] ?? ''}' : '',
          duration = number(status['duration']),
          playing = status['playing'] == true;
      final title = visible
          ? [
              '${status['title'] ?? ''}',
              if (number(status['episode']) > 0)
                'Ep ${number(status['episode'])}',
            ].where((s) => s.isNotEmpty).join(' · ')
          : 'Playing on ${link.tv.name}';
      return Material(
        key: const Key('remote-mini-bar'),
        color: panelRaised,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(16),
          side: const BorderSide(color: line),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: () => Navigator.push(
            context,
            OtakaseRoute<void>(
              builder: (_) => RemoteNowPlaying(services: services),
            ),
          ),
          child: Stack(
            children: [
              Padding(
                padding: const EdgeInsets.all(8),
                child: Row(
                  children: [
                    ClipRRect(
                      borderRadius: BorderRadius.circular(6),
                      child: SizedBox(
                        width: 36,
                        height: 50,
                        child: cover.isEmpty
                            ? const ColoredBox(
                                color: panel,
                                child: Icon(LucideIcons.tv, color: muted),
                              )
                            : CoverImage(cover, width: 36, height: 50),
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Text(
                            title,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: uiStyle(
                              context,
                              size: 15,
                              weight: FontWeight.w600,
                              color: fgBright,
                            ),
                          ),
                          const SizedBox(height: 3),
                          Text(
                            link.live && !link.away
                                ? link.tv.name
                                : '${link.tv.name} · ${remoteStateText(link)}',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: uiStyle(
                              context,
                              size: 12,
                              color: link.live && !link.away ? muted : warn,
                            ),
                          ),
                        ],
                      ),
                    ),
                    IconButton.filled(
                      key: const Key('remote-mini-play'),
                      style: IconButton.styleFrom(
                        backgroundColor: shu,
                        foregroundColor: bgDeep,
                      ),
                      tooltip: playing ? 'Pause' : 'Play',
                      icon: Icon(
                        playing ? LucideIcons.pause : LucideIcons.play,
                      ),
                      onPressed: () => unawaited(
                        link
                            .send(playing ? 'pause' : 'play')
                            .catchError((Object _) {}),
                      ),
                    ),
                  ],
                ),
              ),
              if (duration > 0)
                Positioned(
                  left: 12,
                  right: 12,
                  bottom: 0,
                  height: 2,
                  child: LinearProgressIndicator(
                    value: (number(status['position']) / duration).clamp(0, 1),
                    color: shu,
                    backgroundColor: const Color(0xff5a5148),
                  ),
                ),
            ],
          ),
        ),
      );
    },
  );
}
