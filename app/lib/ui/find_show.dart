import 'dart:async';

import 'package:flutter/material.dart';

import '../core.dart';
import '../data/models.dart';
import '../services.dart';
import 'components.dart';

/// "Wrong show?": the viewer searches the sources with their own words and
/// picks the right one. The pick is kept for the show and tried before any
/// automatic match. Returns true when a pick was made.
Future<bool> findShowMyself(
  BuildContext context,
  AppServices services,
  Anime anime,
  bool isTV,
) async =>
    await showDialog<bool>(
      context: context,
      builder: (_) =>
          _FindShowDialog(services: services, anime: anime, isTV: isTV),
    ) ==
    true;

class _FindShowDialog extends StatefulWidget {
  const _FindShowDialog({
    required this.services,
    required this.anime,
    required this.isTV,
  });
  final AppServices services;
  final Anime anime;
  final bool isTV;
  @override
  State<_FindShowDialog> createState() => _FindShowDialogState();
}

class _FindShowDialogState extends State<_FindShowDialog> {
  late final query = TextEditingController(text: widget.anime.title);
  RequestScope scope = RequestScope();
  List<FoundShow>? results;
  bool searching = false;

  @override
  void dispose() {
    scope.cancel();
    query.dispose();
    super.dispose();
  }

  Future<void> search() async {
    final text = query.text.trim();
    if (text.isEmpty) return;
    scope.cancel();
    final mine = scope = RequestScope();
    setState(() {
      searching = true;
      results = null;
    });
    try {
      final found = await widget.services.resolver.searchAll(
        widget.anime,
        text,
        widget.services.store.text('SubOrDub'),
        mine,
      );
      if (mounted && identical(mine, scope)) setState(() => results = found);
    } catch (_) {
      if (mounted && identical(mine, scope)) setState(() => results = []);
    } finally {
      if (mounted && identical(mine, scope)) setState(() => searching = false);
    }
  }

  Future<void> pick(FoundShow show) async {
    await widget.services.store.setManualMatch(widget.anime, {
      'provider': show.provider,
      'id': show.id,
      'mode': widget.services.store.text('SubOrDub'),
      'title': show.title,
    });
    if (mounted) Navigator.pop(context, true);
  }

  @override
  Widget build(BuildContext context) {
    final picked = widget.services.store.manualMatch(widget.anime);
    final list = results;
    return AlertDialog(
      backgroundColor: panel,
      title: const Text('Find it myself'),
      content: SizedBox(
        width: 600,
        height: 460,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'Wrong show, or nothing found? Search the sources in your own '
              'words and pick the right one. It is used for this show from now on.',
              style: uiStyle(context, size: 14, color: muted),
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: query,
                    autofocus: true,
                    textInputAction: TextInputAction.search,
                    onSubmitted: (_) => unawaited(search()),
                    decoration: const InputDecoration(
                      hintText: 'Show title as the site might spell it',
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                AppButton(
                  label: searching ? 'Searching' : 'Search',
                  icon: LucideIcons.search,
                  filled: true,
                  isTV: widget.isTV,
                  onPressed: searching ? null : () => unawaited(search()),
                ),
              ],
            ),
            if (picked != null) ...[
              const SizedBox(height: 8),
              Row(
                children: [
                  Expanded(
                    child: Text(
                      'Using your pick: ${picked['title'] ?? ''}',
                      style: uiStyle(context, size: 14, color: ok),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  TextButton(
                    onPressed: () async {
                      await widget.services.store.setManualMatch(
                        widget.anime,
                        null,
                      );
                      widget.services.resolver.forget(widget.anime);
                      if (mounted) setState(() {});
                    },
                    child: const Text('Back to automatic'),
                  ),
                ],
              ),
            ],
            const SizedBox(height: 12),
            Expanded(
              child: searching
                  ? const Center(child: CircularProgressIndicator(color: shu))
                  : list == null
                  ? const SizedBox.shrink()
                  : list.isEmpty
                  ? Center(
                      child: Text(
                        'Nothing found. Try fewer words or another spelling.',
                        style: uiStyle(context, color: muted),
                      ),
                    )
                  : ListView(
                      children: [
                        for (final show in list)
                          ChoiceRow(
                            title: Text(
                              show.title,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                            ),
                            trailing: Text(
                              [
                                if (show.label.isNotEmpty &&
                                    show.label != show.title)
                                  show.label,
                                'Source ${show.place}',
                              ].join(' · '),
                              style: uiStyle(context, size: 14, color: muted),
                            ),
                            onTap: () => unawaited(pick(show)),
                          ),
                      ],
                    ),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context, false),
          child: const Text('Close'),
        ),
      ],
    );
  }
}
