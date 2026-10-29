import 'dart:async';

import 'package:flutter/material.dart';

import '../data/app_updates.dart';
import 'components.dart';

class AppUpdatePanel extends StatefulWidget {
  const AppUpdatePanel({
    super.key,
    required this.updates,
    required this.isTV,
    required this.autoCheck,
  });
  final AppUpdates updates;
  final bool isTV, autoCheck;
  @override
  State<AppUpdatePanel> createState() => _AppUpdatePanelState();
}

class _AppUpdatePanelState extends State<AppUpdatePanel>
    with WidgetsBindingObserver {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    if (widget.autoCheck && widget.updates.phase == UpdatePhase.idle) {
      unawaited(widget.updates.check());
    } else if (widget.updates.installed == null) {
      unawaited(widget.updates.loadInstalled());
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      unawaited(widget.updates.resume());
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: widget.updates,
    builder: (context, _) {
      final updates = widget.updates;
      final phase = updates.phase;
      final candidate = updates.update;
      final message = switch (phase) {
        UpdatePhase.idle => 'Check for a new Android app version.',
        UpdatePhase.checking => 'Checking for updates…',
        UpdatePhase.current => 'Your app is up to date.',
        UpdatePhase.unpublished => 'No in-app update has been published yet.',
        UpdatePhase.available => 'An Otakase update is available.',
        UpdatePhase.downloading => 'Downloading and verifying the update…',
        UpdatePhase.ready => 'Update verified. Ready to install.',
        UpdatePhase.permission => 'Allow Otakase to install updates in Android settings, then return here.',
        UpdatePhase.installer => 'Confirm the update in the Android installer.',
        UpdatePhase.failed => updates.error ?? 'Update failed. Please retry.',
      };
      return Padding(
        padding: const EdgeInsets.fromLTRB(24, 16, 24, 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Otakase', style: uiStyle(context, weight: FontWeight.w600)),
            if (updates.installed case final installed?)
              Text(
                'Installed: ${installed.label}',
                style: uiStyle(context, size: 15, color: muted),
              ),
            const SizedBox(height: 12),
            Text(message, style: uiStyle(context)),
            if (candidate != null && updates.available) ...[
              const SizedBox(height: 8),
              Text(
                '${candidate.label} · ${(candidate.size / 1024 / 1024).toStringAsFixed(1)} MB',
                style: uiStyle(context, size: 15, color: accentText),
              ),
            ],
            if (phase == UpdatePhase.downloading) ...[
              const SizedBox(height: 16),
              LinearProgressIndicator(
                value: updates.progress,
                color: shu,
                backgroundColor: line,
                semanticsLabel: 'Update download',
              ),
              const SizedBox(height: 8),
              Text(
                '${((updates.progress ?? 0) * 100).floor()}%',
                style: uiStyle(context, size: 15, color: muted),
              ),
            ],
            const SizedBox(height: 16),
            Wrap(
              spacing: 12,
              runSpacing: 12,
              children: [
                if (!updates.busy && updates.available)
                  OutlinedButton.icon(
                    key: const Key('install-update'),
                    autofocus: widget.isTV,
                    onPressed: updates.downloaded
                        ? updates.install
                        : updates.download,
                    icon: Icon(
                      updates.downloaded
                          ? LucideIcons.packageCheck
                          : LucideIcons.download,
                    ),
                    label: Text(
                      updates.downloaded ? 'Install update' : 'Download update',
                    ),
                  ),
                if (!updates.busy && !updates.downloaded)
                  TextButton.icon(
                    key: const Key('check-update'),
                    autofocus: widget.isTV && !updates.available,
                    onPressed: updates.check,
                    icon: const Icon(LucideIcons.refreshCw),
                    label: const Text('Check for updates'),
                  ),
                if (phase == UpdatePhase.downloading ||
                    phase == UpdatePhase.checking)
                  TextButton(
                    key: const Key('cancel-update'),
                    autofocus: widget.isTV,
                    onPressed: updates.cancel,
                    child: const Text('Cancel'),
                  ),
              ],
            ),
            const SizedBox(height: 12),
            Text(
              'Your profiles, accounts and watch progress stay on this device. Android asks you to approve installation.',
              style: uiStyle(context, size: 15, color: muted),
            ),
          ],
        ),
      );
    },
  );
}
