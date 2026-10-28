import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'components.dart';

class DeviceStatus extends StatefulWidget {
  const DeviceStatus({super.key, this.isTV = false});
  final bool isTV;
  @override
  State<DeviceStatus> createState() => _DeviceStatusState();
}

class _DeviceStatusState extends State<DeviceStatus>
    with WidgetsBindingObserver {
  static final events = const EventChannel('otakase/status')
      .receiveBroadcastStream()
      .asBroadcastStream();
  StreamSubscription<dynamic>? subscription;
  Timer? clock;
  DateTime now = DateTime.now();
  Map<dynamic, dynamic> data = {};
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    subscription = events.listen((event) {
      if (mounted && event is Map) {
        setState(() {
          data = event;
          now = DateTime.now();
        });
      }
    }, onError: (_) {});
    refresh();
    minute();
  }

  void minute() {
    clock = Timer(
      Duration(
        milliseconds: 60000 - DateTime.now().millisecondsSinceEpoch % 60000,
      ),
      () {
        if (mounted) {
          setState(() => now = DateTime.now());
          minute();
        }
      },
    );
  }

  Future<void> refresh() async {
    try {
      final status = await const MethodChannel('otakase/device')
          .invokeMapMethod<dynamic, dynamic>('status');
      if (mounted && status != null) {
        setState(() {
          data = status;
          now = DateTime.now();
        });
      }
    } on MissingPluginException {
      /* Device values are unavailable in previews. */
    } on PlatformException {
      /* Omit unavailable device values. */
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) unawaited(refresh());
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    subscription?.cancel();
    clock?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final time = TimeOfDay.fromDateTime(now).format(context);
    final use24 =
        data['use24Hour'] as bool? ??
        MediaQuery.alwaysUse24HourFormatOf(context);
    final label = use24 ? clockTime(now) : time;
    final battery = data['battery'] as int?;
    final charging = data['charging'] == true;
    final style = mono.copyWith(fontSize: widget.isTV ? 20 : 15, color: muted);
    return Semantics(
      label:
          '$label${!widget.isTV && battery != null ? ', battery $battery percent${charging ? ', charging' : ''}' : ''}',
      excludeSemantics: true,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(label, style: style),
          if (!widget.isTV && battery != null) ...[
            const SizedBox(width: 12),
            CustomPaint(
              size: const Size(24, 16),
              painter: _Battery(battery, charging),
            ),
            const SizedBox(width: 6),
            Text('$battery%', style: style),
          ],
        ],
      ),
    );
  }
}

class _Battery extends CustomPainter {
  _Battery(this.level, this.charging);
  final int level;
  final bool charging;
  @override
  void paint(Canvas canvas, Size size) {
    final color = level <= 15 && !charging ? warn : muted;
    canvas.drawRRect(
      RRect.fromRectAndRadius(
        const Rect.fromLTWH(0, 1, 21, 14),
        const Radius.circular(3),
      ),
      Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.5,
    );
    canvas.drawRect(const Rect.fromLTWH(22, 5, 2, 6), Paint()..color = color);
    canvas.drawRect(
      Rect.fromLTWH(3, 4, 15 * level.clamp(0, 100) / 100, 8),
      Paint()..color = color,
    );
    if (charging) {
      canvas.drawPath(
        Path()
          ..moveTo(13, 2)
          ..lineTo(8, 9)
          ..lineTo(12, 9)
          ..lineTo(9, 14)
          ..lineTo(16, 7)
          ..lineTo(12, 7)
          ..close(),
        Paint()..color = fgBright,
      );
    }
  }

  @override
  bool shouldRepaint(_Battery old) =>
      old.level != level || old.charging != charging;
}
