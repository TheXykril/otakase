import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../ui/components.dart';
import '../ui/progress_thread.dart';
import '../ui/seal.dart';
import 'window_frame.dart';

/// The desktop player controls, styled like the CLI's mpv skin: a title line
/// at the top and one compact floating bar at the bottom with times, a seek
/// rail with opening and ending stripes, and small icon buttons.
class DesktopPlayerBar extends StatelessWidget {
  const DesktopPlayerBar({
    super.key,
    required this.title,
    required this.subtitle,
    required this.position,
    required this.duration,
    required this.buffered,
    required this.playing,
    required this.buffering,
    required this.silent,
    required this.fullscreen,
    required this.trackLabel,
    this.centerLabel = '',
    required this.stripes,
    required this.onBack,
    required this.onPlayPause,
    required this.onSeekBy,
    required this.onSeekStart,
    required this.onSeekChanged,
    required this.onSeekEnd,
    required this.onNext,
    required this.onMute,
    required this.onTracks,
    required this.onQuality,
    required this.onFullscreen,
    this.playFocus,
    this.casting = false,
    this.onCast,
    this.onSkipTimes,
  });

  final String title, subtitle, trackLabel, centerLabel;
  final Duration position, duration, buffered;
  final bool playing, buffering, silent, fullscreen;
  final List<SkipSpan> stripes;
  final VoidCallback onBack, onPlayPause, onMute, onTracks, onQuality;
  final VoidCallback onFullscreen;
  final VoidCallback? onNext;

  /// Opens the cast picker; null hides the button.
  final VoidCallback? onCast;

  /// Opens the skip times panel; the anime player only.
  final VoidCallback? onSkipTimes;
  final bool casting;
  final ValueChanged<int> onSeekBy;
  final ValueChanged<double> onSeekStart, onSeekChanged, onSeekEnd;
  final FocusNode? playFocus;

  static String time(Duration d) {
    final s = d.inSeconds.abs();
    final h = s ~/ 3600, m = s % 3600 ~/ 60, sec = s % 60;
    String two(int v) => v.toString().padLeft(2, '0');
    return h > 0 ? '$h:${two(m)}:${two(sec)}' : '${two(m)}:${two(sec)}';
  }

  @override
  Widget build(BuildContext context) {
    final total = math.max(1, duration.inMilliseconds).toDouble();
    final small = mono.copyWith(fontSize: 13, color: muted, letterSpacing: 0);
    return Stack(
      children: [
        const Positioned.fill(
          child: IgnorePointer(
            child: DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topCenter,
                  end: Alignment.bottomCenter,
                  stops: [0, .18, .7, 1],
                  colors: [
                    Color(0xb3000000),
                    Color(0x00000000),
                    Color(0x00000000),
                    Color(0xcc000000),
                  ],
                ),
              ),
            ),
          ),
        ),
        Positioned(
          top: 0,
          left: 0,
          right: 0,
          child: WindowDragArea(
            child: Padding(
              padding: EdgeInsets.fromLTRB(
                16 + windowButtonsLeading,
                14,
                16,
                14,
              ),
              child: Row(
                children: [
                  _BarIcon(
                    key: const Key('player-back'),
                    icon: LucideIcons.arrowLeft,
                    tooltip: 'Back',
                    onPressed: onBack,
                  ),
                  const SizedBox(width: 8),
                  const OtakaseSeal(size: 26),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text.rich(
                      TextSpan(
                        text: title,
                        style: uiStyle(
                          context,
                          size: 18,
                          weight: FontWeight.w600,
                          color: fgBright,
                        ),
                        children: [
                          TextSpan(
                            text: '   $subtitle',
                            style: uiStyle(context, size: 15, color: muted),
                          ),
                        ],
                      ),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  const WindowButtons(),
                ],
              ),
            ),
          ),
        ),
        Positioned(
          left: 0,
          right: 0,
          bottom: 28,
          child: Center(
            child: Container(
              key: const Key('desktop-player-bar'),
              width: 960,
              padding: const EdgeInsets.fromLTRB(18, 10, 18, 8),
              decoration: BoxDecoration(
                color: const Color(0xe60e0d0c),
                border: Border.all(color: line),
                borderRadius: BorderRadius.circular(12),
              ),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Row(
                    children: [
                      Text(time(position), style: small),
                      const Spacer(),
                      Text(buffering ? 'Buffering' : centerLabel, style: small),
                      const Spacer(),
                      Text('-${time(duration - position)}', style: small),
                    ],
                  ),
                  SizedBox(
                    height: 30,
                    child: Stack(
                      alignment: Alignment.center,
                      children: [
                        Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 24),
                          child: CustomPaint(
                            size: const Size(double.infinity, 5),
                            painter: SeekRailPainter(
                              stripes: stripes,
                              buffered: buffered.inMilliseconds / total,
                              seconds: total / 1000,
                            ),
                          ),
                        ),
                        KageSeekSlider(
                          key: const Key('player-timeline'),
                          value: position.inMilliseconds
                              .clamp(0, total)
                              .toDouble(),
                          max: total,
                          inactiveColor: Colors.transparent,
                          onChangeStart: onSeekStart,
                          onChanged: onSeekChanged,
                          onChangeEnd: onSeekEnd,
                        ),
                        // Opening and ending stripes stay visible over the
                        // played part, like the CLI skin.
                        IgnorePointer(
                          child: Padding(
                            padding: const EdgeInsets.symmetric(horizontal: 24),
                            child: CustomPaint(
                              size: const Size(double.infinity, 5),
                              painter: SeekRailPainter(
                                stripes: stripes,
                                buffered: 0,
                                seconds: total / 1000,
                                rail: false,
                              ),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                  Row(
                    children: [
                      _BarIcon(
                        icon: LucideIcons.rotateCcw,
                        tooltip: 'Back 10 seconds',
                        onPressed: () => onSeekBy(-10),
                      ),
                      _BarIcon(
                        key: const Key('player-play'),
                        icon: playing ? LucideIcons.pause : LucideIcons.play,
                        tooltip: playing ? 'Pause' : 'Play',
                        focusNode: playFocus,
                        onPressed: onPlayPause,
                      ),
                      _BarIcon(
                        icon: LucideIcons.rotateCw,
                        tooltip: 'Forward 10 seconds',
                        onPressed: () => onSeekBy(10),
                      ),
                      _BarIcon(
                        key: const Key('player-next'),
                        icon: LucideIcons.skipForward,
                        tooltip: 'Next episode',
                        onPressed: onNext,
                      ),
                      _BarIcon(
                        key: const Key('player-mute'),
                        icon: silent
                            ? LucideIcons.volumeX
                            : LucideIcons.volume2,
                        tooltip: silent ? 'Unmute' : 'Mute',
                        onPressed: onMute,
                      ),
                      const Spacer(),
                      Text(trackLabel, style: small),
                      const SizedBox(width: 4),
                      _BarIcon(
                        key: const Key('player-tracks'),
                        icon: LucideIcons.captions,
                        tooltip: 'Audio and subtitles',
                        onPressed: onTracks,
                      ),
                      _BarIcon(
                        key: const Key('player-quality'),
                        icon: LucideIcons.settings2,
                        tooltip: 'Quality',
                        onPressed: onQuality,
                      ),
                      if (onSkipTimes != null)
                        _BarIcon(
                          key: const Key('player-skip-times'),
                          icon: LucideIcons.flag,
                          tooltip: 'Skip times',
                          onPressed: onSkipTimes!,
                        ),
                      if (onCast != null)
                        _BarIcon(
                          key: const Key('player-cast'),
                          icon: LucideIcons.cast,
                          tooltip: casting ? 'Casting' : 'Cast to a TV',
                          onPressed: onCast!,
                          active: casting,
                        ),
                      _BarIcon(
                        key: const Key('player-fullscreen'),
                        icon: fullscreen
                            ? LucideIcons.minimize
                            : LucideIcons.maximize,
                        tooltip: fullscreen
                            ? 'Leave full screen'
                            : 'Full screen',
                        onPressed: onFullscreen,
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ),
        ),
      ],
    );
  }
}

class _BarIcon extends StatelessWidget {
  const _BarIcon({
    super.key,
    required this.icon,
    required this.tooltip,
    required this.onPressed,
    this.focusNode,
    this.active = false,
  });
  final bool active;
  final IconData icon;
  final String tooltip;
  final VoidCallback? onPressed;
  final FocusNode? focusNode;
  @override
  Widget build(BuildContext context) => IconButton(
    tooltip: tooltip,
    focusNode: focusNode,
    onPressed: onPressed,
    iconSize: 20,
    color: active ? accentText : fg,
    disabledColor: line,
    hoverColor: panelRaised,
    focusColor: panelRaised,
    visualDensity: VisualDensity.compact,
    icon: Icon(icon),
  );
}

/// Draws the mpv skin rail under the seek slider: the buffered part and the
/// opening and ending stripes.
class SeekRailPainter extends CustomPainter {
  SeekRailPainter({
    required this.stripes,
    required this.buffered,
    required this.seconds,
    this.rail = true,
  });
  final List<SkipSpan> stripes;
  final double buffered, seconds;

  /// Paints the rail and buffered part; false paints only the stripes.
  final bool rail;

  @override
  void paint(Canvas canvas, Size size) {
    final h = size.height;
    final top = (size.height - h) / 2;
    if (rail) {
      canvas.drawRRect(
        RRect.fromLTRBR(0, top, size.width, top + h, const Radius.circular(3)),
        Paint()..color = const Color(0xff5a5148),
      );
    }
    final b = rail ? buffered.clamp(0.0, 1.0) * size.width : 0.0;
    if (b > 0) {
      canvas.drawRRect(
        RRect.fromLTRBR(0, top, b, top + h, const Radius.circular(3)),
        Paint()..color = const Color(0xffa09282),
      );
    }
    if (seconds <= 0 || rail) return;
    for (final s in stripes) {
      final left = (s.start / seconds).clamp(0.0, 1.0) * size.width;
      final right = (s.end / seconds).clamp(0.0, 1.0) * size.width;
      if (right <= left) continue;
      final region = Rect.fromLTRB(left, top - 1, right, top + h + 1);
      canvas.save();
      canvas.clipRect(region);
      canvas.drawRect(region, Paint()..color = const Color(0xff302923));
      for (double x = left; x < right; x += 8) {
        canvas.drawRect(
          Rect.fromLTRB(x + 4, region.top, x + 8, region.bottom),
          Paint()..color = const Color(0xffe3b45a),
        );
      }
      canvas.restore();
    }
  }

  @override
  bool shouldRepaint(SeekRailPainter old) =>
      old.rail != rail ||
      old.buffered != buffered ||
      old.seconds != seconds ||
      old.stripes.length != stripes.length;
}
