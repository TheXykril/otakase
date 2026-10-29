import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../data/profiles.dart';
import 'components.dart';
import 'seal.dart';
import 'device_status.dart';

class ProfileAvatar extends StatelessWidget {
  const ProfileAvatar(this.profile, {super.key, this.size = 64});
  final Profile profile;
  final double size;
  @override
  Widget build(BuildContext context) => profile.avatar.isNotEmpty
      ? ClipOval(
          child: CoverImage(profile.avatar, width: size, height: size),
        )
      : SizedBox.square(
          dimension: size,
          child: Center(
            child: ColorFiltered(
              colorFilter: ColorFilter.mode(
                Color(profile.color),
                BlendMode.srcIn,
              ),
              child: OtakaseSeal(size: size * .8),
            ),
          ),
        );
}

Future<ProfileUnlock?> requestProfileUnlock(
  BuildContext context,
  ProfileManager manager,
  String id,
) => showDialog<ProfileUnlock>(
  context: context,
  builder: (_) => _DirectionDialog(manager: manager, profileId: id),
);
Future<String?> requestDirectionCode(
  BuildContext context,
  ProfileManager manager,
) => showDialog<String>(
  context: context,
  builder: (_) => _DirectionDialog(manager: manager),
);

class ProfilesChooser extends StatefulWidget {
  const ProfilesChooser({
    super.key,
    required this.manager,
    this.canGoBack = false,
  });
  final ProfileManager manager;
  final bool canGoBack;
  @override
  State<ProfilesChooser> createState() => _ProfilesChooserState();
}

class _ProfilesChooserState extends State<ProfilesChooser> {
  String? error;
  Future<void> choose(Profile p) async {
    if (widget.manager.busy) return;
    try {
      ProfileUnlock? unlock;
      if (p.locked &&
          !(widget.manager.activeId == p.id &&
              widget.manager.session != null)) {
        unlock = await requestProfileUnlock(context, widget.manager, p.id);
        if (unlock == null) return;
      }
      await widget.manager.switchTo(p.id, unlock: unlock);
      if (mounted && widget.canGoBack) Navigator.pop(context);
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    }
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: widget.manager,
    builder: (context, _) => Scaffold(
      appBar: AppBar(
        title: const Text("Who's watching?"),
        automaticallyImplyLeading: widget.canGoBack,
        actions: [
          DeviceStatus(isTV: widget.manager.isTV),
          const SizedBox(width: 24),
        ],
      ),
      body: SafeArea(
        child: Padding(
          padding: EdgeInsets.all(widget.manager.isTV ? 48 : 20),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (error != null || widget.manager.error != null)
                Text(
                  error ?? widget.manager.error!,
                  style: uiStyle(context, color: warn),
                ),
              Expanded(
                child: LayoutBuilder(
                  builder: (context, box) => GridView(
                    gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                      crossAxisCount: widget.manager.isTV
                          ? 3
                          : box.maxWidth < 340
                          ? 1
                          : 2,
                      mainAxisSpacing: 24,
                      crossAxisSpacing: 24,
                      mainAxisExtent:
                          (widget.manager.isTV ? 244 : 210) *
                          math.max(
                            1,
                            MediaQuery.textScalerOf(context).scale(1),
                          ),
                    ),
                    children: widget.manager.profiles
                        .map(
                          (p) => FocusBox(
                            key: ValueKey('profile-${p.id}'),
                            isTV: widget.manager.isTV,
                            autofocus: p.id == widget.manager.activeId,
                            onPressed: widget.manager.busy
                                ? null
                                : () => choose(p),
                            child: Container(
                              padding: const EdgeInsets.all(16),
                              decoration: BoxDecoration(
                                color: panel,
                                border: Border.all(color: line),
                                borderRadius: BorderRadius.circular(14),
                              ),
                              child: Column(
                                mainAxisAlignment: MainAxisAlignment.center,
                                children: [
                                  ProfileAvatar(
                                    p,
                                    size: widget.manager.isTV ? 84 : 64,
                                  ),
                                  const SizedBox(height: 12),
                                  Text(
                                    p.name,
                                    maxLines: 2,
                                    overflow: TextOverflow.ellipsis,
                                    style: uiStyle(
                                      context,
                                      size: widget.manager.isTV ? 24 : 18,
                                      weight: FontWeight.w600,
                                    ),
                                  ),
                                  if (p.locked)
                                    Row(
                                      mainAxisSize: MainAxisSize.min,
                                      children: [
                                        const Icon(
                                          LucideIcons.lockKeyhole,
                                          size: 18,
                                        ),
                                        const SizedBox(width: 6),
                                        Text(
                                          'Locked',
                                          style: uiStyle(
                                            context,
                                            size: 15,
                                            color: muted,
                                          ),
                                        ),
                                      ],
                                    ),
                                ],
                              ),
                            ),
                          ),
                        )
                        .toList(),
                  ),
                ),
              ),
              if (widget.manager.busy)
                const LinearProgressIndicator(color: shu),
            ],
          ),
        ),
      ),
    ),
  );
}

class ProfilesPage extends StatefulWidget {
  const ProfilesPage({super.key, required this.manager});
  final ProfileManager manager;
  @override
  State<ProfilesPage> createState() => _ProfilesPageState();
}

class _ProfilesPageState extends State<ProfilesPage> {
  String? error;
  Future<void> action(Future<void> Function() run) async {
    try {
      await run();
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    }
  }

  Future<ProfileUnlock?> authorize(Profile p) async =>
      p.locked ? requestProfileUnlock(context, widget.manager, p.id) : null;
  Future<void> edit(Profile? profile) async {
    ProfileUnlock? unlock;
    if (profile?.locked == true) {
      unlock = await authorize(profile!);
      if (unlock == null || !mounted) return;
    }
    if (!mounted) return;
    final value = await showDialog<({String name, int color})>(
      context: context,
      builder: (_) => _EditProfileDialog(
        profile: profile,
        index: widget.manager.profiles.length,
        isTV: widget.manager.isTV,
      ),
    );
    if (value == null) return;
    if (profile == null) {
      await widget.manager.add(value.name, color: value.color);
    } else {
      await widget.manager.edit(
        profile.id,
        name: value.name,
        color: value.color,
        unlock: unlock,
      );
    }
  }

  Future<void> setLock(Profile p) async {
    final unlock = await authorize(p);
    if (p.locked && unlock == null || !mounted) return;
    final code = await requestDirectionCode(context, widget.manager);
    if (code != null) await widget.manager.setLock(p.id, code, unlock: unlock);
  }

  Future<void> removeLock(Profile p) async {
    final unlock = await authorize(p);
    if (unlock != null) await widget.manager.removeLock(p.id, unlock: unlock);
  }

  Future<void> remove(Profile p) async {
    final unlock = await authorize(p);
    if (p.locked && unlock == null || !mounted) return;
    final confirm = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('Delete ${p.name}?'),
        content: const Text(
          'This removes the profile’s accounts, history and settings from this device.',
        ),
        actions: [
          TextButton(
            autofocus: true,
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete profile'),
          ),
        ],
      ),
    );
    if (confirm == true) await widget.manager.delete(p.id, unlock: unlock);
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: widget.manager,
    builder: (context, _) => Scaffold(
      appBar: AppBar(
        title: const Text('Profiles'),
        leading: const BackButton(),
      ),
      body: ListView(
        padding: EdgeInsets.all(widget.manager.isTV ? 48 : 20),
        children: [
          if (error != null) Text(error!, style: uiStyle(context, color: warn)),
          AppButton(
            label: 'Switch profile',
            isTV: widget.manager.isTV,
            autofocus: true,
            onPressed: () => Navigator.push(
              context,
              OtakaseRoute<void>(
                builder: (_) =>
                    ProfilesChooser(manager: widget.manager, canGoBack: true),
              ),
            ),
          ),
          SwitchListTile(
            title: const Text('Ask on start'),
            subtitle: const Text(
              'Show the profile picker when more than one profile exists.',
            ),
            value: widget.manager.askOnStart,
            onChanged: widget.manager.setAskOnStart,
          ),
          for (final p in widget.manager.profiles)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      ProfileAvatar(p, size: 48),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Text(
                          '${p.name}${p.id == widget.manager.activeId ? ' · Current' : ''}',
                          style: uiStyle(
                            context,
                            size: 21,
                            weight: FontWeight.w600,
                          ),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      AppButton(
                        key: ValueKey('edit-profile-${p.id}'),
                        label: 'Edit',
                        isTV: widget.manager.isTV,
                        onPressed: () => action(() => edit(p)),
                      ),
                      AppButton(
                        key: ValueKey('lock-profile-${p.id}'),
                        label: p.locked ? 'Change code' : 'Set code',
                        isTV: widget.manager.isTV,
                        onPressed: () => action(() => setLock(p)),
                      ),
                      if (p.locked)
                        AppButton(
                          label: 'Remove code',
                          isTV: widget.manager.isTV,
                          onPressed: () => action(() => removeLock(p)),
                        ),
                      if (widget.manager.profiles.length > 1)
                        AppButton(
                          key: ValueKey('delete-profile-${p.id}'),
                          label: 'Delete',
                          isTV: widget.manager.isTV,
                          onPressed: () => action(() => remove(p)),
                        ),
                    ],
                  ),
                  const Divider(height: 24),
                ],
              ),
            ),
          AppButton(
            label: 'Add profile',
            isTV: widget.manager.isTV,
            onPressed: widget.manager.profiles.length < 6
                ? () => action(() => edit(null))
                : null,
          ),
          Text(
            '${widget.manager.profiles.length} of 6 profiles',
            style: uiStyle(context, size: 15, color: muted),
          ),
        ],
      ),
    ),
  );
}

class _EditProfileDialog extends StatefulWidget {
  const _EditProfileDialog({
    this.profile,
    required this.index,
    required this.isTV,
  });
  final Profile? profile;
  final int index;
  final bool isTV;
  @override
  State<_EditProfileDialog> createState() => _EditProfileDialogState();
}

class _EditProfileDialogState extends State<_EditProfileDialog> {
  late final TextEditingController name;
  late int color;
  @override
  void initState() {
    super.initState();
    name = TextEditingController(
      text: widget.profile?.name ?? 'Profile ${widget.index + 1}',
    );
    color =
        widget.profile?.color ??
        profileColors[widget.index % profileColors.length];
  }

  @override
  void dispose() {
    name.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.profile == null ? 'Add profile' : 'Edit profile'),
    content: SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextField(
            controller: name,
            maxLength: 32,
            autofocus: !widget.isTV,
            decoration: const InputDecoration(labelText: 'Profile name'),
          ),
          const SizedBox(height: 12),
          Wrap(
            spacing: 8,
            children: [
              for (var i = 0; i < profileColors.length; i++)
                FocusBox(
                  isTV: widget.isTV,
                  onPressed: () => setState(() => color = profileColors[i]),
                  radius: 999,
                  child: Semantics(
                    button: true,
                    selected: color == profileColors[i],
                    label: 'Profile colour ${i + 1}',
                    child: Container(
                      width: 44,
                      height: 44,
                      decoration: BoxDecoration(
                        shape: BoxShape.circle,
                        color: Color(profileColors[i]),
                        border: Border.all(
                          color: color == profileColors[i]
                              ? fgBright
                              : Colors.transparent,
                          width: 3,
                        ),
                      ),
                      child: color == profileColors[i]
                          ? const Icon(LucideIcons.check, color: bgDeep)
                          : null,
                    ),
                  ),
                ),
            ],
          ),
        ],
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
      TextButton(
        autofocus: widget.isTV,
        onPressed: () {
          if (name.text.trim().isNotEmpty) {
            Navigator.pop(context, (name: name.text.trim(), color: color));
          }
        },
        child: const Text('Save'),
      ),
    ],
  );
}

class _DirectionDialog extends StatefulWidget {
  const _DirectionDialog({required this.manager, this.profileId});
  final ProfileManager manager;
  final String? profileId;
  @override
  State<_DirectionDialog> createState() => _DirectionDialogState();
}

class _DirectionDialogState extends State<_DirectionDialog>
    with SingleTickerProviderStateMixin {
  String code = '', first = '', error = '';
  bool busy = false;
  Offset swipe = Offset.zero;
  late final AnimationController shake;
  Timer? timer;
  int get wait => widget.profileId == null
      ? 0
      : widget.manager.waitSeconds(widget.profileId!);
  @override
  void initState() {
    super.initState();
    shake = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 280),
    );
    timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) setState(() {});
    });
  }

  @override
  void dispose() {
    timer?.cancel();
    shake.dispose();
    super.dispose();
  }

  void append(String value) {
    if (!busy && wait == 0 && code.length < 8) setState(() => code += value);
  }

  Future<void> submit() async {
    if (busy || wait > 0 || !validDirectionCode(code)) return;
    if (widget.profileId == null) {
      if (first.isEmpty) {
        setState(() {
          first = code;
          code = '';
          error = '';
        });
        return;
      }
      if (first == code) {
        Navigator.pop(context, code);
        return;
      }
      setState(() {
        first = '';
        code = '';
        error = 'Codes did not match. Start again.';
      });
    } else {
      setState(() => busy = true);
      final result = await widget.manager.unlock(widget.profileId!, code);
      if (!mounted) return;
      if (result != null) {
        Navigator.pop(context, result);
        return;
      }
      setState(() {
        busy = false;
        code = '';
        error = wait > 0 ? 'Wait $wait seconds.' : 'Wrong code. Try again.';
      });
    }
    if (!MediaQuery.disableAnimationsOf(context)) {
      unawaited(shake.forward(from: 0));
    }
  }

  KeyEventResult key(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent) return KeyEventResult.ignored;
    final value = {
      LogicalKeyboardKey.arrowUp: 'U',
      LogicalKeyboardKey.arrowDown: 'D',
      LogicalKeyboardKey.arrowLeft: 'L',
      LogicalKeyboardKey.arrowRight: 'R',
    }[event.logicalKey];
    if (value != null) {
      append(value);
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.backspace) {
      if (code.isNotEmpty) {
        setState(() => code = code.substring(0, code.length - 1));
      }
      return KeyEventResult.handled;
    }
    if ([
      LogicalKeyboardKey.select,
      LogicalKeyboardKey.enter,
    ].contains(event.logicalKey)) {
      unawaited(submit());
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  @override
  Widget build(BuildContext context) => Focus(
    autofocus: true,
    onKeyEvent: key,
    child: AnimatedBuilder(
      animation: shake,
      builder: (context, child) => Transform.translate(
        offset: Offset(
          math.sin(shake.value * math.pi * 6) * 7 * (1 - shake.value),
          0,
        ),
        child: child,
      ),
      child: AlertDialog(
        title: Text(
          widget.profileId != null
              ? 'Unlock ${widget.manager.profile(widget.profileId!).name}'
              : first.isEmpty
              ? 'Set direction code'
              : 'Enter the code again',
        ),
        content: GestureDetector(
          behavior: HitTestBehavior.opaque,
          onPanStart: (_) => swipe = Offset.zero,
          onPanUpdate: (d) => swipe += d.delta,
          onPanEnd: (_) {
            if (swipe.distance > 28) {
              append(
                swipe.dx.abs() > swipe.dy.abs()
                    ? swipe.dx > 0
                          ? 'R'
                          : 'L'
                    : swipe.dy > 0
                    ? 'D'
                    : 'U',
              );
            }
          },
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(
                wait > 0
                    ? 'Wait $wait seconds.'
                    : 'Use 4–8 directions, then select Continue.',
                style: uiStyle(context),
              ),
              const SizedBox(height: 20),
              Semantics(
                label: '${code.length} directions entered',
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    for (var i = 0; i < 8; i++)
                      Padding(
                        padding: const EdgeInsets.all(5),
                        child: Icon(
                          i < code.length
                              ? LucideIcons.circleDot
                              : LucideIcons.circle,
                          size: 14,
                          color: i < code.length ? fgBright : lineStrong,
                        ),
                      ),
                  ],
                ),
              ),
              const SizedBox(height: 16),
              Wrap(
                spacing: 8,
                children: [
                  for (final e in {
                    'U': '↑',
                    'D': '↓',
                    'L': '←',
                    'R': '→',
                  }.entries)
                    TextButton(
                      key: ValueKey('code-${e.key}'),
                      onPressed: wait > 0 || busy ? null : () => append(e.key),
                      child: Text(e.value, style: uiStyle(context, size: 28)),
                    ),
                ],
              ),
              if (error.isNotEmpty)
                Text(error, style: uiStyle(context, color: warn)),
              if (busy) const LinearProgressIndicator(color: shu),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: code.isEmpty ? null : () => setState(() => code = ''),
            child: const Text('Clear'),
          ),
          TextButton(
            key: const Key('code-continue'),
            onPressed: wait > 0 || busy || code.length < 4 ? null : submit,
            child: const Text('Continue'),
          ),
        ],
      ),
    ),
  );
}
