import 'package:flutter/material.dart';

import '../services.dart';
import 'components.dart';

String traktSyncStatus(AppServices services) {
  if (services.movies.syncing) return 'Syncing with Trakt…';
  if (services.movies.lastError != null || services.trakt.lastError != null) {
    return 'Trakt needs attention · progress stays on this device';
  }
  if (services.movies.hasPendingResume) {
    return 'Some progress is only on this device';
  }
  if (services.movies.lastProgressSentAt != null) {
    return 'Last playback update sent to Trakt';
  }
  return 'Connected · no playback updates sent this session';
}

String accountSyncSummary(AppServices services) {
  final auth = services.auth;
  if (!auth.hasAccounts && !services.trakt.signedIn) {
    return 'Progress saved on this device';
  }
  if (auth.accounts.values.any((a) => a.expired)) {
    return 'Sign in again to sync progress';
  }
  if (auth.accounts.keys.any(services.tracking.updateFailed) ||
      (services.trakt.signedIn &&
          (services.movies.lastError != null ||
              services.trakt.lastError != null))) {
    return 'Sync needs attention · open Accounts';
  }
  if (services.tracking.sendingProgress ||
      services.movies.syncing ||
      services.listSync.running) {
    return 'Syncing… · progress stays on this device';
  }
  if (auth.accounts.keys.any(services.tracking.hasPendingCompletion)) {
    return 'Progress saved locally · waiting to sync';
  }
  if (services.trakt.signedIn && services.movies.hasPendingResume) {
    return 'Some progress is only on this device · open Accounts';
  }
  final sent = [
    for (final service in auth.accounts.keys)
      if (services.tracking.lastSentTo(service) != null) service,
    if (services.trakt.signedIn && services.movies.lastProgressSentAt != null)
      'Trakt',
  ];
  if (sent.isNotEmpty) return 'Last update sent to ${sent.join(' & ')}';
  return 'Progress saved locally · view sync status';
}

/// A live, non-interactive subtitle inside the existing Accounts card.
class AccountSyncStatus extends StatelessWidget {
  const AccountSyncStatus({super.key, required this.services});
  final AppServices services;
  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: Listenable.merge([
      services.auth,
      services.tracking,
      services.listSync,
      services.trakt,
      services.movies,
      services.store,
    ]),
    builder: (context, _) => Text(
      accountSyncSummary(services),
      style: uiStyle(context, size: 15, color: muted),
    ),
  );
}
