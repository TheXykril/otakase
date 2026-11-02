import 'dart:async';

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../services.dart';
import 'community_page.dart';
import 'components.dart';

/// Whether finishing [episode] finishes the show, so a score is asked for
/// when the player closes. A show still airing is not finished however many
/// episodes are out.
bool finishesShow(AppServices services, Anime anime, int episode) =>
    services.store.flag('ScoreOnCompletion') &&
    anime.episodes > 0 &&
    episode >= anime.episodes &&
    anime.status != 'RELEASING' &&
    anime.status != 'NOT_YET_RELEASED';

/// Asks for a score once a player that finished a show closes. Registered
/// on the app's navigator: the player can't ask itself, it is gone by then.
class RatingPrompt extends NavigatorObserver {
  RatingPrompt(this.services, {required this.isTV});
  final AppServices services;
  final bool isTV;

  @override
  void didPop(Route<dynamic> route, Route<dynamic>? previousRoute) {
    // Dialogs inside the player pop too; only the page itself counts.
    if (route is! PageRoute) return;
    final anime = services.ratingDue;
    if (anime == null) return;
    services.ratingDue = null;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      final context = navigator?.context;
      if (context == null || !context.mounted) return;
      unawaited(askRating(context, services, anime, isTV));
    });
  }
}

Future<void> askRating(
  BuildContext context,
  AppServices services,
  Anime anime,
  bool isTV,
) async {
  final current =
      services.tracking.mergedCached(anime)?.score ??
      services.store.integer('LocalScore.${anime.key}') * 10.0;
  final score = await showDialog<int>(
    context: context,
    builder: (_) => _RateDialog(
      anime: anime,
      initial: (current / 10).round().clamp(0, 10),
      isTV: isTV,
    ),
  );
  if (score == null || score <= 0 || !context.mounted) return;
  try {
    if (services.auth.hasAccounts) {
      await services.tracking.update(
        anime,
        status: 'COMPLETED',
        score: score * 10.0,
      );
    }
    await services.store.set('LocalScore.${anime.key}', score);
    await services.store.set('LocalStatus.${anime.key}', 'COMPLETED');
    if (context.mounted) {
      await offerRatingShare(context, services, anime, score.toDouble(), isTV);
    }
  } catch (e) {
    if (context.mounted) {
      ScaffoldMessenger.maybeOf(
        context,
      )?.showSnackBar(SnackBar(content: Text('The score was not saved: $e')));
    }
  }
}

class _RateDialog extends StatefulWidget {
  const _RateDialog({
    required this.anime,
    required this.initial,
    required this.isTV,
  });
  final Anime anime;
  final int initial;
  final bool isTV;
  @override
  State<_RateDialog> createState() => _RateDialogState();
}

class _RateDialogState extends State<_RateDialog> {
  late int score = widget.initial;

  @override
  Widget build(BuildContext context) => AlertDialog(
    backgroundColor: panel,
    title: Text('You finished ${widget.anime.title}'),
    content: SizedBox(
      width: 520,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('How was it?', style: uiStyle(context, color: muted)),
          const SizedBox(height: 16),
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: [
              for (var i = 1; i <= 10; i++)
                FocusBox(
                  isTV: widget.isTV,
                  autofocus: i == (score == 0 ? 7 : score),
                  radius: 999,
                  onPressed: () => setState(() => score = i),
                  child: Container(
                    width: widget.isTV ? 52 : 40,
                    height: widget.isTV ? 52 : 40,
                    alignment: Alignment.center,
                    decoration: BoxDecoration(
                      color: i <= score ? shu : panelRaised,
                      shape: BoxShape.circle,
                      border: Border.all(color: i <= score ? shu : line),
                    ),
                    child: Text(
                      '$i',
                      style: uiStyle(
                        context,
                        weight: FontWeight.w600,
                        color: i <= score ? bgDeep : fg,
                      ),
                    ),
                  ),
                ),
            ],
          ),
          const SizedBox(height: 12),
          Text(
            score == 0 ? 'Pick a score' : '$score / 10',
            style: uiStyle(context, weight: FontWeight.w600),
          ),
        ],
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Not now'),
      ),
      TextButton(
        onPressed: score == 0 ? null : () => Navigator.pop(context, score),
        child: const Text('Save score'),
      ),
    ],
  );
}
