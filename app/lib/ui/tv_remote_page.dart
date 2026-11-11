import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../cast/cast.dart';
import '../data/models.dart';
import '../data/tv_remote.dart';
import '../services.dart';
import 'components.dart';
import 'device_status.dart';
import '../host.dart';

class TVRemotePage extends StatefulWidget {
  const TVRemotePage({
    super.key,
    required this.services,
    required this.isTV,
    this.pairCode,
    this.findTVs = discoverTVs,
    this.firewall,
  });
  final AppServices services;
  final bool isTV;
  final RemotePairCode? pairCode;
  final Future<List<TVDevice>> Function() findTVs;

  /// Asks and opens this computer's firewall; the core's by default.
  final RemoteFirewall? firewall;
  @override
  State<TVRemotePage> createState() => _TVRemotePageState();
}

class _TVRemotePageState extends State<TVRemotePage> {
  TVRemote get remote => widget.services.tvRemote;
  List<TVDevice> devices = [];
  TVDevice? selected;
  RemoteLink? link;
  Json get status => link?.status ?? const {};
  Timer? timer;
  String? error, firewallNote;
  bool busy = false, canAllow = false, allowing = false;
  late final RemoteFirewall firewall =
      widget.firewall ?? CoreFirewall(CastClient(widget.services.core));
  @override
  void initState() {
    super.initState();
    if (widget.isTV) {
      unawaited(newCode());
      if (Host.linux || Host.windows) unawaited(checkFirewall());
      timer = Timer.periodic(const Duration(seconds: 1), (_) {
        if (mounted) setState(() {});
      });
    } else {
      unawaited(scan());
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

  Future<void> checkFirewall() async {
    final open = await firewall.canAllow();
    if (mounted) setState(() => canAllow = open);
  }

  /// Opens the remote's port behind the system's password prompt, so a
  /// phone can reach this computer without anyone typing firewall rules.
  Future<void> allowFirewall() async {
    setState(() {
      allowing = true;
      firewallNote = null;
    });
    final opened = await firewall.allow();
    if (!mounted) return;
    setState(() {
      allowing = false;
      firewallNote = opened
          ? 'Phones can reach this computer now. Pair again on the phone.'
          : 'The firewall was not opened.';
    });
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

  /// Opens the live connection to a paired [tv].
  void connect(TVDevice tv) {
    final old = link;
    if (old != null && old.tv.id == tv.id) return;
    unawaited(old?.close());
    final next = RemoteLink(remote, tv, findTVs: widget.findTVs);
    next.addListener(() {
      if (mounted) setState(() {});
    });
    setState(() {
      selected = tv;
      link = next;
      error = null;
    });
    next.start();
  }

  /// Taps on a TV that is not paired yet: newer TVs ask on screen, older
  /// ones show a code to type.
  Future<void> pair(TVDevice tv, {String initial = ''}) async {
    if (initial.isEmpty && tv.version >= 2) {
      final allowed = await showDialog<bool>(
        context: context,
        barrierDismissible: false,
        builder: (_) => _AskDialog(
          tv: tv,
          remote: remote,
          phoneName: widget.services.profiles?.active.name ?? 'Phone',
        ),
      );
      if (allowed == true && mounted) connect(tv);
      return;
    }
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
      if (mounted) connect(tv);
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    }
  }

  Future<void> enterCode() async {
    final entry = await showDialog<(String, String)>(
      context: context,
      builder: (_) => const _CodeDialog(),
    );
    if (entry == null || !mounted) return;
    setState(() {
      busy = true;
      error = null;
    });
    try {
      final tv = await remote.pairByCode(
        entry.$1,
        address: entry.$2,
        phoneName: widget.services.profiles?.active.name ?? 'Phone',
        findTVs: widget.findTVs,
      );
      if (mounted) {
        setState(() {
          devices = {
            for (final d in [...remote.paired, ...devices]) d.id: d,
          }.values.toList();
        });
        connect(tv);
      }
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> send(String action, {Json args = const {}}) async {
    final current = link;
    if (current == null) return;
    try {
      await current.send(action, args: args);
      if (mounted) setState(() => error = null);
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    }
  }

  @override
  void dispose() {
    timer?.cancel();
    unawaited(link?.close());
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
          leading: widget.isTV
              ? IconButton(
                  autofocus: true,
                  tooltip: MaterialLocalizations.of(context).backButtonTooltip,
                  icon: const BackButtonIcon(),
                  onPressed: () => Navigator.maybePop(context),
                )
              : null,
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
                if (code.device.host.isNotEmpty)
                  Text(
                    'Address ${code.device.port == tvDiscoveryPort ? code.device.host : '${code.device.host}:${code.device.port}'}',
                    key: const Key('tv-pair-address'),
                    textAlign: TextAlign.center,
                    style: mono.copyWith(fontSize: 22, color: muted),
                  ),
                const SizedBox(height: 8),
                Text(
                  'On the phone open More > Control TV and tap this TV, then pick Allow here. Or scan with the phone camera, or tap Enter code.\nCode expires in ${left ~/ 60} min ${left % 60} s.',
                  textAlign: TextAlign.center,
                  style: uiStyle(context),
                ),
                if ((Host.linux || Host.windows || Host.macOS) && !canAllow)
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: Text(
                      'Phone cannot find this computer? Allow port $tvDiscoveryPort (TCP and UDP) in its firewall, or type the address on the phone.',
                      textAlign: TextAlign.center,
                      style: uiStyle(context, color: muted),
                    ),
                  ),
                const SizedBox(height: 20),
              ],
              if (canAllow) ...[
                Text(
                  'Phone cannot reach this computer? Its firewall is on. Allow the phone remote through it once.',
                  textAlign: TextAlign.center,
                  style: uiStyle(context, color: muted),
                ),
                const SizedBox(height: 12),
                AppButton(
                  key: const Key('tv-remote-allow'),
                  label: allowing
                      ? 'Waiting for password…'
                      : 'Allow phone remote',
                  isTV: true,
                  onPressed: allowing ? null : allowFirewall,
                ),
                if (firewallNote != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 8),
                    child: Text(
                      firewallNote!,
                      textAlign: TextAlign.center,
                      style: uiStyle(context, color: muted),
                    ),
                  ),
                const SizedBox(height: 12),
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
              const SizedBox(height: 12),
              AppButton(
                key: const Key('tv-enter-code'),
                label: 'Enter code',
                isTV: false,
                onPressed: busy ? null : enterCode,
              ),
              const SizedBox(height: 16),
              if (!busy && devices.isEmpty)
                Text(
                  'Open More > Phone remote on the TV or computer. Scan its QR code with the phone camera, tap Enter code, or find TVs again.',
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
                        connect(tv);
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
                  switch (link?.state) {
                    LinkState.live || LinkState.polling => 'Connected',
                    LinkState.lost => 'Reconnecting…',
                    _ => 'Connecting…',
                  },
                  key: const Key('tv-link-state'),
                  style: uiStyle(
                    context,
                    color: link?.live == true ? ok : warn,
                  ),
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
                    unawaited(link?.close());
                    if (mounted) {
                      setState(() {
                        selected = null;
                        link = null;
                        devices = remote.paired;
                      });
                    }
                  },
                  child: const Text('Forget TV'),
                ),
              ],
            ],
            if (error ?? link?.error case final problem?)
              Padding(
                padding: const EdgeInsets.only(top: 16),
                child: Text(problem, style: uiStyle(context, color: warn)),
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
    content: RemoteTextInput(
      isTV: AppLayout.tvOf(context),
      builder: (focus) => TextField(
        focusNode: focus,
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

class _CodeDialog extends StatefulWidget {
  const _CodeDialog();
  @override
  State<_CodeDialog> createState() => _CodeDialogState();
}

class _CodeDialogState extends State<_CodeDialog> {
  final code = TextEditingController(), address = TextEditingController();
  @override
  void dispose() {
    code.dispose();
    address.dispose();
    super.dispose();
  }

  bool get ready =>
      code.text.length == 6 &&
      (address.text.trim().isEmpty || parseTVAddress(address.text) != null);

  void submit() {
    if (ready) Navigator.pop(context, (code.text, address.text.trim()));
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Enter the TV code'),
    content: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        TextField(
          key: const Key('tv-code'),
          controller: code,
          autofocus: true,
          maxLength: 6,
          keyboardType: TextInputType.number,
          inputFormatters: [FilteringTextInputFormatter.digitsOnly],
          decoration: const InputDecoration(labelText: 'Code shown on TV'),
          onChanged: (_) => setState(() {}),
          onSubmitted: (_) => submit(),
        ),
        TextField(
          key: const Key('tv-address'),
          controller: address,
          keyboardType: const TextInputType.numberWithOptions(decimal: true),
          inputFormatters: [
            FilteringTextInputFormatter.allow(RegExp(r'[0-9.:]')),
          ],
          decoration: InputDecoration(
            labelText: 'Address shown on TV (optional)',
            hintText: '192.168.1.20',
            errorText:
                address.text.trim().isEmpty ||
                    parseTVAddress(address.text) != null
                ? null
                : 'Use the numbers shown under the code',
          ),
          onChanged: (_) => setState(() {}),
          onSubmitted: (_) => submit(),
        ),
      ],
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
      TextButton(onPressed: ready ? submit : null, child: const Text('Pair')),
    ],
  );
}

/// Phone side of asking a TV: shows the digits the TV shows next to its
/// Allow button and closes with true once the TV allowed this phone.
class _AskDialog extends StatefulWidget {
  const _AskDialog({
    required this.tv,
    required this.remote,
    required this.phoneName,
  });
  final TVDevice tv;
  final TVRemote remote;
  final String phoneName;
  @override
  State<_AskDialog> createState() => _AskDialogState();
}

class _AskDialogState extends State<_AskDialog> {
  String digits = '';
  String? error;
  bool cancelled = false;

  @override
  void initState() {
    super.initState();
    unawaited(run());
  }

  Future<void> run() async {
    try {
      await widget.remote.requestControl(
        widget.tv,
        phoneName: widget.phoneName,
        digits: (d) {
          if (mounted) setState(() => digits = d);
        },
        cancelled: () => cancelled || !mounted,
      );
      if (mounted && !cancelled) Navigator.pop(context, true);
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    }
  }

  @override
  void dispose() {
    cancelled = true;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.tv.name),
    content: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          error ??
              (digits.isEmpty
                  ? 'Asking the TV…'
                  : 'Pick Allow on the TV. It shows these numbers too, so you know it is this phone.'),
          style: uiStyle(context, color: error == null ? fg : warn),
        ),
        if (error == null && digits.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(top: 16),
            child: Center(
              child: Text(
                digits,
                key: const Key('tv-ask-digits'),
                style: mono.copyWith(fontSize: 40, color: accentText),
              ),
            ),
          ),
      ],
    ),
    actions: [
      TextButton(
        onPressed: () {
          cancelled = true;
          Navigator.pop(context, false);
        },
        child: Text(error == null ? 'Cancel' : 'Close'),
      ),
    ],
  );
}

/// TV side: a phone asks to control this TV. Shown over whatever is open,
/// the player too, and closes on its own when the ask runs out.
class RemoteAskDialog extends StatefulWidget {
  const RemoteAskDialog({super.key, required this.remote, required this.ask});
  final TVRemote remote;
  final RemoteAsk ask;
  @override
  State<RemoteAskDialog> createState() => _RemoteAskDialogState();
}

class _RemoteAskDialogState extends State<RemoteAskDialog> {
  Timer? expiry;
  bool answered = false;

  @override
  void initState() {
    super.initState();
    widget.remote.addListener(changed);
    expiry = Timer(
      widget.ask.expires.difference(DateTime.now()),
      () => close(false),
    );
  }

  void changed() {
    if (widget.remote.ask?.id != widget.ask.id) close(false);
  }

  void close(bool allow) {
    if (answered || !mounted) return;
    answered = true;
    if (allow) {
      unawaited(widget.remote.allowAsk());
    } else if (widget.remote.ask?.id == widget.ask.id) {
      widget.remote.denyAsk();
    }
    Navigator.pop(context);
  }

  @override
  void dispose() {
    expiry?.cancel();
    widget.remote.removeListener(changed);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    icon: const Icon(LucideIcons.smartphone, color: shu),
    title: Text('${widget.ask.phone} wants to control this device'),
    content: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(
          'Check that the phone shows these numbers:',
          textAlign: TextAlign.center,
          style: uiStyle(context, color: muted),
        ),
        const SizedBox(height: 12),
        Text(
          widget.ask.digits,
          key: const Key('tv-ask-digits'),
          style: mono.copyWith(fontSize: 40, color: accentText),
        ),
      ],
    ),
    actions: [
      TextButton(
        key: const Key('tv-ask-deny'),
        onPressed: () => close(false),
        child: const Text('Not now'),
      ),
      TextButton(
        key: const Key('tv-ask-allow'),
        autofocus: true,
        onPressed: () => close(true),
        child: const Text('Allow'),
      ),
    ],
  );
}
