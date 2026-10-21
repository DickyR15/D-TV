import 'package:flutter/material.dart';

import 'catalog_sort.dart';

Future<CatalogView?> chooseCatalogView(
  BuildContext context,
  CatalogView current,
) => showModalBottomSheet<CatalogView>(
  context: context,
  showDragHandle: true,
  isScrollControlled: true,
  useSafeArea: true,
  builder: (context) {
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
            const SizedBox(height: 16),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final sort in CatalogSort.values)
                  ChoiceChip(
                    label: Text(sort.label),
                    selected: selected.sort == sort,
                    onSelected: (_) =>
                        update(() => selected = selected.copyWith(sort: sort)),
                  ),
              ],
            ),
            const SizedBox(height: 24),
            const Text('劇集狀態'),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final entry in const {
                  '': '全部',
                  'ongoing': '連載中',
                  'finished': '已完結',
                  'unknown': '狀態未知',
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
            const SizedBox(height: 16),
            Text(
              '排序和篩選作用於已載入的劇集；缺少排序資料的條目排在最後。',
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
