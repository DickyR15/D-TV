import 'package:flutter/material.dart';

import 'catalog_sort.dart';

String _formatLoadedCount(int count) => count.toString().replaceAllMapped(
  RegExp(r'\B(?=(\d{3})+(?!\d))'),
  (_) => ',',
);

Future<CatalogView?> chooseCatalogView(
  BuildContext context,
  CatalogView current, {
  Set<CatalogSort>? availableSorts,
  bool showReleaseFilter = true,
  int? loadedCount,
}) => showModalBottomSheet<CatalogView>(
  context: context,
  showDragHandle: true,
  isScrollControlled: true,
  useSafeArea: true,
  builder: (context) {
    final sorts = availableSorts ?? CatalogSort.values.toSet();
    var selected = current;
    return StatefulBuilder(
      builder: (context, update) => SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(
          20,
          0,
          20,
          20 + MediaQuery.viewInsetsOf(context).bottom,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('排序與篩選', style: Theme.of(context).textTheme.titleLarge),
            if (loadedCount != null) ...[
              const SizedBox(height: 4),
              Text(
                '目前已載入 ${_formatLoadedCount(loadedCount)} 部',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
            const SizedBox(height: 16),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final sort in CatalogSort.values.where(sorts.contains))
                  ChoiceChip(
                    label: Text(sort.label),
                    selected: selected.sort == sort,
                    onSelected: (_) =>
                        update(() => selected = selected.copyWith(sort: sort)),
                  ),
              ],
            ),
            if (showReleaseFilter) ...[
              const SizedBox(height: 24),
              const Text('連載狀態'),
              const SizedBox(height: 8),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                for (final entry in const {
                  '': '全部',
                  'ongoing': '連載中',
                  'finished': '已完結',
                  'unknown': '未知',
                }.entries)
                  ChoiceChip(
                    label: Text(entry.value),
                    selected: selected.release == entry.key,
                    onSelected: (_) => update(
                      () => selected = selected.copyWith(release: entry.key),
                    ),
                  ),
                ],
              ),
            ],
            const SizedBox(height: 16),
            Text(
              '排序會套用至目前已載入的內容；繼續載入更多內容後，也會一併重新排序。沒有對應資料的排序或狀態篩選選項將不會顯示。',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            const SizedBox(height: 20),
            SizedBox(
              width: double.infinity,
              child: FilledButton(
                onPressed: () => Navigator.pop(context, selected),
                child: const Text('應用'),
              ),
            ),
          ],
        ),
      ),
    );
  },
);
