import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../desktop/player_bar.dart';
import 'components.dart';
import 'device_status.dart';
import 'progress_thread.dart';
import 'seal.dart';

/// One icon button on the player's time row.
class PlayerButton {
  const PlayerButton({
    required this.icon,
    required this.label,
    required this.onPressed,
    this.key,
  });
  final IconData icon;
  final String label;

  /// Null shows the button dimmed.
  final VoidCallback? onPressed;
  final Key? key;
}

/// Phone and TV player controls, shared by the anime and the film/series
/// players so both look the same: top bar (back, title, cast, status, seal),
/// −10 / play / +10 in the middle, timeline with skip stripes, and the
/// times with plain icon buttons between them.
class PhonePlayerControls extends StatelessWidget {
  const PhonePlayerControls({
    super.key,
    required this.isTV,
    required this.title,
    required this.subtitle,
    required this.position,
    required this.duration,
    required this.playing,
    required this.buffering,
    required this.onBack,
    required this.onPlayPause,
    required this.onSeekBy,
    required this.onSeekStart,
    required this.onSeekChanged,
    required this.onSeekEnd,
    required this.actions,
    this.playFocus,
    this.spans = const [],
    this.topActions = const [],
    this.above = const [],
  });
  final bool isTV, playing, buffering;
  final String title, subtitle;
  final Duration position, duration;
  final List<SkipSpan> spans;
  final FocusNode? playFocus;
  final VoidCallback onBack, onPlayPause;
  final ValueChanged<int> onSeekBy;
  final ValueChanged<double> onSeekStart, onSeekChanged, onSeekEnd;
  final List<PlayerButton> actions;

  /// Extra icon buttons in the top bar, before the device status (cast).
  final List<Widget> topActions;

  /// Shown right above the timeline (skip chip, next episode card).
  final List<Widget> above;

  @override
  Widget build(BuildContext context) {
    final max = math.max(1, duration.inMilliseconds).toDouble();
    final timeStyle = mono.copyWith(
      fontSize: isTV ? 20 : 15,
      color: fg,
      letterSpacing: 0,
    );
    // The shading runs edge to edge, under a camera cutout too, so it lines
    // up with the video. Only the buttons and text keep to the safe area.
    return Column(
      children: [
        Container(
          decoration: const BoxDecoration(
            gradient: LinearGradient(
              begin: Alignment.topCenter,
              end: Alignment.bottomCenter,
              colors: [bgDeep, Color(0x00080707)],
            ),
          ),
          child: SafeArea(
            bottom: false,
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Row(
                children: [
                  IconButton(
                    tooltip: 'Back',
                    onPressed: onBack,
                    icon: const Icon(LucideIcons.arrowLeft, color: fgBright),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          title,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: uiStyle(
                            context,
                            size: 20,
                            weight: FontWeight.w600,
                            color: fgBright,
                          ),
                        ),
                        if (subtitle.isNotEmpty) ...[
                          const SizedBox(height: 4),
                          Text(
                            subtitle.toUpperCase(),
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: uiStyle(context, size: 15, color: muted),
                          ),
                        ],
                      ],
                    ),
                  ),
                  ...topActions,
                  TopBarEnd(isTV: isTV),
                  const SizedBox(width: 12),
                  const SizedBox(
                    width: 48,
                    height: 48,
                    child: Center(child: OtakaseSeal()),
                  ),
                ],
              ),
            ),
          ),
        ),
        Expanded(
          child: SafeArea(
            top: false,
            bottom: false,
            child: Center(
              child: Wrap(
                alignment: WrapAlignment.center,
                spacing: 24,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  TransportButton(
                    label: 'Rewind 10 seconds',
                    text: '−10',
                    onPressed: () => onSeekBy(-10),
                  ),
                  TransportButton(
                    label: playing ? 'Pause' : 'Play',
                    icon: playing ? LucideIcons.pause : LucideIcons.play,
                    primary: true,
                    focusNode: playFocus,
                    onPressed: onPlayPause,
                  ),
                  TransportButton(
                    label: 'Forward 10 seconds',
                    text: '+10',
                    onPressed: () => onSeekBy(10),
                  ),
                ],
              ),
            ),
          ),
        ),
        for (final widget in above)
          SafeArea(top: false, bottom: false, child: widget),
        Container(
          decoration: const BoxDecoration(
            gradient: LinearGradient(
              begin: Alignment.topCenter,
              end: Alignment.bottomCenter,
              colors: [Color(0x00080707), bgDeep],
            ),
          ),
          child: SafeArea(
            top: false,
            child: Padding(
              padding: EdgeInsets.fromLTRB(24, 8, 24, isTV ? 40 : 12),
              child: Column(
                children: [
                  SizedBox(
                    height: 44,
                    child: Stack(
                      alignment: Alignment.center,
                      children: [
                        Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 24),
                          child: CustomPaint(
                            size: const Size(double.infinity, 6),
                            painter: SkipPainter(spans, duration),
                          ),
                        ),
                        KageSeekSlider(
                          key: const Key('player-timeline'),
                          value: position.inMilliseconds
                              .clamp(0, max)
                              .toDouble(),
                          max: max,
                          onChangeStart: onSeekStart,
                          onChanged: onSeekChanged,
                          onChangeEnd: onSeekEnd,
                        ),
                      ],
                    ),
                  ),
                  Row(
                    children: [
                      Text(
                        '${DesktopPlayerBar.time(position)} / ${DesktopPlayerBar.time(duration)}',
                        style: timeStyle,
                      ),
                      if (buffering) ...[
                        const SizedBox(width: 16),
                        Text(
                          'Buffering',
                          style: uiStyle(context, size: 15, color: muted),
                        ),
                      ],
                      const Spacer(),
                      for (final a in actions)
                        PlayerIconButton(action: a, isTV: isTV),
                      SizedBox(width: actions.isEmpty ? 0 : 12),
                      Text(
                        '−${DesktopPlayerBar.time(duration - position)}',
                        style: timeStyle,
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

/// Plain icon on the time row; holding it (or hovering with a mouse) shows
/// its name. A TV remote cannot hold, so on TV the focused button writes its
/// name under the icon.
class PlayerIconButton extends StatefulWidget {
  const PlayerIconButton({super.key, required this.action, required this.isTV});
  final PlayerButton action;
  final bool isTV;
  @override
  State<PlayerIconButton> createState() => _PlayerIconButtonState();
}

class _PlayerIconButtonState extends State<PlayerIconButton> {
  bool focused = false;
  @override
  Widget build(BuildContext context) {
    final a = widget.action, tv = widget.isTV;
    final enabled = a.onPressed != null;
    final size = tv ? 52.0 : 44.0;
    return Semantics(
      label: a.label,
      button: true,
      enabled: enabled,
      child: Tooltip(
        message: a.label,
        triggerMode: TooltipTriggerMode.longPress,
        child: FocusBox(
          key: a.key,
          isTV: tv,
          radius: KageRadius.full,
          onPressed: a.onPressed,
          onFocusChanged: tv ? (f) => setState(() => focused = f) : null,
          child: ExcludeSemantics(
            child: SizedBox(
              width: size,
              height: size,
              child: Stack(
                clipBehavior: Clip.none,
                alignment: Alignment.center,
                children: [
                  Icon(
                    a.icon,
                    color: enabled ? fgBright : disabled,
                    size: tv ? 26 : 22,
                  ),
                  if (tv && focused)
                    Positioned(
                      top: size + 6,
                      child: Text(
                        a.label,
                        maxLines: 1,
                        softWrap: false,
                        style: uiStyle(context, color: fgBright, height: 1.2),
                      ),
                    ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Small square-cornered chip with an icon, used for "Skip opening" on the
/// player instead of a round pill button.
class PlayerChip extends StatelessWidget {
  const PlayerChip({
    super.key,
    required this.icon,
    required this.label,
    required this.onPressed,
    required this.isTV,
    this.focusNode,
  });
  final IconData icon;
  final String label;
  final VoidCallback onPressed;
  final bool isTV;
  final FocusNode? focusNode;
  @override
  Widget build(BuildContext context) => Semantics(
    label: label,
    button: true,
    child: FocusBox(
      isTV: isTV,
      focusNode: focusNode,
      radius: KageRadius.sm,
      onPressed: onPressed,
      child: Container(
        padding: EdgeInsets.symmetric(
          horizontal: isTV ? 18 : 14,
          vertical: isTV ? 12 : 9,
        ),
        decoration: BoxDecoration(
          color: const Color(0xe6191614),
          border: Border.all(color: line),
          borderRadius: BorderRadius.circular(KageRadius.sm),
        ),
        child: ExcludeSemantics(
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(icon, size: isTV ? 22 : 18, color: fgBright),
              const SizedBox(width: 8),
              Text(label, style: uiStyle(context, color: fgBright)),
            ],
          ),
        ),
      ),
    ),
  );
}

class SkipPainter extends CustomPainter {
  SkipPainter(this.spans, this.duration);
  final List<SkipSpan> spans;
  final Duration duration;
  @override
  void paint(Canvas canvas, Size size) {
    if (duration.inMilliseconds <= 0) return;
    for (final span in spans) {
      final left =
          (span.start * 1000 / duration.inMilliseconds).clamp(0.0, 1.0) *
          size.width;
      final right =
          (span.end * 1000 / duration.inMilliseconds).clamp(0.0, 1.0) *
          size.width;
      final region = Rect.fromLTRB(left, 0, right, 7);
      canvas.save();
      canvas.clipRect(region);
      canvas.drawRect(region, Paint()..color = const Color(0xff302923));
      for (double x = left - 7; x < right; x += 10) {
        canvas.drawPath(
          Path()
            ..moveTo(x, 7)
            ..lineTo(x + 7, 0)
            ..lineTo(x + 11, 0)
            ..lineTo(x + 4, 7)
            ..close(),
          Paint()..color = fg,
        );
      }
      canvas.restore();
    }
  }

  @override
  bool shouldRepaint(SkipPainter old) =>
      old.spans != spans || old.duration != duration;
}
