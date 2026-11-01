import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'app_layout.dart';
import 'core_bridge.dart';
import 'drama_actions.dart';
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
    this.onExitLeft,
  });

  final AppRepository repository;
  final LocalStore store;
  final bool history;
  final ValueChanged<Drama> onOpen;
  final ValueChanged<Drama> onContinue;
  final RemoteGridFocusController? gridFocusController;
  final VoidCallback? onExitLeft;

  @override
  State<SavedLibrary> createState() => _SavedLibraryState();
}

class _SavedLibraryState extends State<SavedLibrary> {
  final FocusNode _clearButtonFocusNode = FocusNode(
    debugLabel: 'saved-library-clear-action',
  );

  @override
  void dispose() {
    widget.gridFocusController?.setFallback(null);
    _clearButtonFocusNode.dispose();
    super.dispose();
  }

  Future<void> _clearHistory() async {
    final epoch = widget.store.profileEpoch;
    final accepted = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('清空觀看記錄？'),
        content: const Text('這會刪除當前使用者的觀看進度，收藏與新集數提醒會保留。'),
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

  Future<void> _clearFavorites() async {
    final epoch = widget.store.profileEpoch;
    final accepted = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('清空收藏？'),
        content: const Text('這會移除目前使用者的全部收藏及更新提醒，不會刪除最近觀看記錄。'),
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
      await saveUserChange(context, widget.store.clearFavorites);
    }
  }

  void _focusClearButton() {
    if (_clearButtonFocusNode.context != null) {
      _clearButtonFocusNode.requestFocus();
    }
  }

  KeyEventResult _clearButtonKey(FocusNode node, KeyEvent event) {
    if (!AppLayout.isTelevision(context) ||
        (event is! KeyDownEvent && event is! KeyRepeatEvent)) {
      return KeyEventResult.ignored;
    }
    final key = event.logicalKey;
    if (key == LogicalKeyboardKey.arrowDown) {
      FocusScope.of(context).focusInDirection(TraversalDirection.down);
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.arrowUp) {
      FocusScope.of(context).focusInDirection(TraversalDirection.up);
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.arrowLeft) {
      final exitLeft = widget.onExitLeft;
      if (exitLeft != null) {
        exitLeft();
        return KeyEventResult.handled;
      }
    }
    if (key == LogicalKeyboardKey.arrowRight) {
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
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
    final badge = state != null && state.hasUpdates
        ? state.updateLabel
        : null;
    return DramaTile(
      key: ValueKey('saved-${drama.id}'),
      drama: drama,
      repository: widget.repository,
      focusNode: focusNode,
      onFocus: onFocus,
      onTap: () => widget.history ? widget.onContinue(drama) : widget.onOpen(drama),
      onMore: () => _actions(drama),
      onLongPress: () => _actions(drama),
      // 收藏與最近觀看皆以長按開啟操作選單，不顯示三點操作鈕。
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
      widget.gridFocusController?.setFallback(
        all.isEmpty ? null : _focusClearButton,
      );
      final items = all;
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
              if (all.isNotEmpty)
                Focus(
                  canRequestFocus: false,
                  skipTraversal: true,
                  onKeyEvent: _clearButtonKey,
                  child: IconButton(
                    focusNode: _clearButtonFocusNode,
                    tooltip: widget.history ? '清空觀看記錄' : '清空收藏',
                    onPressed: widget.history ? _clearHistory : _clearFavorites,
                    icon: const Icon(Icons.delete_outline_rounded),
                  ),
                ),
            ],
          ),
        ),
      ];
      if (items.isEmpty) widget.gridFocusController?.clear();
      final empty = StatusPanel(
        title: widget.history ? '還沒有觀看記錄' : '還沒有收藏',
        message: '去挑一部喜歡的短劇',
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
                            'saved-tv-${widget.history}',
                          ),
                          itemKeys: items.map((item) => item.id).toList(),
                          columns: columns,
                          focusController: widget.gridFocusController,
                          onExitLeft: widget.onExitLeft,
                          onExitUp: _focusClearButton,
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
              'saved-${widget.history}',
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
