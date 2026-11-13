import 'dart:async';

import 'package:flutter/material.dart';

import '../cast/cast.dart';
import '../data/tv_remote.dart';
import '../desktop/desktop_core.dart';
import '../services.dart';
import 'components.dart';
import 'tv_remote_page.dart';

/// Where to play: this screen, an Otakase TV or computer this phone
/// controls fully, or a cast device with basic controls.
sealed class PlayTarget {
  const PlayTarget();
}

class PlayHere extends PlayTarget {
  const PlayHere();
}

class PlayOnTV extends PlayTarget {
  const PlayOnTV(this.tv);
  final TVDevice tv;
}

class PlayOnCast extends PlayTarget {
  const PlayOnCast(this.device);
  final CastDevice device;
}

/// One picker for every screen: paired Otakase TVs and computers first,
/// then Chromecast, DLNA, Kodi and browser casting. [here] adds this
/// device; [otakase] leaves out Otakase TVs where they cannot play it.
Future<PlayTarget?> pickPlayTarget(
  BuildContext context,
  AppServices services, {
  bool here = true,
  bool otakase = true,
  bool cast = true,
  CastClient? caster,
}) => showModalBottomSheet<PlayTarget>(
  context: context,
  backgroundColor: panel,
  showDragHandle: true,
  isScrollControlled: true,
  builder: (_) => _PlayOnSheet(
    services: services,
    here: here,
    otakase: otakase,
    cast: cast,
    caster: caster ?? CastClient(services.core),
  ),
);

class _PlayOnSheet extends StatefulWidget {
  const _PlayOnSheet({
    required this.services,
    required this.here,
    required this.otakase,
    required this.cast,
    required this.caster,
  });
  final AppServices services;

  /// [cast] off leaves out cast devices, for a file on this device that
  /// only another Otakase can find again.
  final bool here, otakase, cast;
  final CastClient caster;
  @override
  State<_PlayOnSheet> createState() => _PlayOnSheetState();
}

class _PlayOnSheetState extends State<_PlayOnSheet> {
  List<CastDevice> casts = [];
  List<TVDevice> account = [];
  bool searching = true;

  @override
  void initState() {
    super.initState();
    if (widget.cast) {
      unawaited(search());
    } else {
      searching = false;
    }
    if (widget.otakase && widget.services.store.flag('RemoteAnywhere')) {
      unawaited(
        widget.services.relay
            .tvs()
            .then((tvs) {
              if (mounted) setState(() => account = tvs);
            })
            .catchError((Object _) {}),
      );
    }
  }

  Future<void> search() async {
    try {
      final found = await widget.caster.devices();
      if (mounted) setState(() => casts = found);
    } catch (_) {
      /* Otakase TVs and this screen still work. */
    } finally {
      if (mounted) setState(() => searching = false);
    }
  }

  Widget heading(String text) => Padding(
    padding: const EdgeInsets.fromLTRB(4, 16, 4, 6),
    child: Text(
      text.toUpperCase(),
      style: mono.copyWith(fontSize: 12, letterSpacing: 1.2),
    ),
  );

  Widget row({
    required Key key,
    required IconData icon,
    required String title,
    String? subtitle,
    Color? accent,
    required VoidCallback onTap,
  }) => ListTile(
    key: key,
    contentPadding: const EdgeInsets.symmetric(horizontal: 8),
    shape: RoundedRectangleBorder(
      borderRadius: BorderRadius.circular(KageRadius.tile),
    ),
    leading: Container(
      width: 42,
      height: 42,
      decoration: BoxDecoration(
        color: panelRaised,
        borderRadius: BorderRadius.circular(KageRadius.md),
      ),
      child: Icon(icon, color: accent ?? fg),
    ),
    title: Text(
      title,
      style: uiStyle(context, weight: FontWeight.w600, color: fgBright),
    ),
    subtitle: subtitle == null
        ? null
        : Text(subtitle, style: uiStyle(context, size: 13, color: muted)),
    onTap: onTap,
  );

  @override
  Widget build(BuildContext context) {
    final linked = widget.services.remoteLink.value?.tv.id;
    final tvs = widget.otakase
        ? {
            for (final tv in [...account, ...widget.services.tvRemote.paired])
              tv.id: tv,
          }.values.toList()
        : const <TVDevice>[];
    final mine = {for (final tv in account) tv.id};
    return SafeArea(
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(context).height * .8,
        ),
        child: ListView(
          shrinkWrap: true,
          padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 4),
              child: Text(
                'Play on',
                style: uiStyle(
                  context,
                  size: 20,
                  weight: FontWeight.w700,
                  color: fgBright,
                ),
              ),
            ),
            if (widget.otakase) ...[
              heading('Otakase · full remote'),
              for (final tv in tvs)
                row(
                  key: Key('play-on-tv-${tv.id}'),
                  icon: LucideIcons.tv,
                  accent: tv.id == linked ? shu : null,
                  title: tv.name,
                  subtitle: tv.id == linked
                      ? 'Connected'
                      : widget.services.tvRemote.isPaired(tv.id)
                      ? 'Paired'
                      : mine.contains(tv.id)
                      ? 'Your account, through Discord'
                      : 'Paired',
                  onTap: () => Navigator.pop(context, PlayOnTV(tv)),
                ),
              row(
                key: const Key('play-on-pair'),
                icon: LucideIcons.plus,
                title: tvs.isEmpty ? 'Pair a TV or computer' : 'Pair another',
                subtitle: 'Otakase on the same Wi-Fi',
                onTap: () {
                  final navigator = Navigator.of(context);
                  navigator.pop();
                  unawaited(
                    navigator.push(
                      OtakaseRoute<void>(
                        builder: (_) => TVRemotePage(
                          services: widget.services,
                          isTV: false,
                        ),
                      ),
                    ),
                  );
                },
              ),
            ],
            if (widget.here) ...[
              heading('This device'),
              row(
                key: const Key('play-on-here'),
                icon: isDesktop ? LucideIcons.monitor : LucideIcons.smartphone,
                title: isDesktop ? 'This computer' : 'This phone',
                onTap: () => Navigator.pop(context, const PlayHere()),
              ),
            ],
            if (widget.cast) heading('Cast · basic controls'),
            if (!widget.cast)
              const SizedBox.shrink()
            else if (searching)
              Padding(
                padding: const EdgeInsets.all(12),
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
                        'Looking for cast devices',
                        style: uiStyle(context, color: muted),
                      ),
                    ),
                  ],
                ),
              )
            else if (casts.isEmpty)
              Padding(
                padding: const EdgeInsets.all(12),
                child: Text(
                  'No cast devices on this network.',
                  style: uiStyle(context, color: muted),
                ),
              ),
            for (final device in casts)
              row(
                key: Key('play-on-cast-${device.id}'),
                icon: device.icon,
                title: device.isBrowser ? 'Any browser' : device.name,
                subtitle: device.isBrowser
                    ? 'Link and QR code on your network'
                    : device.kind,
                onTap: () => Navigator.pop(context, PlayOnCast(device)),
              ),
          ],
        ),
      ),
    );
  }
}
