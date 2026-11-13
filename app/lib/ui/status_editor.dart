import 'dart:math';

import 'package:flutter/material.dart';

import '../core.dart';
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
      // Nothing watched yet means it is for later.
      status = progress > 0 ? 'CURRENT' : 'PLANNING';
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
      await s.store.setLocalStatus(widget.anime, status);
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
      if (mounted) setState(() => error = errorMessage(e));
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.services.auth.hasAccounts ? 'List status' : 'Your list'),
    content: SizedBox(
      width: widget.isTV ? 560 : 480,
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              widget.services.auth.hasAccounts
                  ? 'Updates ${widget.services.auth.accounts.keys.join(' and ')}.'
                  : 'Saved on this device. Sign in to AniList or MAL in More, '
                        'Your accounts, to keep it everywhere.',
              style: uiStyle(context, color: muted),
            ),
            const SizedBox(height: 16),
            // Statuses sit in the editor itself; no second dialog.
            GroupHeading(
              'Status',
              padding: const EdgeInsets.only(top: 16, bottom: 10),
            ),
            AppSegmented<String>(
              columns: widget.isTV ? 3 : 2,
              isTV: widget.isTV,
              options: [
                for (final e in anilistStatuses.entries)
                  if (e.key != 'ALL') (e.key, e.value),
              ],
              selected: status,
              keyOf: (v) => ValueKey('status-$v'),
              onChanged: (v) {
                if (!saving) setState(() => status = v);
              },
            ),
            GroupHeading(
              'Watched episodes',
              padding: const EdgeInsets.only(top: 20, bottom: 10),
            ),
            AppStepper(
              label: 'Progress',
              value: progress,
              max: widget.anime.episodes > 0 ? widget.anime.episodes : null,
              text:
                  '$progress${widget.anime.episodes > 0 ? ' / ${widget.anime.episodes}' : ''}',
              isTV: widget.isTV,
              enabled: !saving,
              onChanged: (v) => setState(() => progress = v),
            ),
            GroupHeading(
              'Score',
              padding: const EdgeInsets.only(top: 20, bottom: 10),
            ),
            AppStepper(
              label: 'Score',
              value: score,
              max: 10,
              text: score == 0 ? 'Unscored' : '$score / 10',
              isTV: widget.isTV,
              enabled: !saving,
              onChanged: (v) => setState(() => score = v),
            ),
            const SizedBox(height: 12),
            if (error != null)
              Text(error!, style: uiStyle(context, color: warn)),
          ],
        ),
      ),
    ),
    actions: [
      AppButton(
        label: 'Cancel',
        variant: ButtonVariant.text,
        autofocus: true,
        isTV: widget.isTV,
        onPressed: saving ? null : () => Navigator.pop(context),
      ),
      AppButton(
        key: const Key('status-save'),
        label: saving ? 'Saving' : 'Save',
        variant: ButtonVariant.primary,
        isTV: widget.isTV,
        onPressed: saving ? null : save,
      ),
    ],
  );
}
