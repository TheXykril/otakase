import 'package:flutter/material.dart';

import '../data/community_extras.dart';
import '../services.dart';
import 'community_extras_page.dart';
import 'components.dart';

/// Takes a snapshot when the viewer opens the report. No stream URLs or
/// authorization headers are collected.
class PlaybackReportButton extends StatelessWidget {
  const PlaybackReportButton({
    super.key,
    required this.services,
    required this.isTV,
    required this.title,
    required this.episode,
    required this.problem,
    required this.position,
    this.provider = '',
    this.variant,
  });

  /// How loud the button is; the player's error screen keeps it quiet.
  final ButtonVariant? variant;
  final AppServices services;
  final bool isTV;
  final String title, episode, problem, provider;
  final Duration position;

  @override
  Widget build(BuildContext context) => AppButton(
    label: 'Report playback problem',
    variant: variant,
    isTV: isTV,
    onPressed: () => Navigator.push(
      context,
      OtakaseRoute<void>(
        builder: (_) => ReportPage(
          services: services,
          isTV: isTV,
          initialTitle: 'Playback problem: $title',
          initialBody: [title, if (episode.isNotEmpty) episode].join(' · '),
          playbackDetails: playbackReportDetails(
            provider: provider,
            position: position,
            problem: problem,
          ),
        ),
      ),
    ),
  );
}
