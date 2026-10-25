import 'package:flutter/material.dart';

import 'app_layout.dart';
import 'catalog_sort.dart';
import 'core_bridge.dart';
import 'drama_actions.dart';
import 'follow_state.dart';
import 'local_store.dart';
import 'models.dart';
import 'remote_widgets.dart';
import 'widgets.dart';
import 'ui_localization.dart';

class SavedLibrary extends StatefulWidget {
  const SavedLibrary({
    super.key,
    required this.repository,
    required this.store,
    required this.history,
    required this.onOpen,
    required this.onContinue,
    this.gridFocusController,
    this.searchFocusNode,
    this.onExitLeft,
  });

  final AppRepository repository;
  final LocalStore store;
  final bool history;
  final ValueChanged<Drama> onOpen;
  final ValueChanged<Drama> onContinue;
  final RemoteGridFocusController? gridFocusController;
  final FocusNode? searchFocusNode;
  final VoidCallback? onExitLeft;

  @override
  State<SavedLibrary> createState() => _SavedLibraryState();
}

class _SavedLibraryState extends State<SavedLibrary> {
  final _search = TextEditingController();
  String _filter = '';

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  Future<void> _clearHistory() async {
    final epoch = widget.store.profileEpoch;
    final accepted = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('清空觀看記錄？'),
        content: const Text('這會刪除當前使用者的觀看進度，收藏狀態和手動已看標記會保留。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            autofocus: AppLayout.isTelevision(context),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('清空'),
          ),
        ],
      ),
    );
    if (accepted == true && mounted && epoch == widget.store.profileEpoch) {
      await saveUserChange(context, widget.store.clearHistory);
    }
  }

  void _actions(Drama drama) => showDramaActions(
    context,
    drama: drama,
    store: widget.store,
    history: widget.history,
    onContinue: () => widget.onContinue(drama),
  );

  Widget _tile(Drama drama, {FocusNode? focusNode, VoidCallback? onFocus}) {
    final watched = widget.store.watched(drama.id);
    final state = widget.store.following(drama.id);
    final badge = state == null
        ? null
        : '${state.label}${state.hasUpdates ? ' · ${state.updateLabel}' : ''}';
    return DramaTile(
      key: ValueKey('saved-${drama.id}'),
      drama: drama,
      repository: widget.repository,
      focusNode: focusNode,
      onFocus: onFocus,
      onTap: () => widget.history ? widget.onContinue(drama) : widget.onOpen(drama),
      onMore: () => _actions(drama),
      onLongPress: () => _actions(drama),
      // 收藏與最近觀看皆以長按開啟標記選單，不顯示三點操作鈕。
      actions: null,
      badge: badge,
      subtitle: watched == null
          ? uiTraditional(SourceSite.byId(drama.source).name)
          : '第 ${watched.episode} 集 · ${formatPosition(watched.position)}',
      remotePadding: AppLayout.isTelevision(context)
          ? const EdgeInsets.all(2)
          : const EdgeInsets.all(4),
    );
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.store,
    builder: (context, _) {
      final history = widget.store.history;
      final all = widget.history
          ? history.map((entry) => entry.drama).toList()
          : widget.store.favorites;
      final items = all.where((drama) {
        if (!matchesDramaQuery(drama, _search.text)) return false;
        final state = widget.store.following(drama.id);
        return widget.history ||
            _filter.isEmpty ||
            (_filter == 'updates'
                ? state?.hasUpdates == true
                : state?.status.name == _filter);
      }).toList();
      final header = [
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 12, 12, 8),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  '${widget.history ? '最近觀看' : '我的收藏'} · ${all.length}',
                  style: Theme.of(context).textTheme.titleLarge,
                ),
              ),
              if (widget.history && all.isNotEmpty)
                IconButton(
                  tooltip: '清空觀看記錄',
                  onPressed: _clearHistory,
                  icon: const Icon(Icons.delete_outline_rounded),
                ),
            ],
          ),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
          child: RemoteTextInput(
            child: TextField(
              key: ValueKey(
                widget.history ? 'history-search' : 'favorites-search',
              ),
              controller: _search,
              focusNode: widget.searchFocusNode,
              onChanged: (_) => setState(() {}),
              textInputAction: TextInputAction.search,
              decoration: InputDecoration(
                hintText: widget.history ? '搜尋觀看記錄' : '搜尋收藏',
                prefixIcon: const Icon(Icons.search_rounded),
                suffixIcon: _search.text.isEmpty
                    ? null
                    : IconButton(
                        tooltip: '清空搜尋',
                        onPressed: () => setState(_search.clear),
                        icon: const Icon(Icons.close_rounded),
                      ),
              ),
            ),
          ),
        ),
        if (!widget.history)
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: Row(
              children: [
                for (final filter in [
                  ('', '全部'),
                  for (final status in FollowStatus.values)
                    (status.name, status.label),
                  ('updates', '有更新'),
                ])
                  Padding(
                    padding: const EdgeInsets.only(right: 8),
                    child: ChoiceChip(
                      key: ValueKey('follow-filter-${filter.$1}'),
                      label: Text(filter.$2),
                      selected: _filter == filter.$1,
                      onSelected: (_) => setState(() => _filter = filter.$1),
                    ),
                  ),
              ],
            ),
          ),
      ];
      if (items.isEmpty) widget.gridFocusController?.clear();
      final empty = StatusPanel(
        title: all.isEmpty
            ? widget.history
                  ? '還沒有觀看記錄'
                  : '還沒有收藏'
            : '沒有符合的紀錄',
        message: all.isEmpty ? '去挑一部喜歡的短劇' : '可以更換搜尋詞或篩選條件。',
        icon: widget.history
            ? Icons.history_rounded
            : Icons.bookmark_border_rounded,
      );
      return LayoutBuilder(
        builder: (context, constraints) {
          if (AppLayout.isTelevision(context)) {
            // Use the same poster count and cell width as the ranking boards:
            // wide TV=5 columns, medium=3, narrow=2.
            final columns = constraints.maxWidth >= 620
                ? 5
                : constraints.maxWidth >= 420
                ? 3
                : 2;
            final tileWidth =
                (constraints.maxWidth - 36 - (columns - 1) * 14) / columns;
            return Column(
              children: [
                ConstrainedBox(
                  constraints: BoxConstraints(
                    maxHeight: constraints.maxHeight * .5,
                  ),
                  child: SingleChildScrollView(child: Column(children: header)),
                ),
                Expanded(
                  child: items.isEmpty
                      ? empty
                      : RemoteGrid(
                          key: ValueKey(
                            'saved-tv-${widget.history}-$_filter-${_search.text}',
                          ),
                          itemKeys: items.map((item) => item.id).toList(),
                          columns: columns,
                          focusController: widget.gridFocusController,
                          onExitLeft: widget.onExitLeft,
                          itemExtent: DramaTile.extentFor(
                                context,
                                tileWidth - 4,
                              ) +
                              4,
                          itemBuilder: (_, index, node, onFocus) => _tile(
                            items[index],
                            focusNode: node,
                            onFocus: onFocus,
                          ),
                        ),
                ),
              ],
            );
          }
          final padding = constraints.maxWidth < 600 ? 16.0 : 24.0;
          return CustomScrollView(
            key: PageStorageKey(
              'saved-${widget.history}-$_filter-${_search.text}',
            ),
            slivers: [
              SliverToBoxAdapter(child: Column(children: header)),
              if (items.isEmpty)
                SliverFillRemaining(hasScrollBody: false, child: empty)
              else
                SliverPadding(
                  padding: EdgeInsets.fromLTRB(padding, 0, padding, 20),
                  sliver: SliverGrid(
                    gridDelegate: dramaGridDelegate(
                      context,
                      constraints.maxWidth - 2 * padding,
                    ),
                    delegate: SliverChildBuilderDelegate(
                      (_, index) => _tile(items[index]),
                      childCount: items.length,
                    ),
                  ),
                ),
            ],
          );
        },
      );
    },
  );
}
