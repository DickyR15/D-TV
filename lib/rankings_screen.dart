import 'dart:async';

import 'package:flutter/material.dart';

import 'app_layout.dart';

import 'catalog_filters.dart';
import 'core_bridge.dart';
import 'detail_screen.dart';
import 'playback_launch_screen.dart';
import 'drama_actions.dart';
import 'local_store.dart';
import 'models.dart';
import 'ranking_models.dart';
import 'remote_widgets.dart';
import 'widgets.dart';
import 'ui_localization.dart';

class RankingsScreen extends StatefulWidget {
  const RankingsScreen({
    super.key,
    required this.repository,
    required this.store,
    required this.initialGroup,
    this.initialBoardId,
    this.embedded = false,
    this.onExitLeft,
  });
  final AppRepository repository;
  final LocalStore store;
  final String initialGroup;
  final String? initialBoardId;
  final bool embedded;
  final VoidCallback? onExitLeft;

  @override
  State<RankingsScreen> createState() => _RankingsScreenState();
}

class _RankingSnapshot {
  _RankingSnapshot({
    required this.items,
    required this.page,
    required this.hasMore,
    required this.updatedText,
    required this.checkedAt,
  });

  final List<RankingItem> items;
  final int page;
  final bool hasMore;
  final String updatedText;
  final DateTime checkedAt;

  bool get isFresh {
    final age = DateTime.now().difference(checkedAt);
    // Leave a small margin under the native ranking cache's five-minute TTL.
    return !age.isNegative && age < const Duration(minutes: 4);
  }
}

class _RankingMemory {
  List<RankingBoard>? boards;
  Future<List<RankingBoard>>? boardsFuture;
  final pages = <String, _RankingSnapshot>{};
}

final Expando<_RankingMemory> _rankingMemoryByRepository =
    Expando<_RankingMemory>('D-TV ranking memory');

class _RankingsScreenState extends State<RankingsScreen> {
  final _scroll = ScrollController();
  List<RankingBoard> _boards = [];
  RankingBoard? _board;
  List<RankingItem> _items = [];
  bool _loading = true;
  bool _more = false;
  bool _hasMore = false;
  bool _failedMore = false;
  int _page = 1;
  int _generation = 0;
  int _automaticRetries = 0;
  Timer? _retryTimer;
  String? _error;
  String _updated = '';

  _RankingMemory get _memory =>
      _rankingMemoryByRepository[widget.repository] ??= _RankingMemory();

  String _userFacingError(Object error) {
    final message = error.toString();
    if (message.contains('context deadline exceeded') ||
        message.contains('deadline exceeded') ||
        message.contains('i/o timeout')) {
      return '榜單請求逾時，請重試或稍後再試。';
    }
    return message;
  }

  Future<List<RankingBoard>> _loadBoardsCached() {
    final memory = _memory;
    final boards = memory.boards;
    if (boards != null) return Future.value(boards);
    final pending = memory.boardsFuture;
    if (pending != null) return pending;

    late final Future<List<RankingBoard>> future;
    future = widget.repository.rankingBoards().then((result) {
      memory.boards = List<RankingBoard>.unmodifiable(result);
      return memory.boards!;
    }).whenComplete(() {
      if (identical(memory.boardsFuture, future)) {
        memory.boardsFuture = null;
      }
    });
    memory.boardsFuture = future;
    return future;
  }

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_onScroll);
    widget.repository.catalogUpdates.addListener(_metadataChanged);
    _initialize();
  }

  @override
  void dispose() {
    widget.repository.catalogUpdates.removeListener(_metadataChanged);
    _generation++;
    _retryTimer?.cancel();
    _scroll.removeListener(_onScroll);
    _scroll.dispose();
    super.dispose();
  }

  void _onScroll() {
    if (!mounted || !_hasMore || _loading || _more || !_scroll.hasClients) {
      return;
    }
    final position = _scroll.position;
    final threshold = (position.viewportDimension * 1.2).clamp(260.0, 720.0);
    if (position.extentAfter <= threshold) {
      _load(more: true);
    }
  }

  void _metadataChanged() {
    if (mounted) setState(() {});
  }

  Future<void> _initialize() async {
    final generation = ++_generation;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final boards = (await _loadBoardsCached())
          .where((board) => widget.store.allowsSource(board.source))
          .toList();
      if (!mounted || generation != _generation) return;
      final selectedBoard =
          boards.where((board) => board.id == widget.initialBoardId).firstOrNull ??
          boards.where((board) => board.groupId == widget.initialGroup).firstOrNull ??
          boards.firstOrNull;
      final cached = selectedBoard == null
          ? null
          : _memory.pages[selectedBoard.id];
      setState(() {
        _boards = boards;
        _board = selectedBoard;
        if (cached != null && cached.items.isNotEmpty) {
          _items = List<RankingItem>.of(cached.items);
          _page = cached.page;
          _hasMore = cached.hasMore;
          _updated = cached.updatedText;
        }
        _loading = false;
      });
      if (_board != null && (cached == null || !cached.isFresh)) {
        // A stale in-memory list stays visible while the native layer refreshes.
        await _load();
      }
    } catch (error) {
      if (mounted && generation == _generation) {
        setState(() {
          _loading = false;
          _error = _userFacingError(error);
        });
      }
    }
  }

  Future<void> _load({
    bool more = false,
    bool force = false,
    bool quiet = false,
  }) async {
    final board = _board;
    if (board == null || more && (_loading || _more || !_hasMore)) return;
    final generation = ++_generation;
    setState(() {
      _loading = !more && !quiet;
      _more = more;
      if (!quiet) {
        _error = null;
        _failedMore = false;
      }
    });
    try {
      final result = await widget.repository.rankings(
        board.id,
        page: more ? _page + 1 : 1,
        force: force,
      );
      if (!mounted || generation != _generation) return;
      final rows = <String, RankingItem>{
        if (more)
          for (final item in _items) item.drama.id: item,
        for (final item in result.items) item.drama.id: item,
      };
      final mergedItems = rows.values.toList()
        ..sort((a, b) => a.rank.compareTo(b.rank));
      setState(() {
        _items = mergedItems;
        _page = result.page;
        _hasMore = result.hasMore;
        _updated = result.updatedText;
        _error = result.warning.isEmpty ? null : result.warning;
        _loading = false;
        _more = false;
      });
      _memory.pages[board.id] = _RankingSnapshot(
        items: List<RankingItem>.unmodifiable(mergedItems),
        page: result.page,
        hasMore: result.hasMore,
        updatedText: result.updatedText,
        // A stale native page must remain stale in the Dart memory cache.
        checkedAt: result.stale
            ? DateTime.now().subtract(const Duration(minutes: 5))
            : DateTime.now(),
      );
      _retryTimer?.cancel();
      _automaticRetries = 0;
      if (result.stale && !more && !force) {
        final staleGeneration = generation;
        _retryTimer = Timer(const Duration(milliseconds: 250), () {
          if (!mounted ||
              staleGeneration != _generation ||
              _board?.id != board.id) {
            return;
          }
          // The native layer already started a refresh. Join it without
          // restarting the visible top progress bar over cached posters.
          unawaited(_load(force: true, quiet: true));
        });
      }
    } catch (error) {
      if (mounted && generation == _generation) {
        final shouldRetryAutomatically =
            !more && _items.isEmpty && _automaticRetries == 0;
        setState(() {
          _error = _userFacingError(error);
          _failedMore = more;
          _loading = false;
          _more = false;
        });
        // A transient timeout is common on first connection. Retry once
        // automatically so users don't need to focus the Retry button.
        if (shouldRetryAutomatically) {
          _automaticRetries++;
          _retryTimer?.cancel();
          _retryTimer = Timer(const Duration(milliseconds: 800), () {
            if (!mounted ||
                generation != _generation ||
                _board?.id != board.id ||
                _items.isNotEmpty) {
              return;
            }
            unawaited(_load());
          });
        }
      }
    }
  }

  void _select(RankingBoard board) {
    if (_board?.id == board.id) return;
    final cached = _memory.pages[board.id];
    _retryTimer?.cancel();
    _automaticRetries = 0;
    setState(() {
      _board = board;
      _items = cached == null ? [] : List<RankingItem>.of(cached.items);
      _updated = cached?.updatedText ?? '';
      _page = cached?.page ?? 1;
      _hasMore = cached?.hasMore ?? false;
      _error = null;
      _failedMore = false;
      _more = false;
      _loading = cached == null || !cached.isFresh;
    });
    if (_scroll.hasClients) _scroll.jumpTo(0);
    if (cached == null || !cached.isFresh) {
      // Keep old rows onscreen instead of clearing the grid while fetching.
      _load();
    }
  }

  String _boardTitle(RankingBoard? board) => switch (board?.id) {
    'hongguo-hot' => '紅果熱播榜',
    'hongguo-real' => '真人熱播榜',
    'hongguo-ai' => 'AI熱播榜',
    'hongguo-comic' => '漫劇熱播榜',
    _ => uiTraditional(board?.name ?? '榜單'),
  };

  void _openDrama(RankingItem item) {
    CoverRequestScheduler.pauseBackground();
    unawaited(
      openPlaybackDirectly(
        context,
        drama: item.drama,
        repository: widget.repository,
        store: widget.store,
      ).whenComplete(CoverRequestScheduler.resumeBackground),
    );
  }

  Widget _loadMoreFooter() => Center(
    child: _more
        ? const Padding(
            padding: EdgeInsets.all(16),
            child: CircularProgressIndicator(),
          )
        : _hasMore
        ? OutlinedButton.icon(
            onPressed: () => _load(more: true),
            icon: const Icon(Icons.expand_more_rounded),
            label: const Text('載入更多'),
          )
        : const Padding(
            padding: EdgeInsets.all(16),
            child: Text('已顯示全部榜單'),
          ),
  );

  Widget _rankingList(List<RankingItem> items) => RefreshIndicator(
    onRefresh: () => _load(force: true),
    child: ListView.builder(
      key: ValueKey('ranking-${_board!.id}'),
      controller: _scroll,
      physics: const AlwaysScrollableScrollPhysics(),
      padding: const EdgeInsets.fromLTRB(12, 0, 12, 24),
      itemCount: items.length + 1,
      itemBuilder: (context, index) {
        if (index == items.length) return _loadMoreFooter();
        final item = items[index];
        return Card(
          child: InkWell(
            key: ValueKey('rank-${item.rank}-${item.drama.id}'),
            borderRadius: BorderRadius.circular(12),
            onTap: () => _openDrama(item),
            child: Padding(
              padding: const EdgeInsets.all(12),
              child: Row(
                children: [
                  SizedBox(
                    width: 36,
                    child: Text(
                      '${item.rank}',
                      style: TextStyle(
                        fontSize: 24,
                        fontWeight: FontWeight.w800,
                        color: item.rank <= 3
                            ? Theme.of(context).colorScheme.primary
                            : Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
                  SizedBox(
                    width: 72,
                    height: 108,
                    child: DramaCover(
                      drama: item.drama,
                      repository: widget.repository,
                      radius: 8,
                    ),
                  ),
                  const SizedBox(width: 14),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          uiTraditional(item.drama.title),
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                        if (item.drama.category.isNotEmpty)
                          Text(
                            uiTraditional(item.drama.category),
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                        if (item.metric.isNotEmpty)
                          Padding(
                            padding: const EdgeInsets.only(top: 8),
                            child: Text(
                              item.metric,
                              style: TextStyle(
                                color: Theme.of(context).colorScheme.primary,
                              ),
                            ),
                          ),
                      ],
                    ),
                  ),
                  const Icon(Icons.chevron_right_rounded),
                ],
              ),
            ),
          ),
        );
      },
    ),
  );

  Widget _rankingTile(
    RankingItem item, {
    required double posterWidth,
    required bool remote,
    FocusNode? focusNode,
    VoidCallback? onFocus,
  }) {
    final theme = Theme.of(context);
    final following = widget.store.following(item.drama.id);
    final badgeBackground = switch (item.rank) {
      1 => const Color(0xFFFFD166),
      2 => const Color(0xFFE0E5EB),
      3 => const Color(0xFFCD9365),
      _ => const Color(0xB8202328),
    };
    final badgeForeground = item.rank <= 2
        ? const Color(0xFF252328)
        : Colors.white;
    final card = Container(
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainer,
        borderRadius: BorderRadius.circular(14),
      ),
      clipBehavior: Clip.antiAlias,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SizedBox(
            height: posterWidth * 1.5,
            child: Stack(
              fit: StackFit.expand,
              children: [
                DramaCover(
                  drama: item.drama,
                  repository: widget.repository,
                  radius: 0,
                  showGradient: false,
                  showEpisodeBadge: false,
                ),
                Positioned(
                  top: 8,
                  left: 8,
                  child: Container(
                    constraints: const BoxConstraints(minWidth: 28, minHeight: 28),
                    alignment: Alignment.center,
                    padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
                    decoration: BoxDecoration(
                      color: badgeBackground,
                      borderRadius: BorderRadius.circular(7),
                    ),
                    child: Text(
                      '${item.rank}',
                      style: TextStyle(
                        color: badgeForeground,
                        fontSize: 14,
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                ),
                if (following != null)
                  Positioned(
                    left: 8,
                    right: 8,
                    bottom: 8,
                    child: Align(
                      alignment: Alignment.bottomLeft,
                      child: Container(
                        padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 6),
                        decoration: BoxDecoration(
                          color: const Color(0xE61A1118),
                          borderRadius: BorderRadius.circular(8),
                          border: Border.all(color: const Color(0x66FFFFFF)),
                        ),
                        child: Text(
                          '${following.status.label}${following.hasUpdates ? ' · ${following.updateLabel}' : ''}',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            color: Colors.white,
                            fontSize: 12,
                            fontWeight: FontWeight.w700,
                            shadows: [Shadow(color: Colors.black54, blurRadius: 4)],
                          ),
                        ),
                      ),
                    ),
                  ),
              ],
            ),
          ),
          Expanded(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(9, 8, 9, 7),
              child: Text(
                uiTraditional(item.drama.title),
                key: ValueKey('rank-title-${item.rank}-${item.drama.id}'),
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(
                  fontSize: 14.5,
                  height: 1.25,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
          ),
        ],
      ),
    );
    if (remote) {
      return RemoteTarget(
        key: ValueKey('rank-${item.rank}-${item.drama.id}'),
        focusNode: focusNode,
        onFocus: onFocus,
        onPressed: () => _openDrama(item),
        onLongPress: () => showDramaActions(
          context,
          drama: item.drama,
          store: widget.store,
          onContinue: () => _openDrama(item),
        ),
        label: '第 ${item.rank} 名，${uiTraditional(item.drama.title)}，長按開啟標記選單',
        padding: const EdgeInsets.all(2),
        radius: 15,
        borderWidth: 2,
        outlined: true,
        child: card,
      );
    }
    return Material(
      color: Colors.transparent,
      child: InkWell(
        key: ValueKey('rank-${item.rank}-${item.drama.id}'),
        onTap: () => _openDrama(item),
        onLongPress: () => showDramaActions(
          context,
          drama: item.drama,
          store: widget.store,
          onContinue: () => _openDrama(item),
        ),
        borderRadius: BorderRadius.circular(14),
        child: card,
      ),
    );
  }

  /// Keep ranking poster density in sync with the wide category grid.
  int _posterColumns(double width) =>
      width >= 620 ? 5 : width >= 420 ? 3 : 2;

  Widget _rankingGrid(List<RankingItem> items, double width) {
    final television = AppLayout.isTelevision(context);
    final columns = _posterColumns(width);
    const spacing = 14.0;
    const horizontalPadding = 18.0;
    final posterWidth =
        (width - horizontalPadding * 2 - spacing * (columns - 1)) / columns;
    final itemExtent = posterWidth * 1.5 + 66;
    final footer = Padding(
      padding: const EdgeInsets.only(top: 10, bottom: 18),
      child: _loadMoreFooter(),
    );
    if (television) {
      return RemoteGrid(
        key: ValueKey('ranking-grid-${_board!.id}'),
        itemKeys: items.map((item) => item.drama.id).toList(),
        columns: columns,
        itemExtent: itemExtent,
        spacing: spacing,
        padding: const EdgeInsets.fromLTRB(
          horizontalPadding,
          0,
          horizontalPadding,
          12,
        ),
        controller: _scroll,
        // Return left-edge focus to the active ranking in the side navigation.
        autofocus: false,
        onExitLeft: widget.onExitLeft,
        itemBuilder: (context, index, focusNode, onFocus) => _rankingTile(
          items[index],
          posterWidth: posterWidth - 4,
          remote: true,
          focusNode: focusNode,
          onFocus: onFocus,
        ),
        footer: footer,
      );
    }
    return RefreshIndicator(
      onRefresh: () => _load(force: true),
      child: CustomScrollView(
        key: ValueKey('ranking-grid-${_board!.id}'),
        controller: _scroll,
        physics: const AlwaysScrollableScrollPhysics(),
        slivers: [
          SliverPadding(
            padding: const EdgeInsets.fromLTRB(
              horizontalPadding,
              0,
              horizontalPadding,
              8,
            ),
            sliver: SliverGrid(
              gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                crossAxisCount: columns,
                mainAxisExtent: itemExtent,
                crossAxisSpacing: spacing,
                mainAxisSpacing: spacing,
              ),
              delegate: SliverChildBuilderDelegate(
                (context, index) => _rankingTile(
                  items[index],
                  posterWidth: posterWidth,
                  remote: false,
                ),
                childCount: items.length,
              ),
            ),
          ),
          SliverToBoxAdapter(child: footer),
        ],
      ),
    );
  }

  Future<void> _scrollToTop() async {
    if (!_scroll.hasClients) return;
    await _scroll.animateTo(
      0,
      duration: const Duration(milliseconds: 260),
      curve: Curves.easeOutCubic,
    );
  }

  Widget _wideFooter(int count) {
    final theme = Theme.of(context);
    final status = _more
        ? '正在載入更多…'
        : _hasMore
        ? '上滑載入更多'
        : '已顯示全部短劇';
    return SafeArea(
      top: false,
      child: Container(
        height: 64,
        padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 6),
        decoration: BoxDecoration(
          color: theme.colorScheme.surface,
          border: Border(top: BorderSide(color: theme.colorScheme.outlineVariant)),
        ),
        child: Row(
          children: [
            FilledButton.tonalIcon(
              onPressed: _scrollToTop,
              icon: const Icon(Icons.keyboard_double_arrow_up_rounded),
              label: const Text('回到頂部'),
            ),
            const Spacer(),
            Text(
              '已載入 $count 部短劇 · $status',
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            const Spacer(),
            FilledButton.tonalIcon(
              onPressed: _loading || _more ? null : () => _load(force: true),
              icon: const Icon(Icons.refresh_rounded),
              label: const Text('重新整理'),
            ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final wide = AppLayout.isTelevision(context) || MediaQuery.sizeOf(context).width >= 840;
    final groups = SourceGroup.fromSources(

      _boards.map((board) => SourceSite.byId(board.source)).toSet(),
    );
    final boards = _boards
        .where((board) => board.groupId == _board?.groupId)
        .toList();
    final items = _items
        .map(
          (item) => RankingItem(
            item.rank,
            widget.repository.catalogUpdates.current(item.drama),
            metric: uiTraditional(item.metric),
          ),
        )
        .where(
          (item) =>
              widget.store.allowsSource(item.drama.source) &&
              !(item.drama.source == 'huangdou' &&
                  widget.store.hideVip &&
                  item.drama.vip),
        )
        .toList();
    return Scaffold(
      appBar: widget.embedded ? null : AppBar(
        title: PopupMenuButton<SourceGroup>(
          tooltip: '切換榜單站源',
          enabled: groups.length > 1,
          onSelected: (group) =>
              _select(_boards.firstWhere((board) => board.groupId == group.id)),
          itemBuilder: (_) => [
            for (final group in groups)
              PopupMenuItem(value: group, child: Text(uiTraditional(group.name))),
          ],
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(
                _board == null
                    ? '榜單'
                    : '${uiTraditional(SourceSite.byId(_board!.source).groupName)}榜單',
              ),
              if (groups.length > 1) const Icon(Icons.expand_more_rounded),
            ],
          ),
        ),
        actions: [
          RefreshAction(
            loading: _loading || _more,
            tooltip: '更新榜單',
            onPressed: _board == null ? _initialize : () => _load(force: true),
          ),
        ],
      ),
      bottomNavigationBar: wide ? _wideFooter(items.length) : null,
      body: SafeArea(
        top: false,
        child: Column(
          children: [
            if (widget.embedded)
              Padding(
                padding: EdgeInsets.fromLTRB(18, wide ? 14 : 8, 18, 8),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.center,
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            _boardTitle(_board),
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: Theme.of(context).textTheme.headlineSmall?.copyWith(
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                          const SizedBox(height: 4),
                          Text(
                            '已載入 ${items.length} 部短劇',
                            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                              color: Theme.of(context).colorScheme.onSurfaceVariant,
                            ),
                          ),
                        ],
                      ),
                    ),
                    if (!wide)
                      RefreshAction(
                        loading: _loading || _more,
                        tooltip: '更新榜單',
                        onPressed: _board == null ? _initialize : () => _load(force: true),
                      ),
                  ],
                ),
              ),
            if (!widget.embedded && boards.isNotEmpty)
              CatalogFilters(
                key: ValueKey('ranking-boards-${_board!.groupId}'),
                categories: [
                  for (final board in boards)
                    CatalogCategory(board.id, uiTraditional(board.name)),
                ],
                category: _board!.id,
                onCategory: (id) =>
                    _select(boards.firstWhere((board) => board.id == id)),
                onRetry: () => _load(force: true),
              ),
            if (_board != null && !widget.embedded)
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
                child: Align(
                  alignment: Alignment.centerLeft,
                  child: Text(
                    [
                      uiTraditional(_board!.description),
                      if (_updated.isNotEmpty) uiTraditional(_updated),
                    ].join(' '),
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ),
              ),
            if (_loading && items.isNotEmpty)
              const LinearProgressIndicator(minHeight: 2),
            if (_error != null && items.isNotEmpty)
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        uiTraditional(_error!),
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
                    ),
                    TextButton(
                      onPressed: _loading || _more
                          ? null
                          : () => _load(more: _failedMore, force: true),
                      child: const Text('重試'),
                    ),
                  ],
                ),
              ),
            Expanded(
              child: _loading && items.isEmpty
                  ? Center(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          const CircularProgressIndicator(),
                          if (_board?.source == 'hongguo') ...[
                            const SizedBox(height: 16),
                            const Text('正在取得榜單，資料未完整返回時會自動重試'),
                          ],
                        ],
                      ),
                    )
                  : items.isEmpty
                  ? StatusPanel(
                      title: _error == null ? '暫無榜單內容' : '榜單暫時無法載入',
                      message: uiTraditional(_error ?? '目前站源暫無可顯示的榜單。'),
                      onRetry: _board == null
                          ? _initialize
                          : () => _load(force: true),
                    )
                  : LayoutBuilder(
                      builder: (context, constraints) {
                        if (wide) {
                          return _rankingGrid(items, constraints.maxWidth);
                        }
                        return _rankingList(items);
                      },
                    ),

            ),
          ],
        ),
      ),
    );
  }
}
