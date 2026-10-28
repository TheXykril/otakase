import 'dart:math';

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../data/tracking.dart';
import '../services.dart';
import 'components.dart';
import 'community_page.dart';

Future<void> editListStatus(
  BuildContext context,
  AppServices services,
  Anime anime,
  bool isTV,
) => showDialog<void>(
  context: context,
  builder: (_) => _StatusEditor(services: services, anime: anime, isTV: isTV),
);

class _StatusEditor extends StatefulWidget {
  const _StatusEditor({
    required this.services,
    required this.anime,
    required this.isTV,
  });
  final AppServices services;
  final Anime anime;
  final bool isTV;
  @override
  State<_StatusEditor> createState() => _StatusEditorState();
}

class _StatusEditorState extends State<_StatusEditor> {
  late int progress, score;
  String status = 'CURRENT';
  bool saving = false;
  String? error;
  @override
  void initState() {
    super.initState();
    final s = widget.services;
    progress = max(
      s.tracking.progress(widget.anime),
      s.store.entry(widget.anime)?.watched.fold<int>(0, max) ?? 0,
    );
    final e = s.tracking.mergedCached(widget.anime);
    score =
        ((e?.score ?? s.store.integer('LocalScore.${widget.anime.key}') * 10) /
                10)
            .round();
    status = anilistStatuses.containsKey(e?.status)
        ? e!.status
        : const {
                'watching': 'CURRENT',
                'plan_to_watch': 'PLANNING',
                'completed': 'COMPLETED',
                'on_hold': 'PAUSED',
                'dropped': 'DROPPED',
              }[e?.status] ??
              s.store.text('LocalStatus.${widget.anime.key}');
    if (!anilistStatuses.containsKey(status) || status == 'ALL') {
      status = 'CURRENT';
    }
  }

  Future<void> save() async {
    setState(() {
      saving = true;
      error = null;
    });
    try {
      final s = widget.services;
      if (s.auth.hasAccounts) {
        await s.tracking.update(
          widget.anime,
          status: status,
          progress: progress,
          score: score * 10.0,
        );
      }
      await s.store.setWatchedProgress(widget.anime, progress);
      await s.store.set('LocalStatus.${widget.anime.key}', status);
      await s.store.set('LocalScore.${widget.anime.key}', score);
      if (mounted && status == 'COMPLETED' && score > 0) {
        await offerRatingShare(
          context,
          s,
          widget.anime,
          score.toDouble(),
          widget.isTV,
        );
      }
      if (mounted) Navigator.pop(context);
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(
      widget.services.auth.hasAccounts ? 'List status' : 'Local progress',
    ),
    content: SizedBox(
      width: 480,
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              widget.services.auth.hasAccounts
                  ? 'Updates ${widget.services.auth.accounts.keys.join(' and ')}.'
                  : 'Saved on this device. Public lists are read only.',
              style: uiStyle(context, color: muted),
            ),
            const SizedBox(height: 16),
            AppButton(
              label: anilistStatuses[status]!,
              isTV: widget.isTV,
              onPressed: saving
                  ? null
                  : () async {
                      final statuses = anilistStatuses.entries.where(
                        (e) => e.key != 'ALL',
                      );
                      final chosen = await chooseValue(
                        context,
                        'List status',
                        statuses.map((e) => e.value).toList(),
                        selected: anilistStatuses[status],
                      );
                      if (chosen != null && mounted) {
                        setState(
                          () => status = statuses
                              .firstWhere((e) => e.value == chosen)
                              .key,
                        );
                      }
                    },
            ),
            const SizedBox(height: 12),
            Text(
              'Watched episodes',
              style: uiStyle(context, weight: FontWeight.w600),
            ),
            Row(
              children: [
                IconButton(
                  tooltip: 'Decrease progress',
                  onPressed: saving || progress <= 0
                      ? null
                      : () => setState(() => progress--),
                  icon: const Icon(LucideIcons.minus),
                ),
                Expanded(
                  child: Text(
                    '$progress${widget.anime.episodes > 0 ? ' / ${widget.anime.episodes}' : ''}',
                    textAlign: TextAlign.center,
                    style: uiStyle(context),
                  ),
                ),
                IconButton(
                  tooltip: 'Increase progress',
                  onPressed:
                      saving ||
                          widget.anime.episodes > 0 &&
                              progress >= widget.anime.episodes
                      ? null
                      : () => setState(() => progress++),
                  icon: const Icon(LucideIcons.plus),
                ),
              ],
            ),
            Text('Score', style: uiStyle(context, weight: FontWeight.w600)),
            Row(
              children: [
                IconButton(
                  tooltip: 'Decrease score',
                  onPressed: saving || score <= 0
                      ? null
                      : () => setState(() => score--),
                  icon: const Icon(LucideIcons.minus),
                ),
                Expanded(
                  child: Text(
                    score == 0 ? 'Unscored' : '$score / 10',
                    textAlign: TextAlign.center,
                    style: uiStyle(context),
                  ),
                ),
                IconButton(
                  tooltip: 'Increase score',
                  onPressed: saving || score >= 10
                      ? null
                      : () => setState(() => score++),
                  icon: const Icon(LucideIcons.plus),
                ),
              ],
            ),
            if (error != null)
              Text(error!, style: uiStyle(context, color: warn)),
          ],
        ),
      ),
    ),
    actions: [
      TextButton(
        autofocus: true,
        onPressed: saving ? null : () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
      TextButton(
        onPressed: saving ? null : save,
        child: Text(saving ? 'Saving' : 'Save'),
      ),
    ],
  );
}
