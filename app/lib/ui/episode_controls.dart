import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'components.dart';

/// A local visual cover only: watched state and playback remain unchanged.
class SpoilerArt extends StatelessWidget {
  const SpoilerArt({
    super.key,
    required this.hidden,
    required this.child,
    this.iconAlignment = Alignment.center,
  });
  final bool hidden;
  final Widget child;
  final Alignment iconAlignment;

  @override
  Widget build(BuildContext context) {
    if (!hidden) return child;
    // The spoiler blur is real (it hides the picture), so it gets its own
    // layer and is not redrawn with the rest of the page.
    return RepaintBoundary(
      child: Stack(
        fit: StackFit.expand,
        children: [
          ExcludeSemantics(
            child: ImageFiltered(
              imageFilter: ui.ImageFilter.blur(sigmaX: 22, sigmaY: 22),
              child: child,
            ),
          ),
          ColoredBox(color: bgDeep.withValues(alpha: .55)),
          Align(
            alignment: iconAlignment,
            child: const Padding(
              padding: EdgeInsets.all(10),
              child: Icon(LucideIcons.eyeOff, color: fg, size: 32),
            ),
          ),
        ],
      ),
    );
  }
}

Future<int?> chooseEpisode(
  BuildContext context, {
  required int total,
  required int current,
  required bool isTV,
}) => showDialog<int>(
  context: context,
  builder: (_) => _EpisodeJump(total: total, current: current, isTV: isTV),
);

class _EpisodeJump extends StatefulWidget {
  const _EpisodeJump({
    required this.total,
    required this.current,
    required this.isTV,
  });
  final int total, current;
  final bool isTV;

  @override
  State<_EpisodeJump> createState() => _EpisodeJumpState();
}

class _EpisodeJumpState extends State<_EpisodeJump> {
  final input = TextEditingController();
  final confirmFocus = FocusNode(debugLabel: 'Confirm episode jump');

  @override
  void dispose() {
    input.dispose();
    confirmFocus.dispose();
    super.dispose();
  }

  int? get selected {
    final value = int.tryParse(input.text);
    return value != null && value >= 1 && value <= widget.total ? value : null;
  }

  void edit(String value) => setState(() => input.text = value);
  void digit(String value) {
    if (input.text.length < widget.total.toString().length) {
      edit('${input.text}$value');
    }
  }

  void delete() {
    if (input.text.isNotEmpty) {
      edit(input.text.substring(0, input.text.length - 1));
    }
  }

  KeyEventResult key(FocusNode node, KeyEvent event) {
    final logical = event.logicalKey;
    int? number;
    final code = logical.keyId;
    if (code >= LogicalKeyboardKey.digit0.keyId &&
        code <= LogicalKeyboardKey.digit9.keyId) {
      number = code - LogicalKeyboardKey.digit0.keyId;
    } else if (code >= LogicalKeyboardKey.numpad0.keyId &&
        code <= LogicalKeyboardKey.numpad9.keyId) {
      number = code - LogicalKeyboardKey.numpad0.keyId;
    }
    if (number != null) {
      // Handle both halves so unhandled digits cannot fall back to native TV
      // channel entry. Firmware-reserved keys may never reach the app.
      if (event is KeyDownEvent) {
        digit('$number');
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted && selected != null) confirmFocus.requestFocus();
        });
      }
      return KeyEventResult.handled;
    }
    if (logical == LogicalKeyboardKey.backspace ||
        logical == LogicalKeyboardKey.delete) {
      if (event is KeyDownEvent || event is KeyRepeatEvent) delete();
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  @override
  Widget build(BuildContext context) => Focus(
    onKeyEvent: key,
    child: AlertDialog(
      title: const Text('Go to episode'),
      content: SizedBox(
        width: 440,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                widget.isTV
                    ? 'Use the remote number buttons or the keypad.'
                    : 'Use the number keys or the keypad.',
                style: uiStyle(context, color: muted),
              ),
              const SizedBox(height: 16),
              // No text focus/IME: physical numbers go through the dialog's
              // Focus ancestor before Flutter can forward them to the host.
              TextField(
                key: const Key('episode-jump-input'),
                controller: input,
                readOnly: true,
                canRequestFocus: false,
                enableInteractiveSelection: false,
                decoration: InputDecoration(
                  labelText: 'Episode number',
                  helperText: '1–${widget.total}',
                  errorText: input.text.isNotEmpty && selected == null
                      ? 'Choose an episode from 1 to ${widget.total}.'
                      : null,
                ),
              ),
              const SizedBox(height: 16),
              for (final row in [
                ['1', '2', '3'],
                ['4', '5', '6'],
                ['7', '8', '9'],
                ['Clear', '0', 'Delete'],
              ])
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: Row(
                    children: [
                      for (var i = 0; i < row.length; i++) ...[
                        if (i > 0) const SizedBox(width: 8),
                        Expanded(
                          child: AppButton(
                            key: ValueKey('episode-jump-${row[i]}'),
                            label: row[i],
                            isTV: widget.isTV,
                            autofocus: row[i] == '1',
                            onPressed: () {
                              if (row[i] == 'Clear') {
                                edit('');
                              } else if (row[i] == 'Delete') {
                                delete();
                              } else {
                                digit(row[i]);
                              }
                            },
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  AppButton(
                    label: 'Current · ${widget.current}',
                    isTV: widget.isTV,
                    onPressed: () => Navigator.pop(context, widget.current),
                  ),
                  AppButton(
                    label: 'Latest · ${widget.total}',
                    isTV: widget.isTV,
                    onPressed: () => Navigator.pop(context, widget.total),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
      actions: [
        AppButton(
          label: 'Cancel',
          isTV: widget.isTV,
          onPressed: () => Navigator.pop(context),
        ),
        AppButton(
          key: const Key('episode-jump-confirm'),
          focusNode: confirmFocus,
          label: 'Go to episode',
          icon: LucideIcons.arrowRight,
          filled: true,
          isTV: widget.isTV,
          onPressed: selected == null
              ? null
              : () => Navigator.pop(context, selected),
        ),
      ],
    ),
  );
}

class EpisodeInfo extends StatelessWidget {
  const EpisodeInfo({
    super.key,
    required this.episode,
    required this.title,
    required this.airdate,
    required this.duration,
    required this.position,
    required this.seen,
    required this.hidden,
    required this.isTV,
    required this.artwork,
    required this.onPlay,
    required this.onReveal,
    this.description = '',
    this.heading = '',
  });
  final int episode, duration, position;
  final String title, airdate, description;

  /// The bar's title, such as S2E5; empty says Episode N.
  final String heading;
  final bool seen, hidden, isTV;
  final Widget artwork;
  final VoidCallback onPlay, onReveal;

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: Text(heading.isNotEmpty ? heading : 'Episode $episode'),
    ),
    body: ListView(
      padding: const EdgeInsets.all(24),
      children: [
        AspectRatio(
          aspectRatio: 16 / 9,
          child: ClipRRect(
            borderRadius: BorderRadius.circular(KageRadius.md),
            child: SpoilerArt(hidden: hidden, child: artwork),
          ),
        ),
        const SizedBox(height: 20),
        Text(
          hidden ? 'Episode $episode' : title,
          style: uiStyle(context, size: 26, weight: FontWeight.w700),
        ),
        const SizedBox(height: 12),
        Text(
          [
            if (airdate.isNotEmpty) airdate,
            if (duration > 0) '$duration min',
            if (seen)
              'Watched'
            else if (position > 0)
              'Resume at ${position ~/ 60}m ${position % 60}s'
            else
              'Not watched',
          ].join(' · '),
          style: uiStyle(context, color: muted),
        ),
        if (!hidden && description.isNotEmpty) ...[
          const SizedBox(height: 16),
          Text(description, style: uiStyle(context)),
        ],
        const SizedBox(height: 24),
        if (hidden) ...[
          Text(
            'Episode artwork and title are hidden by your spoiler setting.',
            style: uiStyle(context, color: muted),
          ),
          const SizedBox(height: 16),
          AppButton(
            label: 'Reveal this episode',
            icon: LucideIcons.eye,
            isTV: isTV,
            autofocus: true,
            onPressed: onReveal,
          ),
          const SizedBox(height: 12),
        ],
        AppButton(
          label: position > 0
              ? 'Resume episode $episode'
              : 'Play episode $episode',
          icon: LucideIcons.play,
          filled: true,
          isTV: isTV,
          autofocus: !hidden,
          onPressed: onPlay,
        ),
      ],
    ),
  );
}
