import 'package:flutter/material.dart';

import 'components.dart';

class EpisodeProgressThread extends StatelessWidget {
  const EpisodeProgressThread({
    super.key,
    required this.episode,
    required this.current,
    required this.started,
    required this.x,
    required this.child,
  });
  final int episode, current;
  final bool started;
  final double x;
  final Widget child;
  @override
  Widget build(BuildContext context) => Stack(
    children: [
      child,
      if (started && episode <= current)
        Positioned.fill(
          child: IgnorePointer(
            child: CustomPaint(
              painter: EpisodeThreadPainter(
                episode: episode,
                current: current,
                x: x,
              ),
            ),
          ),
        ),
      if (started && episode == current)
        Positioned(
          left: x - 3.5,
          top: 0,
          bottom: 0,
          width: 7,
          child: IgnorePointer(
            child: Center(
              child: Container(
                key: ValueKey('episode-thread-node-$episode'),
                width: 7,
                height: 7,
                decoration: const BoxDecoration(
                  color: shu,
                  shape: BoxShape.circle,
                ),
              ),
            ),
          ),
        ),
    ],
  );
}

class EpisodeThreadPainter extends CustomPainter {
  const EpisodeThreadPainter({
    required this.episode,
    required this.current,
    required this.x,
  });
  final int episode, current;
  final double x;
  @override
  void paint(Canvas canvas, Size size) {
    canvas.drawLine(
      Offset(x, episode == 1 ? size.height / 2 : 0),
      Offset(x, episode == current ? size.height / 2 : size.height),
      Paint()
        ..color = shu
        ..strokeWidth = 3,
    );
  }

  @override
  bool shouldRepaint(EpisodeThreadPainter old) =>
      old.episode != episode || old.current != current || old.x != x;
}

class KageSeekSlider extends StatelessWidget {
  const KageSeekSlider({
    super.key,
    required this.value,
    required this.max,
    required this.onChanged,
    required this.onChangeStart,
    required this.onChangeEnd,
  });
  final double value, max;
  final ValueChanged<double> onChanged, onChangeStart, onChangeEnd;
  @override
  Widget build(BuildContext context) => SliderTheme(
    data: SliderTheme.of(context).copyWith(
      trackHeight: 5,
      activeTrackColor: shu,
      inactiveTrackColor: const Color(0xff5a5148),
      thumbShape: const _ThreadThumb(),
    ),
    child: Slider(
      value: value,
      max: max,
      onChanged: onChanged,
      onChangeStart: onChangeStart,
      onChangeEnd: onChangeEnd,
    ),
  );
}

class _ThreadThumb extends SliderComponentShape {
  const _ThreadThumb();
  @override
  Size getPreferredSize(bool isEnabled, bool isDiscrete) =>
      const Size.square(13);
  @override
  void paint(
    PaintingContext context,
    Offset center, {
    required Animation<double> activationAnimation,
    required Animation<double> enableAnimation,
    required bool isDiscrete,
    required TextPainter labelPainter,
    required RenderBox parentBox,
    required SliderThemeData sliderTheme,
    required TextDirection textDirection,
    required double value,
    required double textScaleFactor,
    required Size sizeWithOverflow,
  }) {
    context.canvas.drawCircle(center, 6.5, Paint()..color = fgBright);
    context.canvas.drawCircle(
      center,
      5.5,
      Paint()
        ..color = shu
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2,
    );
  }
}
