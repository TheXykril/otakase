import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../core.dart';
import '../data/models.dart';
import '../desktop/desktop_core.dart';
import '../ui/components.dart';

/// A TV, speaker box or browser the core found to cast to.
class CastDevice {
  CastDevice.fromJson(Json json)
    : id = json['id'] as String? ?? '',
      name = json['name'] as String? ?? 'Device',
      kind = json['kind'] as String? ?? '',
      address = json['address'] as String? ?? '';
  final String id, name, kind, address;

  bool get isBrowser => kind.toLowerCase() == 'browser';

  IconData get icon => switch (kind.toLowerCase()) {
    'browser' => LucideIcons.globe,
    'kodi' => LucideIcons.monitorPlay,
    'dlna' => LucideIcons.tv,
    _ => LucideIcons.cast,
  };

  String get description => switch (kind.toLowerCase()) {
    'browser' => 'Any phone, TV or console on this network',
    'dlna' => 'Smart TV (DLNA)${address.isEmpty ? '' : ' · $address'}',
    'kodi' => 'Kodi${address.isEmpty ? '' : ' · $address'}',
    _ => 'Chromecast${address.isEmpty ? '' : ' · $address'}',
  };
}

/// What the device casting says about itself.
class CastStatus {
  CastStatus.fromJson(Json json)
    : active = json['active'] == true,
      device = json['device'] as String? ?? '',
      kind = json['kind'] as String? ?? '',
      state = json['state'] as String? ?? '',
      position = (json['position'] as num?)?.toDouble() ?? 0,
      duration = (json['duration'] as num?)?.toDouble() ?? 0,
      volume = (json['volume'] as num?)?.toDouble(),
      paused = json['paused'] == true,
      load = (json['load'] as num?)?.toInt() ?? 0,
      note = json['note'] as String? ?? '',
      error = json['error'] as String? ?? '',
      link = json['link'] as String? ?? '',
      viewers = (json['viewers'] as num?)?.toInt() ?? 0;
  final bool active, paused;
  final String device, kind, state, note, error, link;
  final double position, duration;
  final double? volume;
  final int load, viewers;

  bool get playing => state == 'playing' || state == 'buffering';

  String get label => switch (state) {
    'connecting' => 'Connecting to $device',
    'loading' => 'Starting on $device',
    'buffering' => 'Buffering on $device',
    'paused' => 'Paused on $device',
    'playing' => 'Playing on $device',
    'ended' => 'Finished on $device',
    'error' => 'Casting stopped',
    _ => 'Casting to $device',
  };
}

Future<void> _multicast(bool on) async {
  if (isDesktop) return;
  try {
    await const MethodChannel('otakase/device')
        .invokeMethod<void>('localDiscovery', on);
  } catch (_) {
    /* Ethernet and hosts without the lock need none. */
  }
}

/// The core's cast operations.
class CastClient {
  CastClient(this.core);
  final CoreClient core;

  Future<List<CastDevice>> devices({int seconds = 4}) async {
    // Android drops the multicast answers TVs send unless the app holds a
    // Wi-Fi multicast lock while it listens.
    await _multicast(true);
    final Json data;
    try {
      data = await core.request({
        'operation': 'cast_devices',
        'cast': {'timeout': seconds},
      }, timeout: Duration(seconds: seconds + 10));
    } finally {
      await _multicast(false);
    }
    return [
      for (final d in data['devices'] as List? ?? const [])
        if (d is Map) CastDevice.fromJson(d.cast<String, dynamic>()),
    ];
  }

  Future<void> start({
    required CastDevice device,
    required ResolvedStream stream,
    required String title,
    String subtitle = '',
    double start = 0,
    double duration = 0,
  }) async {
    final headers = Map<String, String>.of(stream.headers);
    if (stream.referrer.isNotEmpty) headers['Referer'] = stream.referrer;
    await core.request({
      'operation': 'cast_start',
      'cast': {
        'device': device.id,
        'url': stream.url,
        'headers': headers,
        'hls': stream.hls || stream.url.contains('.m3u8'),
        'subtitle': subtitle,
        'start': start,
        'duration': duration,
        'title': title,
      },
    }, timeout: const Duration(seconds: 15));
  }

  Future<CastStatus> status() async => CastStatus.fromJson(
    await core.request({
      'operation': 'cast_status',
    }, timeout: const Duration(seconds: 12)),
  );

  Future<void> control(String action, [double value = 0]) => core.request({
    'operation': 'cast_control',
    'cast': {'action': action, 'value': value},
  }, timeout: const Duration(seconds: 12));
}

/// Lists the devices on the network and returns the one picked.
Future<CastDevice?> pickCastDevice(BuildContext context, CastClient client) =>
    showDialog<CastDevice>(
      context: context,
      builder: (_) => _CastPicker(client: client),
    );

class _CastPicker extends StatefulWidget {
  const _CastPicker({required this.client});
  final CastClient client;
  @override
  State<_CastPicker> createState() => _CastPickerState();
}

class _CastPickerState extends State<_CastPicker> {
  List<CastDevice> found = [];
  bool searching = true;
  String error = '';

  @override
  void initState() {
    super.initState();
    unawaited(search());
  }

  Future<void> search() async {
    setState(() {
      searching = true;
      error = '';
    });
    try {
      final devices = await widget.client.devices();
      if (mounted) setState(() => found = devices);
    } catch (_) {
      if (mounted) setState(() => error = 'Searching the network failed.');
    } finally {
      if (mounted) setState(() => searching = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final tvs = found.where((d) => !d.isBrowser).toList();
    final browser = found.where((d) => d.isBrowser).toList();
    return AlertDialog(
      title: const Text('Cast to'),
      content: SizedBox(
        width: 460,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (searching)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Row(
                    children: [
                      const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Text(
                          'Looking for TVs on this network',
                          style: uiStyle(context, color: muted),
                        ),
                      ),
                    ],
                  ),
                ),
              if (!searching && tvs.isEmpty)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 8),
                  child: Text(
                    error.isNotEmpty
                        ? error
                        : 'No TVs found. Check that the TV is on and on the '
                              'same Wi-Fi, and that this computer\'s firewall '
                              'lets the app reach the local network.',
                    style: uiStyle(context, color: muted),
                  ),
                ),
              for (final device in [...tvs, ...browser])
                ChoiceRow(
                  key: Key('cast-device-${device.id}'),
                  autofocus: device == [...tvs, ...browser].first,
                  title: Row(
                    children: [
                      Icon(device.icon, color: accentText),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              device.name,
                              style: uiStyle(context, weight: FontWeight.w600),
                            ),
                            Text(
                              device.description,
                              style: uiStyle(context, size: 14, color: muted),
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                  onTap: () => Navigator.pop(context, device),
                ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: searching ? null : search,
          child: const Text('Search again'),
        ),
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Cancel'),
        ),
      ],
    );
  }
}

/// Shown in place of the picture while an episode plays on another screen.
class CastPanel extends StatelessWidget {
  const CastPanel({
    super.key,
    required this.status,
    required this.deviceName,
    required this.onStop,
    this.isTV = false,
  });
  final CastStatus? status;
  final String deviceName;
  final VoidCallback onStop;
  final bool isTV;

  @override
  Widget build(BuildContext context) {
    final s = status;
    final label = s?.label ?? 'Connecting to $deviceName';
    final link = s?.link ?? '';
    return ColoredBox(
      color: bgDeep,
      child: Center(
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(24, 96, 24, 200),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(LucideIcons.cast, size: 56, color: accentText),
              const SizedBox(height: 16),
              Text(
                label,
                key: const Key('cast-label'),
                textAlign: TextAlign.center,
                style: uiStyle(context, size: 22, weight: FontWeight.w600),
              ),
              if (s != null && s.error.isNotEmpty) ...[
                const SizedBox(height: 8),
                Text(
                  s.error,
                  textAlign: TextAlign.center,
                  style: uiStyle(context, color: accentText),
                ),
              ],
              if (s != null && s.note.isNotEmpty) ...[
                const SizedBox(height: 8),
                ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 520),
                  child: Text(
                    s.note,
                    textAlign: TextAlign.center,
                    style: uiStyle(context, color: muted),
                  ),
                ),
              ],
              if (link.isNotEmpty) ...[
                const SizedBox(height: 20),
                Container(
                  color: Colors.white,
                  padding: const EdgeInsets.all(10),
                  child: QrImageView(
                    data: link,
                    size: 180,
                    backgroundColor: Colors.white,
                  ),
                ),
                const SizedBox(height: 12),
                SelectableText(
                  link,
                  key: const Key('cast-link'),
                  style: mono.copyWith(fontSize: 18, color: fgBright),
                ),
                const SizedBox(height: 6),
                Text(
                  s!.viewers == 0
                      ? 'Open this link or scan the code on the screen to '
                            'watch on. It must be on the same network.'
                      : '${s.viewers} ${s.viewers == 1 ? 'screen' : 'screens'} watching',
                  textAlign: TextAlign.center,
                  style: uiStyle(context, size: 14, color: muted),
                ),
              ],
              const SizedBox(height: 20),
              AppButton(
                key: const Key('cast-stop'),
                label: 'Stop casting',
                icon: LucideIcons.x,
                onPressed: onStop,
                isTV: isTV,
              ),
            ],
          ),
        ),
      ),
    );
  }
}
