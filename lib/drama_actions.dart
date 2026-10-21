import 'package:flutter/material.dart';

import 'follow_state.dart';
import 'local_store.dart';
import 'models.dart';
import 'widgets.dart';

Future<void> showDramaActions(
  BuildContext context, {
  required Drama drama,
  required LocalStore store,
  VoidCallback? onContinue,
  VoidCallback? onDownload,
  VoidCallback? onSelect,
  bool history = false,
}) async {
  final epoch = store.profileEpoch;
  final following = store.following(drama.id);
  final choices = <(String, String, IconData)>[
    if (onContinue != null && store.watched(drama.id) != null)
      ('continue', '繼續觀看', Icons.play_arrow_rounded),
    ('favorite', following == null ? '加入追劇' : '取消追劇', Icons.bookmark_outline),
    for (final status in FollowStatus.values)
      (
        'status:${status.name}',
        '標記${status.label}',
        switch (status) {
          FollowStatus.planned => Icons.bookmark_add_outlined,
          FollowStatus.watching => Icons.play_circle_outline,
          FollowStatus.watched => Icons.check_circle_outline,
        },
      ),
    if (following != null && following.hasUpdates)
      ('read', '標記 ${following.updateLabel}已讀', Icons.mark_email_read_outlined),
    if (onDownload != null && store.canDownload)
      ('download', '下載選集', Icons.download_outlined),
    if (onSelect != null && store.canDownload)
      ('select', '多選下載', Icons.checklist_rounded),
    if (history && store.watched(drama.id) != null)
      ('removeHistory', '刪除這條觀看記錄', Icons.history_toggle_off),
  ];
  final choice = await showDialog<String>(
    context: context,
    builder: (context) => SimpleDialog(
      title: Text(drama.title, maxLines: 2, overflow: TextOverflow.ellipsis),
      children: [
        for (final entry in choices.indexed)
          SimpleDialogOption(
            onPressed: () => Navigator.pop(context, entry.$2.$1),
            child: Row(
              children: [
                Icon(entry.$2.$3, size: 22),
                const SizedBox(width: 14),
                Expanded(child: Text(entry.$2.$2)),
                if (entry.$2.$1 == 'status:${following?.status.name}')
                  const Icon(Icons.check_rounded, size: 20),
              ],
            ),
          ),
        Padding(
          padding: const EdgeInsets.fromLTRB(24, 10, 24, 8),
          child: Text(
            '手動標記已看會保留真實播放進度。',
            style: Theme.of(context).textTheme.bodySmall,
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
  if (choice.startsWith('status:')) {
    final status = FollowStatus.values.firstWhere(
      (value) => choice == 'status:${value.name}',
    );
    await saveUserChange(context, () => store.setFollowStatus(drama, status));
    return;
  }
  switch (choice) {
    case 'continue':
      onContinue?.call();
    case 'favorite':
      await saveUserChange(context, () => store.toggleFavorite(drama));
    case 'read':
      await saveUserChange(context, () => store.markUpdatesRead(drama.id));
    case 'download':
      if (store.canDownload) onDownload?.call();
    case 'select':
      if (store.canDownload) onSelect?.call();
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
    tooltip: '${drama.title} · 更多操作',
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
