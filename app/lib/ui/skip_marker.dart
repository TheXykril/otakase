import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../data/skip_times.dart';
import 'components.dart';

String skipKindName(String type) => switch (type) {
  'op' => 'Opening',
  'ed' => 'Ending',
  _ => 'Recap',
};

String clock(double seconds) {
  final s = math.max(0, seconds).round();
  final h = s ~/ 3600, m = s % 3600 ~/ 60, sec = s % 60;
  final mm = h > 0 ? m.toString().padLeft(2, '0') : '$m';
  return '${h > 0 ? '$h:' : ''}$mm:${sec.toString().padLeft(2, '0')}';
}

/// What the viewer picked in the skip times panel.
sealed class SkipPanelChoice {}

class MarkSpan extends SkipPanelChoice {
  MarkSpan(this.type);
  final String type;
}

class WatchSpan extends SkipPanelChoice {
  WatchSpan(this.span);
  final SkipSpan span;
}

/// The episode's skip times, where they came from, a vote on each AniSkip
/// one, and a way to mark missing or wrong ones.
Future<SkipPanelChoice?> showSkipTimesPanel(
  BuildContext context, {
  required List<SkipSpan> spans,
  required SkipTimes skipTimes,
  required bool canSubmit,
  required bool isTV,
}) => showDialog<SkipPanelChoice>(
  context: context,
  builder: (dialog) => _SkipTimesPanel(
    spans: spans,
    skipTimes: skipTimes,
    canSubmit: canSubmit,
    isTV: isTV,
  ),
);

class _SkipTimesPanel extends StatefulWidget {
  const _SkipTimesPanel({
    required this.spans,
    required this.skipTimes,
    required this.canSubmit,
    required this.isTV,
  });
  final List<SkipSpan> spans;
  final SkipTimes skipTimes;
  final bool canSubmit, isTV;
  @override
  State<_SkipTimesPanel> createState() => _SkipTimesPanelState();
}

class _SkipTimesPanelState extends State<_SkipTimesPanel> {
  final thanked = <String>{};
  String error = '';

  Future<void> vote(SkipSpan span, bool up) async {
    try {
      await widget.skipTimes.vote(span, up: up);
      if (mounted) setState(() => thanked.add(span.id));
    } catch (_) {
      if (mounted) setState(() => error = 'AniSkip could not be reached.');
    }
  }

  @override
  Widget build(BuildContext context) {
    final kinds = widget.spans.map((s) => s.type).toSet();
    return AlertDialog(
      backgroundColor: panel,
      title: const Text('Skip times'),
      content: SizedBox(
        width: 560,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (widget.spans.isEmpty)
                Text(
                  'Nothing is on file for this episode yet.',
                  style: uiStyle(context, color: muted),
                ),
              for (final span in widget.spans) ...[
                ChoiceRow(
                  autofocus: span == widget.spans.first,
                  title: Text(
                    '${skipKindName(span.type)}  ${clock(span.start)} – ${clock(span.end)}',
                  ),
                  trailing: Text(
                    SkipTimes.sourceNames[span.source] ?? 'This source',
                    style: uiStyle(context, size: 14, color: muted),
                  ),
                  onTap: () => Navigator.pop(context, WatchSpan(span)),
                ),
                if (span.id.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(left: 16, bottom: 8),
                    child:
                        thanked.contains(span.id) ||
                            widget.skipTimes.voted(span)
                        ? Text(
                            'Thanks, your vote is in.',
                            style: uiStyle(context, size: 14, color: ok),
                          )
                        : Wrap(
                            spacing: 8,
                            runSpacing: 8,
                            crossAxisAlignment: WrapCrossAlignment.center,
                            children: [
                              Text(
                                'Right times?',
                                style: uiStyle(context, size: 14, color: muted),
                              ),
                              AppButton(
                                label: 'Yes',
                                icon: LucideIcons.thumbsUp,
                                isTV: widget.isTV,
                                onPressed: () => unawaited(vote(span, true)),
                              ),
                              AppButton(
                                label: 'No',
                                icon: LucideIcons.thumbsDown,
                                isTV: widget.isTV,
                                onPressed: () => unawaited(vote(span, false)),
                              ),
                            ],
                          ),
                  ),
              ],
              const SizedBox(height: 12),
              const Divider(),
              const SizedBox(height: 8),
              if (widget.canSubmit) ...[
                Text(
                  'Missing or wrong? Mark it while you watch and it goes to '
                  'AniSkip, for everyone.',
                  style: uiStyle(context, size: 14, color: muted),
                ),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final type in ['op', 'ed', 'recap'])
                      AppButton(
                        label:
                            '${kinds.contains(type) ? 'Fix' : 'Mark'} ${skipKindName(type).toLowerCase()}',
                        autofocus: widget.spans.isEmpty && type == 'op',
                        isTV: widget.isTV,
                        onPressed: () => Navigator.pop(context, MarkSpan(type)),
                      ),
                  ],
                ),
              ] else
                Text(
                  'This show has no MyAnimeList id, which AniSkip files '
                  'times under, so times cannot be sent for it.',
                  style: uiStyle(context, size: 14, color: muted),
                ),
              if (error.isNotEmpty) ...[
                const SizedBox(height: 12),
                Text(error, style: uiStyle(context, size: 14, color: warn)),
              ],
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Close'),
        ),
      ],
    );
  }
}

/// Typical lengths, for the end a new mark starts with.
const typicalSpan = {'op': 90.0, 'ed': 90.0, 'recap': 60.0};

/// The card shown while the viewer marks a span over the playing video:
/// set the start and end from the current moment, nudge either by a
/// second, check both, then send.
class SkipMarkerCard extends StatefulWidget {
  const SkipMarkerCard({
    super.key,
    required this.type,
    required this.position,
    required this.duration,
    required this.seekTo,
    required this.send,
    required this.onClose,
    required this.isTV,
    this.existing,
  });
  final String type;
  final double Function() position;
  final double duration;
  final Future<void> Function(double seconds) seekTo;

  /// Sends the span; returns an error for the viewer, or null when sent.
  final Future<String?> Function(double start, double end) send;
  final VoidCallback onClose;
  final bool isTV;

  /// The span being fixed, when there is one.
  final SkipSpan? existing;
  @override
  State<SkipMarkerCard> createState() => _SkipMarkerCardState();
}

class _SkipMarkerCardState extends State<SkipMarkerCard> {
  late double start, end;
  bool sending = false;
  String note = '', error = '';

  @override
  void initState() {
    super.initState();
    final e = widget.existing;
    if (e != null) {
      start = e.start;
      end = e.end;
    } else {
      start = (widget.position() * 10).round() / 10;
      end = math.min(widget.duration, start + typicalSpan[widget.type]!);
    }
  }

  double clamp(double v) => v.clamp(0, math.max(0, widget.duration)).toDouble();

  void setStart(double v) => setState(() {
    start = clamp(v);
    if (end <= start) end = clamp(start + typicalSpan[widget.type]!);
    error = '';
  });

  void setEnd(double v) => setState(() {
    end = clamp(v);
    error = '';
  });

  Future<void> submit() async {
    setState(() {
      sending = true;
      error = '';
    });
    final problem = await widget.send(start, end);
    if (!mounted) return;
    setState(() {
      sending = false;
      if (problem == null) {
        note = 'Sent to AniSkip. Thank you!';
      } else {
        error = problem;
      }
    });
    if (problem == null) {
      await Future<void>.delayed(const Duration(seconds: 2));
      if (mounted) widget.onClose();
    }
  }

  Widget point(String label, double value, ValueChanged<double> change) => Row(
    children: [
      SizedBox(
        width: 56,
        child: Text(label, style: uiStyle(context, color: muted)),
      ),
      SizedBox(
        width: 76,
        child: Text(
          clock(value),
          style: mono.copyWith(fontSize: widget.isTV ? 20 : 16, color: fg),
        ),
      ),
      Expanded(
        child: Wrap(
          spacing: 6,
          runSpacing: 6,
          children: [
            AppButton(
              label: '−1s',
              isTV: widget.isTV,
              onPressed: () => change(value - 1),
            ),
            AppButton(
              label: '+1s',
              isTV: widget.isTV,
              onPressed: () => change(value + 1),
            ),
            AppButton(
              label: 'Now',
              icon: LucideIcons.mapPin,
              isTV: widget.isTV,
              onPressed: () => change(widget.position()),
            ),
          ],
        ),
      ),
    ],
  );

  @override
  Widget build(BuildContext context) {
    final name = skipKindName(widget.type).toLowerCase();
    return Container(
      width: widget.isTV ? 640 : 520,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: panel.withValues(alpha: .96),
        border: Border.all(color: line),
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(LucideIcons.flag, color: accentText, size: 18),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  'Marking the $name · ${clock(end - start)} long',
                  style: uiStyle(context, weight: FontWeight.w600),
                ),
              ),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            'Press Now where it starts and where it ends, or nudge by a second. '
            '${widget.type == 'recap' ? 'Recaps' : '${skipKindName(widget.type)}s'} '
            'usually run ${clock(typicalSpan[widget.type]!)}.',
            style: uiStyle(context, size: 14, color: muted),
          ),
          const SizedBox(height: 12),
          point('Start', start, setStart),
          const SizedBox(height: 8),
          point('End', end, setEnd),
          const SizedBox(height: 12),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              AppButton(
                label: 'Watch start',
                icon: LucideIcons.play,
                isTV: widget.isTV,
                autofocus: true,
                onPressed: () =>
                    unawaited(widget.seekTo(math.max(0, start - 2))),
              ),
              AppButton(
                label: 'Watch end',
                icon: LucideIcons.play,
                isTV: widget.isTV,
                onPressed: () => unawaited(widget.seekTo(math.max(0, end - 3))),
              ),
              AppButton(
                label: sending ? 'Sending' : 'Send',
                icon: LucideIcons.send,
                filled: true,
                isTV: widget.isTV,
                onPressed: sending || note.isNotEmpty ? null : submit,
              ),
              AppButton(
                label: 'Cancel',
                isTV: widget.isTV,
                onPressed: widget.onClose,
              ),
            ],
          ),
          if (note.isNotEmpty || error.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(
              note.isNotEmpty ? note : error,
              style: uiStyle(
                context,
                size: 14,
                color: note.isNotEmpty ? ok : warn,
              ),
            ),
          ],
        ],
      ),
    );
  }
}
