import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../data/heritage.dart';
import '../data/models.dart';
import '../services.dart';
import 'components.dart';

const otakaseMovieEntry = Anime(
  id: -1,
  malId: -1,
  title: 'Otakase: The Movie',
  nativeTitle: '終',
  format: 'SPECIAL',
  description: 'A short film about your journey through anime.',
);
bool isOtakaseMovie(Anime anime) => anime.id == -1 && anime.malId == -1;

class OtakaseMovie extends StatefulWidget {
  const OtakaseMovie({
    super.key,
    required this.services,
    required this.isTV,
    this.milestone = 0,
  });
  final AppServices services;
  final bool isTV;
  final int milestone;
  @override
  State<OtakaseMovie> createState() => _OtakaseMovieState();
}

class _OtakaseMovieState extends State<OtakaseMovie> {
  WatchStats? stats;
  List<Anime> favourites = [];
  Timer? timer;
  int elapsed = 0;
  bool paused = false;
  int get chapter => (elapsed ~/ 7000).clamp(0, 4);
  @override
  void initState() {
    super.initState();
    unawaited(load());
    timer = Timer.periodic(const Duration(milliseconds: 100), (_) {
      if (!mounted || paused) return;
      if (elapsed >= 35000) {
        Navigator.pop(context);
        return;
      }
      setState(() => elapsed += 100);
    });
  }

  Future<void> load() async {
    final snapshot = await widget.services.heritage.stats.snapshot();
    if (!mounted) return;
    setState(() => stats = snapshot);
    if (widget.services.auth.signedIn('AniList')) {
      try {
        final r = await widget.services.auth.graph(
          'query { Viewer { favourites { anime(perPage:12) { nodes { id idMal title { romaji english native } coverImage { large } isAdult } } } } }',
          {},
        );
        final nodes =
            r['Viewer']?['favourites']?['anime']?['nodes'] as List? ?? [];
        if (mounted) {
          setState(
            () => favourites = nodes
                .whereType<Map>()
                .map((m) => Anime.fromJson(m.cast<String, dynamic>()))
                .where(widget.services.store.visible)
                .toList(),
          );
        }
      } catch (_) {
        /* The film continues when favourites are unavailable. */
      }
    }
  }

  void jump(int delta) =>
      setState(() => elapsed = ((chapter + delta).clamp(0, 4)) * 7000);
  @override
  void dispose() {
    timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final name =
        widget.services.auth.account('AniList')?.username ??
        widget.services.auth.account('MAL')?.username ??
        widget.services.profiles?.active.name ??
        'you';
    const titles = [
      'Counting the Stars',
      'The Voices',
      'Memories That Never Fade',
      'Legendary Conquests',
      'The Journey Continues',
    ];
    return Scaffold(
      backgroundColor: bgDeep,
      body: Focus(
        autofocus: true,
        onKeyEvent: (node, e) {
          if (e is! KeyDownEvent || !node.hasPrimaryFocus) {
            return KeyEventResult.ignored;
          }
          if (e.logicalKey == LogicalKeyboardKey.arrowRight) {
            jump(1);
            return KeyEventResult.handled;
          }
          if (e.logicalKey == LogicalKeyboardKey.arrowLeft) {
            jump(-1);
            return KeyEventResult.handled;
          }
          if ([
            LogicalKeyboardKey.select,
            LogicalKeyboardKey.enter,
            LogicalKeyboardKey.space,
          ].contains(e.logicalKey)) {
            setState(() => paused = !paused);
            return KeyEventResult.handled;
          }
          return KeyEventResult.ignored;
        },
        child: Stack(
          fit: StackFit.expand,
          children: [
            ExcludeSemantics(
              child: CustomPaint(
                painter: _NightSky(
                  MediaQuery.disableAnimationsOf(context) ? 0 : elapsed / 35000,
                ),
              ),
            ),
            SafeArea(
              child: Padding(
                padding: EdgeInsets.all(widget.isTV ? 48 : 20),
                child: Column(
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            'OTAKASE: THE MOVIE',
                            style: mono.copyWith(color: muted, fontSize: 15),
                          ),
                        ),
                        IconButton(
                          tooltip: 'Close film',
                          onPressed: () => Navigator.pop(context),
                          icon: const Icon(LucideIcons.x),
                        ),
                      ],
                    ),
                    Expanded(
                      child: Center(
                        child: SingleChildScrollView(
                          child: AnimatedSwitcher(
                            duration: MediaQuery.disableAnimationsOf(context)
                                ? Duration.zero
                                : const Duration(milliseconds: 500),
                            child: Column(
                              key: ValueKey(chapter),
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                Text(
                                  chapter < 4
                                      ? const ['I', 'II', 'III', 'IV'][chapter]
                                      : '終',
                                  style: display.copyWith(
                                    color: shu,
                                    fontSize: chapter < 4 ? 40 : 72,
                                  ),
                                ),
                                const SizedBox(height: 18),
                                Text(
                                  titles[chapter],
                                  textAlign: TextAlign.center,
                                  style: display.copyWith(
                                    fontSize: widget.isTV ? 44 : 30,
                                  ),
                                ),
                                const SizedBox(height: 28),
                                if (chapter == 0)
                                  Text(
                                    stats == null
                                        ? 'Every episode, one at a time.'
                                        : '${stats!.episodes} episodes\n${stats!.completed} stories completed\n${stats!.minutes} minutes in other worlds',
                                    textAlign: TextAlign.center,
                                    style: uiStyle(
                                      context,
                                      size: widget.isTV ? 26 : 20,
                                      height: 1.8,
                                    ),
                                  ),
                                if (chapter == 1)
                                  Text(
                                    '“There. The one still watching.”\n“$name?”\n“Yes. ${stats != null && stats!.episodes > 0 ? '${stats!.episodes} episodes, and still going.' : 'Every episode, one at a time.'}”\n“Can they hear us?”\n“Sometimes. In the quiet between episodes, they almost do.”',
                                    textAlign: TextAlign.center,
                                    style: uiStyle(
                                      context,
                                      size: widget.isTV ? 24 : 18,
                                      height: 1.8,
                                    ),
                                  ),
                                if (chapter == 2) ...[
                                  if (favourites.isEmpty)
                                    Text(
                                      'The stories stay with you.',
                                      style: uiStyle(context, size: 22),
                                    )
                                  else
                                    Wrap(
                                      alignment: WrapAlignment.center,
                                      spacing: 10,
                                      runSpacing: 10,
                                      children: [
                                        for (final a in favourites.take(
                                          widget.isTV ? 10 : 6,
                                        ))
                                          SizedBox(
                                            width: widget.isTV ? 90 : 70,
                                            child: Column(
                                              children: [
                                                ClipRRect(
                                                  borderRadius:
                                                      BorderRadius.circular(6),
                                                  child: AspectRatio(
                                                    aspectRatio: 2 / 3,
                                                    child: CoverImage(
                                                      a.cover,
                                                      width: widget.isTV
                                                          ? 90
                                                          : 70,
                                                      height: widget.isTV
                                                          ? 135
                                                          : 105,
                                                    ),
                                                  ),
                                                ),
                                                const SizedBox(height: 5),
                                                Text(
                                                  a.title,
                                                  maxLines: 1,
                                                  overflow:
                                                      TextOverflow.ellipsis,
                                                  style: uiStyle(
                                                    context,
                                                    size: 12,
                                                  ),
                                                ),
                                              ],
                                            ),
                                          ),
                                      ],
                                    ),
                                ],
                                if (chapter == 3)
                                  Text(
                                    '${widget.milestone > 0
                                        ? '${widget.milestone} episodes. A milestone worth keeping.\n'
                                        : stats != null && stats!.completed > 0
                                        ? '${stats!.completed} endings, and still curious.\n'
                                        : ''}${widget.services.store.text('TouchGrass').isNotEmpty ? 'Touch Grass\n' : ''}The one who stayed for the credits.',
                                    textAlign: TextAlign.center,
                                    style: uiStyle(
                                      context,
                                      size: widget.isTV ? 26 : 20,
                                      height: 1.8,
                                    ),
                                  ),
                                if (chapter == 4)
                                  Text(
                                    'Mata ne, $name.',
                                    textAlign: TextAlign.center,
                                    style: uiStyle(context, size: 24),
                                  ),
                              ],
                            ),
                          ),
                        ),
                      ),
                    ),
                    LinearProgressIndicator(
                      value: elapsed / 35000,
                      minHeight: 3,
                      color: shu,
                      backgroundColor: line,
                    ),
                    const SizedBox(height: 14),
                    Wrap(
                      alignment: WrapAlignment.center,
                      spacing: 14,
                      children: [
                        TextButton(
                          onPressed: () => jump(-1),
                          child: const Text('Previous'),
                        ),
                        TextButton(
                          onPressed: () => setState(() => paused = !paused),
                          child: Text(paused ? 'Play' : 'Pause'),
                        ),
                        TextButton(
                          onPressed: () => jump(1),
                          child: const Text('Next chapter'),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _NightSky extends CustomPainter {
  const _NightSky(this.time);
  final double time;
  @override
  void paint(Canvas canvas, Size size) {
    final random = math.Random(12);
    for (var i = 0; i < 90; i++) {
      final x = random.nextDouble() * size.width,
          y = random.nextDouble() * size.height;
      final glow = .15 + .35 * (.5 + .5 * math.sin(time * math.pi * 4 + i));
      canvas.drawCircle(
        Offset(x, y),
        random.nextDouble() * 1.5 + .5,
        Paint()..color = fg.withValues(alpha: glow),
      );
    }
    canvas.drawCircle(
      Offset(size.width * .82, size.height * .22),
      size.shortestSide * .18,
      Paint()
        ..shader =
            RadialGradient(
              colors: [shu.withValues(alpha: .08), Colors.transparent],
            ).createShader(
              Rect.fromCircle(
                center: Offset(size.width * .82, size.height * .22),
                radius: size.shortestSide * .18,
              ),
            ),
    );
  }

  @override
  bool shouldRepaint(_NightSky old) => time != old.time;
}
