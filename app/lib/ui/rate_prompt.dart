import 'dart:async';

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../data/trakt.dart';
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
    final title = services.titleRatingDue;
    if (title != null) {
      services.titleRatingDue = null;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        final context = navigator?.context;
        if (context == null || !context.mounted) return;
        unawaited(askTitleRating(context, services, title, isTV));
      });
      return;
    }
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
    builder: (_) => RateDialog(
      name: anime.title,
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
    await services.store.setLocalStatus(anime, 'COMPLETED');
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

/// Asks for a score from 1 to 10 for a film or show and keeps it, on Trakt
/// too when signed in. [finished] words it for the end of playback.
Future<void> askTitleRating(
  BuildContext context,
  AppServices services,
  MediaTitle title,
  bool isTV, {
  bool finished = true,
}) async {
  final score = await showDialog<int>(
    context: context,
    builder: (_) => RateDialog(
      name: title.title,
      initial: services.movies.score(title),
      isTV: isTV,
      finished: finished,
    ),
  );
  if (score == null || score <= 0 || !context.mounted) return;
  try {
    await services.movies.rate(title, score);
  } catch (e) {
    if (context.mounted) {
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(
        SnackBar(content: Text('The score was not sent to Trakt: $e')),
      );
    }
  }
}

class RateDialog extends StatefulWidget {
  const RateDialog({
    super.key,
    required this.name,
    required this.initial,
    required this.isTV,
    this.finished = true,
  });
  final String name;
  final int initial;
  final bool isTV, finished;
  @override
  State<RateDialog> createState() => _RateDialogState();
}

class _RateDialogState extends State<RateDialog> {
  late int score = widget.initial;

  @override
  Widget build(BuildContext context) => AlertDialog(
    backgroundColor: panel,
    title: Text(
      widget.finished ? 'You finished ${widget.name}' : 'Rate ${widget.name}',
    ),
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
                  radius: KageRadius.full,
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
