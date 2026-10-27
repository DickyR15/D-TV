import 'package:flutter/material.dart';

import 'local_store.dart';
import 'models.dart';
import 'widgets.dart';
import 'ui_localization.dart';

Future<void> showDramaActions(
  BuildContext context, {
  required Drama drama,
  required LocalStore store,
  VoidCallback? onContinue,
  bool history = false,
}) async {
  final epoch = store.profileEpoch;
  final following = store.following(drama.id);
  final choices = <(String, String, IconData)>[
    if (onContinue != null && store.watched(drama.id) != null)
      ('continue', '繼續觀看', Icons.play_arrow_rounded),
    ('favorite', following == null ? '加入收藏' : '取消收藏', Icons.bookmark_outline),
    if (following != null && following.hasUpdates)
      ('read', '標記 ${following.updateLabel}已讀', Icons.mark_email_read_outlined),
    if (history && store.watched(drama.id) != null)
      ('removeHistory', '刪除這條觀看記錄', Icons.history_toggle_off),
  ];
  final choice = await showDialog<String>(
    context: context,
    builder: (context) => SimpleDialog(
      title: Text(uiTraditional(drama.title), maxLines: 2, overflow: TextOverflow.ellipsis),
      children: [
        for (final entry in choices.indexed)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 8),
            child: SizedBox(
              width: double.infinity,
              child: TextButton.icon(
                key: ValueKey('drama-action-${entry.$2.$1}'),
                autofocus: entry.$2.$1 == 'favorite',
                onPressed: () => Navigator.pop(context, entry.$2.$1),
                style: TextButton.styleFrom(
                  alignment: Alignment.centerLeft,
                  minimumSize: const Size.fromHeight(52),
                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                ),
                icon: Icon(entry.$2.$3, size: 22),
                label: Text(entry.$2.$2),
              ),
            ),
          ),
      ],
    ),
  );
  if (choice == null ||
      !context.mounted ||
      epoch != store.profileEpoch ||
      !store.allowsSource(drama.source)) {
    return;
  }
  switch (choice) {
    case 'continue':
      onContinue?.call();
    case 'favorite':
      await saveUserChange(context, () => store.toggleFavorite(drama));
    case 'read':
      await saveUserChange(context, () => store.markUpdatesRead(drama.id));
    case 'removeHistory':
      await saveUserChange(context, () => store.removeHistory(drama.id));
  }
}

class DramaActionButton extends StatelessWidget {
  const DramaActionButton({
    super.key,
    required this.drama,
    required this.onPressed,
  });
  final Drama drama;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) => IconButton.filledTonal(
    key: ValueKey('drama-actions-${drama.id}'),
    tooltip: '${uiTraditional(drama.title)} · 更多操作',
    onPressed: onPressed,
    icon: const Icon(Icons.more_horiz_rounded, size: 20),
    style: IconButton.styleFrom(
      backgroundColor: Colors.black.withValues(alpha: .64),
      foregroundColor: Colors.white,
      minimumSize: const Size(40, 40),
      padding: const EdgeInsets.all(8),
      visualDensity: VisualDensity.compact,
    ),
  );
}
