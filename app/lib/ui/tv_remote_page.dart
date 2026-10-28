import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../data/models.dart';
import '../data/tv_remote.dart';
import '../services.dart';
import 'components.dart';
import 'device_status.dart';

class TVRemotePage extends StatefulWidget {
  const TVRemotePage({
    super.key,
    required this.services,
    required this.isTV,
    this.pairCode,
    this.findTVs = discoverTVs,
  });
  final AppServices services;
  final bool isTV;
  final RemotePairCode? pairCode;
  final Future<List<TVDevice>> Function() findTVs;
  @override
  State<TVRemotePage> createState() => _TVRemotePageState();
}

class _TVRemotePageState extends State<TVRemotePage> {
  TVRemote get remote => widget.services.tvRemote;
  List<TVDevice> devices = [];
  TVDevice? selected;
  Json status = {};
  Timer? timer;
  String? error;
  bool busy = false, polling = false;
  @override
  void initState() {
    super.initState();
    if (widget.isTV) {
      unawaited(newCode());
      timer = Timer.periodic(const Duration(seconds: 1), (_) {
        if (mounted) setState(() {});
      });
    } else {
      unawaited(scan());
      timer = Timer.periodic(const Duration(seconds: 2), (_) => refresh());
      if (widget.pairCode case final code?) {
        WidgetsBinding.instance.addPostFrameCallback(
          (_) => pair(code.device, initial: code.code),
        );
      }
    }
  }

  Future<void> newCode() async {
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await remote.beginPair();
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> scan() async {
    await remote.initialize();
    if (!mounted) return;
    setState(() {
      busy = true;
      error = null;
      devices = remote.paired;
    });
    try {
      try {
        await const MethodChannel('otakase/device')
            .invokeMethod<void>('localDiscovery', true);
      } catch (_) {
        /* Ethernet needs no Wi-Fi lock. */
      }
      final found = await widget.findTVs();
      if (!mounted) return;
      for (final tv in found) {
        remote.found(tv);
      }
      setState(
        () => devices = {
          for (final tv in [...remote.paired, ...found]) tv.id: tv,
        }.values.toList(),
      );
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    } finally {
      try {
        await const MethodChannel('otakase/device')
            .invokeMethod<void>('localDiscovery', false);
      } catch (_) {
        /* The lock is optional. */
      }
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> pair(TVDevice tv, {String initial = ''}) async {
    final code = await showDialog<String>(
      context: context,
      builder: (_) => _PairDialog(name: tv.name, initial: initial),
    );
    if (code == null || !mounted) return;
    try {
      await remote.pair(
        tv,
        code,
        phoneName: widget.services.profiles?.active.name ?? 'Phone',
      );
      if (mounted) {
        setState(() => selected = tv);
        await refresh();
      }
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    }
  }

  Future<void> refresh() async {
    final tv = selected;
    if (tv == null || polling || !mounted) return;
    polling = true;
    try {
      final value = await remote.send(tv);
      if (mounted && selected?.id == tv.id) {
        setState(() {
          status = value;
          error = null;
        });
      }
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    } finally {
      polling = false;
    }
  }

  Future<void> send(String action, {Json args = const {}}) async {
    final tv = selected;
    if (tv == null) return;
    try {
      await remote.send(tv, action: action, args: args);
      if (mounted) {
        setState(() => error = null);
        unawaited(refresh());
      }
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    }
  }

  @override
  void dispose() {
    timer?.cancel();
    if (widget.isTV) remote.endPair();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: remote,
    builder: (context, _) {
      final code = remote.code,
          left = remote.code?.expires.difference(DateTime.now()).inSeconds ?? 0;
      final visible =
          status['adult'] != true || widget.services.store.adultAllowed;
      return Scaffold(
        appBar: AppBar(
          title: Text(widget.isTV ? 'Phone remote' : 'Control TV'),
          actions: [
            DeviceStatus(isTV: widget.isTV),
            const SizedBox(width: 16),
          ],
        ),
        body: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            PageHeading(
              widget.isTV ? 'Your phone, your remote' : 'Your TV, from here',
              '繋',
            ),
            const SizedBox(height: 20),
            Text(
              'Keep both devices on the same local network.',
              style: uiStyle(context, color: muted),
            ),
            const SizedBox(height: 20),
            if (widget.isTV) ...[
              if (code != null && left > 0) ...[
                Center(
                  child: ColoredBox(
                    color: Colors.white,
                    child: QrImageView(data: code.qr.toString(), size: 184),
                  ),
                ),
                const SizedBox(height: 18),
                Text(
                  code.code,
                  textAlign: TextAlign.center,
                  style: mono.copyWith(fontSize: 38, color: accentText),
                ),
                Text(
                  'Scan with your phone, or enter this code in More > Control TV.\nExpires in ${left ~/ 60} min ${left % 60} s.',
                  textAlign: TextAlign.center,
                  style: uiStyle(context),
                ),
                const SizedBox(height: 20),
              ],
              AppButton(
                label: code != null && left > 0
                    ? 'New pairing code'
                    : 'Show pairing code',
                autofocus: true,
                isTV: true,
                onPressed: busy ? null : newCode,
              ),
              const SizedBox(height: 12),
              AppButton(
                label: 'Forget paired phones',
                isTV: true,
                onPressed: () async {
                  final yes = await showDialog<bool>(
                    context: context,
                    builder: (c) => AlertDialog(
                      title: const Text('Forget paired phones?'),
                      actions: [
                        TextButton(
                          autofocus: true,
                          onPressed: () => Navigator.pop(c, false),
                          child: const Text('Keep phones'),
                        ),
                        TextButton(
                          onPressed: () => Navigator.pop(c, true),
                          child: const Text('Forget'),
                        ),
                      ],
                    ),
                  );
                  if (yes == true) await remote.forgetPhones();
                },
              ),
            ] else ...[
              AppButton(
                label: busy ? 'Looking for TVs…' : 'Find TVs',
                autofocus: true,
                isTV: false,
                onPressed: busy ? null : scan,
              ),
              const SizedBox(height: 16),
              if (!busy && devices.isEmpty)
                Text(
                  'Open More > Phone remote on the TV, then scan its QR code or find TVs again.',
                  style: uiStyle(context),
                ),
              for (final tv in devices)
                Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: AppButton(
                    label:
                        '${tv.name}${remote.paired.any((p) => p.id == tv.id) ? ' · Paired' : ''}',
                    isTV: false,
                    onPressed: () async {
                      if (remote.paired.any((p) => p.id == tv.id)) {
                        setState(() => selected = tv);
                        await refresh();
                      } else {
                        await pair(tv);
                      }
                    },
                  ),
                ),
              if (selected case final tv?) ...[
                const SizedBox(height: 20),
                Text(
                  tv.name,
                  style: uiStyle(context, size: 24, weight: FontWeight.w600),
                ),
                Text(
                  visible
                      ? (status['title'] as String? ??
                            'Open an episode on the TV')
                      : 'Playing on TV',
                  style: uiStyle(context),
                ),
                if (visible && number(status['episode']) > 0)
                  Text(
                    'Episode ${number(status['episode'])} · ${number(status['position'])} s',
                    style: uiStyle(context, color: muted),
                  ),
                const SizedBox(height: 16),
                Wrap(
                  spacing: 12,
                  runSpacing: 12,
                  children: [
                    AppButton(
                      label: 'Play',
                      isTV: false,
                      onPressed: () => send('play'),
                    ),
                    AppButton(
                      label: 'Pause',
                      isTV: false,
                      onPressed: () => send('pause'),
                    ),
                    AppButton(
                      label: '−10 s',
                      isTV: false,
                      onPressed: () => send('seek', args: {'seconds': -10}),
                    ),
                    AppButton(
                      label: '+10 s',
                      isTV: false,
                      onPressed: () => send('seek', args: {'seconds': 10}),
                    ),
                    AppButton(
                      label: 'Next episode',
                      isTV: false,
                      onPressed: () => send('next'),
                    ),
                    if (visible &&
                        status['room'] != true &&
                        number(status['episodes']) > 0)
                      AppButton(
                        label: 'Pick episode',
                        isTV: false,
                        onPressed: () async {
                          final ep = await chooseValue(
                            context,
                            'Episode',
                            List.generate(
                              number(status['episodes']).clamp(1, 10000),
                              (i) => '${i + 1}',
                            ),
                            selected: '${number(status['episode'])}',
                          );
                          if (ep != null) {
                            await send(
                              'episode',
                              args: {'episode': int.parse(ep)},
                            );
                          }
                        },
                      ),
                    AppButton(
                      label: 'Volume −',
                      isTV: false,
                      onPressed: () => send(
                        'volume',
                        args: {
                          'volume': (number(status['volume']) - 10).clamp(
                            0,
                            100,
                          ),
                        },
                      ),
                    ),
                    AppButton(
                      label: 'Volume +',
                      isTV: false,
                      onPressed: () => send(
                        'volume',
                        args: {
                          'volume': (number(status['volume']) + 10).clamp(
                            0,
                            100,
                          ),
                        },
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 16),
                TextButton(
                  onPressed: () async {
                    await remote.forget(tv.id);
                    if (mounted) {
                      setState(() {
                        selected = null;
                        status = {};
                        devices = remote.paired;
                      });
                    }
                  },
                  child: const Text('Forget TV'),
                ),
              ],
            ],
            if (error != null)
              Padding(
                padding: const EdgeInsets.only(top: 16),
                child: Text(error!, style: uiStyle(context, color: warn)),
              ),
          ],
        ),
      );
    },
  );
}

class _PairDialog extends StatefulWidget {
  const _PairDialog({required this.name, required this.initial});
  final String name, initial;
  @override
  State<_PairDialog> createState() => _PairDialogState();
}

class _PairDialogState extends State<_PairDialog> {
  late final text = TextEditingController(text: widget.initial);
  @override
  void dispose() {
    text.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text('Pair with ${widget.name}'),
    content: TextField(
      key: const Key('tv-pair-code'),
      controller: text,
      autofocus: true,
      maxLength: 6,
      keyboardType: TextInputType.number,
      inputFormatters: [FilteringTextInputFormatter.digitsOnly],
      decoration: const InputDecoration(labelText: 'Code shown on TV'),
      onChanged: (_) => setState(() {}),
      onSubmitted: (v) {
        if (v.length == 6) Navigator.pop(context, v);
      },
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
      TextButton(
        onPressed: text.text.length == 6
            ? () => Navigator.pop(context, text.text)
            : null,
        child: const Text('Pair'),
      ),
    ],
  );
}
