import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'app_layout.dart';
import 'app_bottom_navigation.dart';
import 'core_bridge.dart';
import 'catalog_filters.dart';
import 'catalog_browser.dart';
import 'catalog_sort.dart';
import 'recommendations_screen.dart';
import 'rankings_screen.dart';
import 'playback_launch_screen.dart';
import 'detail_screen.dart';
import 'local_store.dart';
import 'lan_screen.dart';
import 'models.dart';
import 'remote_widgets.dart';
import 'widgets.dart';
import 'vip_icon.dart';
import 'settings_screen.dart';
import 'profiles_screen.dart';
import 'search_input.dart';
import 'sources_screen.dart';
import 'drama_actions.dart';
import 'follow_updater.dart';
import 'library_updater.dart';
import 'saved_library.dart';
import 'ui_localization.dart';
import 'phone_search_dialog.dart';
import 'app_navigation.dart';

class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key, required this.repository, required this.store});
  final AppRepository repository;
  final LocalStore store;
  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  static const _recommendationCategory = 'app:recommendations';
  static const _revealBatch = 60;
  final _search = TextEditingController();
  final _scroll = ScrollController();
  final Map<int, FocusNode> _navigationFocusNodes = {};
  final RemoteGridFocusController _savedGridFocus = RemoteGridFocusController();
  final FocusNode _savedSearchFocusNode = FocusNode(debugLabel: 'saved-library-search');
  Timer? _debounce;
  late SourceSite _source;
  bool _allSources = false;
  List<Drama> _items = [];
  bool _loading = true;
  bool _loadingMore = false;
  bool _hasMore = true;
  String? _error;
  int _generation = 0;
  int _tab = 0;
  bool _initialTabResolved = false;
  String _submittedQuery = '';
  final _categorySelections = <String, String>{};
  late final CatalogBrowser _browser;
  bool _searchVisible = false;
  bool _categoriesLoading = false;
  String? _categoriesError;
  int _categoryGeneration = 0;
  late final LibraryUpdater _updater;
  late final FollowUpdater _followUpdater;
  final _changedSources = <String>{};
  Timer? _cacheRefreshTimer;
  Timer? _watcherStartTimer;
  bool _catalogInitialized = false;
  bool _refreshingUpdatedCache = false;
  bool _showRecommendations = false;
  String _sourceSignature = '';
  bool _catalogLoadScheduled = false;
  int _revealed = _revealBatch;
  int? _revealKey;
  bool _hiddenItems = false;
  List<Drama>? _visibleCacheItems;
  int? _visibleCacheKey;
  List<Drama> _visibleCache = const [];
  DateTime? _lastBackAt;
  DateTime? _lastHandledBackEventAt;
  DateTime? _ignoreExitPromptUntil;
  bool _playbackRouteActive = false;
  bool _exitConfirmationShowing = false;
  bool _homePopupOpen = false;
  PhoneSearchServer? _phoneSearchServer;
  StreamSubscription<String>? _phoneSearchSubscription;
  Future<PhoneSearchServer>? _phoneSearchServerStarting;

  bool get _isCatalogTab => _tab == 0 || _tab == 8;
  bool get _isRankingTab => _tab >= 4 && _tab <= 7;
  String? get _rankingBoardId => switch (_tab) {
    4 => 'hongguo-hot',
    5 => 'hongguo-real',
    6 => 'hongguo-ai',
    7 => 'hongguo-comic',
    _ => null,
  };

  List<SourceGroup> get _sourceGroups {
    final groups = SourceGroup.fromSources(widget.store.sources);
    return [
      if (groups.length > 1) SourceGroup('all', '所有片源', widget.store.sources),
      ...groups,
    ];
  }

  SourceGroup get _group =>
      _sourceGroups
          .where((group) => group.id == (_allSources ? 'all' : _source.groupId))
          .firstOrNull ??
      SourceGroup(_source.groupId, _source.groupName, [_source]);
  bool get _onlineSearch => _group.sources.any((source) => source.onlineSearch);
  bool get _searchSuggestions =>
      _group.sources.any((source) => source.searchSuggestions);
  String get _searchHint {
    final online = _group.sources
        .where((source) => source.onlineSearch)
        .toList();
    if (online.isEmpty) {
      return '篩選本機已更新劇庫';
    }
    final names = online.map((source) => uiTraditional(source.name)).join('、');
    return online.length == _group.sources.length
        ? '搜尋$names'
        : '搜尋$names及本機劇庫';
  }

  String get _category => _categorySelections[_group.id] ?? '';
  List<CatalogCategory> get _categories => _browser.categories(_group);
  String get _displayCategory =>
      _showRecommendations ? _recommendationCategory : _category;
  List<CatalogCategory> get _displayCategories => [
    CatalogCategory.all,
    if (_group.id == 'hongguo')
      const CatalogCategory(_recommendationCategory, '推薦'),
    ..._categories.where((entry) => entry.id.isNotEmpty),
  ];

  Future<void> _loadCategories({
    bool force = false,
    bool cacheOnly = false,
  }) async {
    final generation = ++_categoryGeneration;
    final group = _group;
    setState(() {
      _categoriesLoading = true;
      _categoriesError = null;
    });
    final error = await _browser.loadCategories(
      group,
      force: force,
      cacheOnly: cacheOnly,
    );
    if (!mounted ||
        generation != _categoryGeneration ||
        group.id != _group.id) {
      return;
    }
    setState(() {
      _categoriesLoading = false;
      _categoriesError = error;
    });
    if (!_showRecommendations &&
        _category.isNotEmpty &&
        !_categories.any((entry) => entry.id == _category)) {
      _changeCategory('');
    }
  }

  Future<void> _refreshCatalog() async {
    if (widget.repository.supportsSourceManagement) {
      if (_group.sources.any((source) => _updater.busy(source.id))) return;
      _pauseCatalog();
      await _updater.update(_group.sources);
      return;
    }
    await _loadCategories(force: true);
    if (mounted) await _load(force: true);
  }

  void _updateChanged() {
    if (mounted) setState(() {});
  }

  /// 密碼鎖切換後可見站源會變，這裡把當前站源歸一化到仍然可見的站源。
  void _sourcesChanged() {
    if (!mounted) return;
    final visible = widget.store.sources;
    final signature = visible.map((site) => site.id).join(',');
    if (signature == _sourceSignature) return;
    final wasEmpty = _sourceSignature.isEmpty;
    _sourceSignature = signature;
    if (visible.isEmpty) {
      setState(() {
        _items = [];
        _hasMore = false;
        _loading = false;
        _loadingMore = false;
      });
      return;
    }
    final allowed = visible.map((site) => site.id).toSet();
    if (!wasEmpty &&
        allowed.contains(_source.id) &&
        _source.id == widget.store.source) {
      return;
    }
    _changeSource(SourceSite.byId(widget.store.source));
  }

  void _catalogUpdated(String source) {
    if (!mounted) return;
    _changedSources.add(source);
    _cacheRefreshTimer?.cancel();
    _watcherStartTimer?.cancel();
    _cacheRefreshTimer = Timer(const Duration(milliseconds: 100), () {
      unawaited(_reloadUpdatedCache());
    });
  }

  Future<void> _reloadUpdatedCache() async {
    if (_refreshingUpdatedCache) return;
    _refreshingUpdatedCache = true;
    final epoch = widget.store.profileEpoch;
    try {
      while (mounted &&
          _changedSources.isNotEmpty &&
          epoch == widget.store.profileEpoch) {
        final sources = Set.of(_changedSources);
        _changedSources.clear();
        final updates = <String, Drama>{};
        for (final source in sources) {
          if (!widget.store.allowsSource(source)) continue;
          try {
            final cached = await _browser.cached(source);
            for (final drama in cached.items) {
              if (widget.store.allowsSource(drama.source)) {
                updates[drama.id] = drama;
              }
            }
          } catch (error) {
            if (mounted && epoch == widget.store.profileEpoch) {
              setState(() => _error = '更新後讀取快取失敗：$error');
            }
          }
        }
        if (!mounted || epoch != widget.store.profileEpoch) return;
        // A background cache read can temporarily return no rows while a source
        // refresh is writing its catalog. Never replace visible data with that
        // empty snapshot; only merge and persist when there is actual data.
        if (updates.isNotEmpty) {
          _browser.updateDramas(updates.values);
          setState(() {
            _items = [
              for (final item in _items)
                updates[item.id] == null
                    ? item
                    : item.merge(updates[item.id]!),
            ];
          });
          await saveUserChange(
            context,
            () => widget.store.refreshDramas(updates.values),
          );
        }
        if (!mounted || epoch != widget.store.profileEpoch) return;
        if (_group.sources.any((source) => sources.contains(source.id))) {
          await Future.wait([
            _loadCategories(cacheOnly: true),
            if (updates.isNotEmpty &&
                !_loading &&
                !_loadingMore &&
                (!_onlineSearch || _search.text.trim().isEmpty))
              _load(cacheOnly: true),
          ]);
        }
      }
    } finally {
      _refreshingUpdatedCache = false;
    }
  }

  void _changeGroup(SourceGroup group) {
    if (_group.id != group.id) {
      _changeSource(group.sources.first, allSources: group.id == 'all');
    }
  }

  void _changeCategory(String category) {
    if (category == _recommendationCategory && _group.id == 'hongguo') {
      if (_showRecommendations) return;
      _pauseCatalog();
      setState(() {
        _showRecommendations = true;
        _search.clear();
        _searchVisible = false;
        _submittedQuery = '';
      });
      return;
    }
    if (_category == category && !_showRecommendations) return;
    _debounce?.cancel();
    setState(() {
      _showRecommendations = false;
          _categorySelections[_group.id] = category;
      if (_onlineSearch) {
        _search.clear();
        _submittedQuery = '';
      }
      _items = [];
      _hasMore = true;
      _error = null;
    });
    if (_scroll.hasClients) _scroll.jumpTo(0);
    _load(useCache: true);
  }

  void _swipeCategory(DragEndDetails details) {
    final velocity = details.primaryVelocity ?? 0;
    if (velocity.abs() < 240) return;
    final categories = _displayCategories;
    final index = categories.indexWhere(
      (entry) => entry.id == _displayCategory,
    );
    final next = index + (velocity < 0 ? 1 : -1);
    if (next >= 0 && next < categories.length) {
      _changeCategory(categories[next].id);
    }
  }

  void _toggleSearch() {
    if (AppLayout.isTelevision(context)) {
      _televisionSearch();
      return;
    }
    if (_showRecommendations) _changeCategory('');
    final hadQuery = _search.text.isNotEmpty;
    setState(() {
      _searchVisible = !_searchVisible;
      if (!_searchVisible) _search.clear();
    });
    if (!_searchVisible && hadQuery) _searchChanged('');
  }

  Future<PhoneSearchServer> _ensurePhoneSearchServer() {
    final existing = _phoneSearchServer;
    if (existing != null) return Future.value(existing);
    final starting = _phoneSearchServerStarting;
    if (starting != null) return starting;

    final future = PhoneSearchServer.start().then((server) {
      if (!mounted) {
        unawaited(server.close());
        throw StateError('D TV 已關閉');
      }
      _phoneSearchServer = server;
      _phoneSearchSubscription = server.queries.listen(_onPhoneSearchQuery);
      return server;
    }).whenComplete(() {
      _phoneSearchServerStarting = null;
    });
    _phoneSearchServerStarting = future;
    return future;
  }

  void _onPhoneSearchQuery(String query) {
    final normalized = query.trim();
    if (!mounted || normalized.isEmpty) return;
    _submitHeaderSearch(normalized);
    if (!mounted) return;
    ScaffoldMessenger.maybeOf(context)?.showSnackBar(
      SnackBar(
        content: Text('已收到手機搜尋：${uiTraditional(normalized)}'),
        duration: const Duration(seconds: 1),
      ),
    );
  }

  Future<void> _openPhoneSearch() async {
    _resetBackConfirmation();
    late final PhoneSearchServer server;
    try {
      server = await _ensurePhoneSearchServer();
    } catch (error) {
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        builder: (dialogContext) => AlertDialog(
          title: const Text('無法開啟手機搜尋'),
          content: Text(
            '請確認電視已連上 Wi-Fi 或有線網路，並讓手機連到同一個區域網路。\n\n$error',
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(dialogContext).pop(),
              child: const Text('知道了'),
            ),
          ],
        ),
      );
      _resetBackConfirmation(ignoreTrailingBack: true);
      return;
    }
    if (!mounted) return;
    await showPhoneSearchDialog(context, server: server);
    _resetBackConfirmation(ignoreTrailingBack: true);
  }

  void _openRankings() {
    _resetBackConfirmation();
    _pauseCatalog();
    Navigator.push<void>(
      context,
      MaterialPageRoute(
        builder: (_) => RankingsScreen(
          repository: widget.repository,
          store: widget.store,
          initialGroup: _group.id,
        ),
      ),
    ).whenComplete(() => _resetBackConfirmation(ignoreTrailingBack: true));
  }

  Future<void> _manageSources() async {
    _resetBackConfirmation();
    _pauseCatalog();
    await Navigator.push<void>(
      context,
      MaterialPageRoute(
        builder: (_) => SourcesScreen(
          repository: widget.repository,
          store: widget.store,
          initialSource: _source.id,
        ),
      ),
    );
    _resetBackConfirmation(ignoreTrailingBack: true);
    if (!mounted) return;
    await _loadCategories();
    if (mounted && (!_onlineSearch || _submittedQuery.isEmpty)) {
      await _load(useCache: true);
    }
  }

  void _submitHeaderSearch(String value) {
    final query = value.trim();
    if (query.isEmpty) return;
    if (_isRankingTab) {
      setState(() {
        _tab = AppLayout.isTelevision(context) ? 8 : 0;
        _showRecommendations = false;
        _categorySelections[_group.id] = '';
        _search.clear();
        _searchVisible = false;
        _submittedQuery = '';
        _error = null;
      });
    }
    _submitSearch(query);
  }

  Future<void> _televisionSearch() async {
    _resetBackConfirmation();
    final query = await showDialog<String>(
      context: context,
      builder: (_) => TelevisionSearchDialog(
        initialValue: _search.text,
        title: _group.id == 'all'
            ? '搜尋可用片源'
            : _onlineSearch
            ? '搜尋${uiTraditional(_group.name)}短劇'
            : '篩選目前已載入的短劇',
        recentSearches: widget.store.recentSearches,
        onCancel: () => unawaited(widget.repository.cancelSuggestions()),
        onPhoneSearch: () => unawaited(_openPhoneSearch()),
        suggestions: _searchSuggestions ? widget.repository.suggestions : null,
      ),
    );
    _resetBackConfirmation(ignoreTrailingBack: true);
    if (query != null && mounted) {
      _submitSearch(query);
    }
  }

  void _onAppNavigationChanged() {
    if (!mounted || !AppLayout.isTelevision(context)) return;
    final change = appNavigationObserver.changes.value;
    if (change == null) return;
    switch (change.event) {
      case AppNavigationEvent.pop:
      case AppNavigationEvent.remove:
      case AppNavigationEvent.replace:
        // A dialog or child page may close before the TV remote delivers its
        // trailing Back event. Swallow that event app-wide, not just for menus.
        _resetBackConfirmation(
          ignoreTrailingBack: true,
          trailingBackWindow: const Duration(milliseconds: 900),
        );
        break;
      case AppNavigationEvent.push:
        _resetBackConfirmation();
        break;
    }
  }

  void _resetBackConfirmation({
    bool ignoreTrailingBack = false,
    Duration trailingBackWindow = const Duration(milliseconds: 500),
  }) {
    _lastBackAt = null;
    _ignoreExitPromptUntil = ignoreTrailingBack
        ? DateTime.now().add(trailingBackWindow)
        : null;
    if (mounted) {
      ScaffoldMessenger.of(context).hideCurrentSnackBar();
    }
  }

  void _homePopupOpened() {
    _homePopupOpen = true;
    _resetBackConfirmation();
  }

  void _homePopupSelected() {
    _homePopupOpen = false;
    _resetBackConfirmation();
  }

  void _homePopupCanceled() {
    _homePopupOpen = false;
    // PopupMenuRoute can consume Back before the home PopScope receives the
    // same physical key. Suppress that trailing event so it cannot look like
    // the first press of the app-exit confirmation.
    _resetBackConfirmation(ignoreTrailingBack: true);
  }

  void _televisionBack() {
    final now = DateTime.now();

    // Android TV can report the same physical Back press through both the
    // platform pop callback and keyboard shortcuts. Handle that press once.
    final lastHandled = _lastHandledBackEventAt;
    if (lastHandled != null &&
        now.difference(lastHandled) < const Duration(milliseconds: 250)) {
      return;
    }
    _lastHandledBackEventAt = now;

    final routeIsCurrent = ModalRoute.of(context)?.isCurrent ?? true;

    // Menus are routes too. A Back press here belongs to the open menu, not
    // to the home screen's double-Back-to-exit behavior.
    if (_homePopupOpen) {
      _resetBackConfirmation(ignoreTrailingBack: true);
      if (!routeIsCurrent && Navigator.of(context).canPop()) {
        Navigator.of(context).pop();
      }
      return;
    }

    final ignoreUntil = _ignoreExitPromptUntil;
    if (_playbackRouteActive ||
        (ignoreUntil != null && now.isBefore(ignoreUntil))) {
      // A Back key that belongs to a nested route must not trigger the
      // home screen's "press again to exit" prompt.
      _lastBackAt = null;
      ScaffoldMessenger.of(context).hideCurrentSnackBar();
      // Some TV remotes deliver Back as a keyboard shortcut while focus
      // remains on the home route. Close the top playback route in that case.
      if (_playbackRouteActive &&
          !routeIsCurrent &&
          Navigator.of(context).canPop()) {
        Navigator.of(context).pop();
      }
      return;
    }

    // Settings, source management, dialogs and popup routes must close first.
    // Never show the app-exit prompt while the home route is not on top.
    if (!routeIsCurrent) {
      _lastBackAt = null;
      ScaffoldMessenger.of(context).hideCurrentSnackBar();
      if (Navigator.of(context).canPop()) {
        Navigator.of(context).pop();
      }
      return;
    }
    _ignoreExitPromptUntil = null;
    if (_search.text.isNotEmpty) {
      _search.clear();
      _searchChanged('');
      _lastBackAt = null;
      return;
    }

    final previous = _lastBackAt;
    if (previous != null &&
        now.difference(previous) <= const Duration(seconds: 3)) {
      _lastBackAt = null;
      ScaffoldMessenger.of(context).hideCurrentSnackBar();
      unawaited(SystemNavigator.pop());
      return;
    }

    // First Back press returns to Redfruit Hot and shows a SmartTube-style toast.
    if (_tab != 4) {
      _selectNavigationDestination(4);
      _navigationFocusNodes[4]?.requestFocus();
    }
    _lastBackAt = now;
    final messenger = ScaffoldMessenger.of(context);
    messenger
      ..hideCurrentSnackBar()
      ..showSnackBar(
        SnackBar(
          behavior: SnackBarBehavior.floating,
          width: (MediaQuery.sizeOf(context).width - 32)
              .clamp(260.0, 320.0)
              .toDouble(),
          duration: const Duration(seconds: 3),
          backgroundColor: const Color(0xEE171920),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(14),
            side: const BorderSide(color: Color(0x44FFFFFF)),
          ),
          content: const Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              _DtvBrandMark(size: 22),
              SizedBox(width: 10),
              Text(
                '再按一次返回鍵退出 D TV',
                style: TextStyle(
                  color: Colors.white,
                  fontSize: 15,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
          ),
        ),
      );
  }

  @override
  void initState() {
    super.initState();
    appNavigationObserver.changes.addListener(_onAppNavigationChanged);
    _scroll.addListener(_onCatalogScroll);
    _source = SourceSite.byId(widget.store.source);
    _allSources = widget.store.catalogView.allSources;
    _sourceSignature = widget.store.sources.map((site) => site.id).join(',');
    final savedCatalogView = widget.store.catalogView;
    if (savedCatalogView.sort != CatalogSort.date ||
        savedCatalogView.release.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted) return;
        unawaited(
          saveUserChange(
            context,
            () => widget.store.setCatalogView(
              savedCatalogView.copyWith(sort: CatalogSort.date, release: ''),
            ),
          ),
        );
      });
    }
    _browser = CatalogBrowser(widget.repository);
    _updater = LibraryUpdater(
      widget.repository,
      widget.store,
      onCatalogChanged: _catalogUpdated,
    )..addListener(_updateChanged);
    // Source-status polling scans every configured source. Let the first TV
    // frame and the active ranking settle before starting this background work.
    _watcherStartTimer = Timer(const Duration(seconds: 8), () {
      if (mounted) _updater.startWatching();
    });
    _followUpdater = FollowUpdater(widget.repository, widget.store)..start();
    widget.store.addListener(_sourcesChanged);
    widget.repository.catalogUpdates.addListener(_metadataChanged);
    if (widget.store.sources.isNotEmpty) {
      // A wide-screen device opens on Hot Rankings. Don't fetch/merge the
      // hidden catalog and categories concurrently with that first ranking.
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && _isCatalogTab) _ensureCatalogInitialized();
      });
    } else {
      _loading = false;
    }
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_initialTabResolved) return;
    _initialTabResolved = true;

    // Large-screen layouts open directly on the Redfruit Hot ranking.
    // Narrow/mobile layouts keep the catalog tab so bottom navigation remains valid.
    final wideHome = AppLayout.isTelevision(context) ||
        MediaQuery.sizeOf(context).width >= 840;
    if (wideHome) _tab = 4;
  }

  @override
  void dispose() {
    appNavigationObserver.changes.removeListener(_onAppNavigationChanged);
    _updater.removeListener(_updateChanged);
    _updater.dispose();
    _followUpdater.dispose();
    _cacheRefreshTimer?.cancel();
    widget.store.removeListener(_sourcesChanged);
    widget.repository.catalogUpdates.removeListener(_metadataChanged);
    _generation++;
    _categoryGeneration++;
    unawaited(_browser.cancel());
    unawaited(widget.repository.cancelSuggestions());
    _debounce?.cancel();
    unawaited(_phoneSearchSubscription?.cancel());
    _phoneSearchSubscription = null;
    final phoneSearchServer = _phoneSearchServer;
    _phoneSearchServer = null;
    if (phoneSearchServer != null) unawaited(phoneSearchServer.close());
    _search.dispose();
    _scroll.removeListener(_onCatalogScroll);
    _scroll.dispose();
    for (final node in _navigationFocusNodes.values) {
      node.dispose();
    }
    _navigationFocusNodes.clear();
    _savedSearchFocusNode.dispose();
    super.dispose();
  }

  bool get _canLoadMore => _hasMore && !_loading && !_loadingMore;

  void _onCatalogScroll() {
    if (!mounted ||
        _showRecommendations ||
        !(_hiddenItems || _canLoadMore) ||
        !_scroll.hasClients) {
      return;
    }
    final position = _scroll.position;
    final threshold = (position.viewportDimension * 1.5).clamp(320.0, 900.0);
    if (position.extentAfter > threshold || _catalogLoadScheduled) return;
    _catalogLoadScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _catalogLoadScheduled = false;
      if (!mounted || _showRecommendations || !_scroll.hasClients) return;
      if (_hiddenItems || _canLoadMore) _showMore();
    });
  }

  void _showMore() {
    if (_hiddenItems) {
      setState(() => _revealed += _revealBatch);
    } else {
      unawaited(_load(more: true));
    }
  }

  List<Drama> _reveal(List<Drama> items) {
    final view = widget.store.catalogView;
    final key = Object.hash(
      _group.id,
      _category,
      _submittedQuery,
      _search.text.trim(),
      view.sort,
      view.release,
      _hideVip,
    );
    if (key != _revealKey) {
      _revealKey = key;
      _revealed = _revealBatch;
    }
    _hiddenItems = items.length > _revealed;
    return _hiddenItems ? items.sublist(0, _revealed) : items;
  }

  void _metadataChanged() {
    final drama = widget.repository.catalogUpdates.latest;
    if (!mounted || drama == null || !widget.store.allowsSource(drama.source)) {
      return;
    }
    _browser.updateDrama(drama);
    setState(() {
      _items = [
        for (final item in _items)
          item.id == drama.id ? item.merge(drama) : item,
      ];
    });
    unawaited(saveUserChange(context, () => widget.store.refreshDrama(drama)));
  }

  Future<void> _load({
    bool more = false,
    bool useCache = false,
    bool force = false,
    bool cacheOnly = false,
  }) async {
    if (_showRecommendations) return;
    if (more && (_loading || _loadingMore || !_hasMore)) return;
    final generation = ++_generation;
    final group = _group;
    final query = _onlineSearch ? _search.text.trim() : '';
    setState(() {
      _error = null;
      if (query.isNotEmpty) _categorySelections[group.id] = '';
      if (more) {
        _loadingMore = true;
      } else {
        _loading = true;
        _loadingMore = false;
        // Background cache-only refreshes must not clear the visible list
        // just because the query differs from the last completed request.
        if (!cacheOnly && query != _submittedQuery) _items = [];
      }
    });
    CatalogPage? refreshed;
    void accept(CatalogPage result, {bool cached = false}) {
      if (!mounted || generation != _generation) return;
      final preserveVisibleCatalog =
          cacheOnly && result.items.isEmpty && _items.isNotEmpty;
      setState(() {
        if (!preserveVisibleCatalog) _items = result.items;
        if (!preserveVisibleCatalog &&
            more &&
            result.items.length > _revealed) {
          _revealed = result.items.length;
        }
        if (!preserveVisibleCatalog) {
          _hasMore = result.hasMore;
          _submittedQuery = query;
        }
        _loading = preserveVisibleCatalog ? false : cached && !result.fresh;
        _loadingMore = false;
        _error = preserveVisibleCatalog
            ? null
            : result.warning.isEmpty
            ? null
            : result.warning;
      });
      if (identical(result, refreshed)) return;
      refreshed = result;
      // A cache-only refresh is advisory. An empty result means "no usable
      // cached snapshot right now", not "the user's catalog is empty".
      if (cacheOnly && result.items.isEmpty) return;
      unawaited(
        saveUserChange(context, () => widget.store.refreshDramas(result.items)),
      );
    }

    try {
      final result = await _browser.load(
        group,
        category: _category,
        query: query,
        more: more,
        useCache: useCache,
        force: force,
        cacheOnly: cacheOnly,
        onCached: (result) => accept(result, cached: true),
      );
      accept(result);
    } catch (error) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _loading = false;
        _loadingMore = false;
        _error = error.toString();
      });
    }
  }

  void _changeSource(SourceSite source, {bool allSources = false}) {
    if (_source.id == source.id && _allSources == allSources) {
      return;
    }
    _debounce?.cancel();
    _search.clear();
    setState(() {
      _showRecommendations = false;
      _source = source;
      _allSources = allSources;
      _items = [];
      _hasMore = true;
      _submittedQuery = '';
      _error = null;
    });
    unawaited(
      saveUserChange(
        context,
        () => widget.store.setCatalogSource(source.id, allSources: allSources),
      ),
    );
    if (_scroll.hasClients) {
      _scroll.jumpTo(0);
    }
    _load(useCache: true);
    _loadCategories();
  }

  void _searchChanged(String query) {
    _debounce?.cancel();
    if (_onlineSearch && (_loading || _loadingMore)) {
      _generation++;
      unawaited(_browser.cancel());
      _loading = _loadingMore = false;
    }
    setState(() {});
    final normalized = query.trim();
    if (normalized.isEmpty) {
      if (_onlineSearch) {
        _debounce = Timer(const Duration(milliseconds: 300), () => _load());
      }
      return;
    }
    if (normalized.length > 100) return;
    _debounce = Timer(const Duration(milliseconds: 500), () {
      if (!mounted ||
          _search.text.trim() != normalized ||
          !_search.value.composing.isCollapsed ||
          (normalized == _submittedQuery && !_loading && !_loadingMore)) {
        return;
      }
      _submitHeaderSearch(normalized);
    });
  }

  void _submitSearch(String query) {
    if (_showRecommendations) {
      _showRecommendations = false;
      _categorySelections[_group.id] = '';
    }
    _search.text = query.trim();
    _debounce?.cancel();
    if (_search.text.isNotEmpty) {
      unawaited(
        saveUserChange(
          context,
          () => widget.store.rememberSearch(_search.text),
        ),
      );
    }
    if (_onlineSearch) {
      _load();
    } else {
      setState(() {});
    }
  }

  void _openDrama(Drama drama, {bool resume = false}) {
    _pauseCatalog();
    CoverRequestScheduler.pauseBackground();
    _playbackRouteActive = true;
    _lastBackAt = null;
    _ignoreExitPromptUntil = null;
    ScaffoldMessenger.of(context).hideCurrentSnackBar();
    // 所有海報統一一鍵播放；openPlaybackDirectly 會依觀看記錄選擇續播集數與進度。
    unawaited(
      openPlaybackDirectly(
        context,
        drama: drama,
        repository: widget.repository,
        store: widget.store,
      ).whenComplete(() {
        _playbackRouteActive = false;
        _lastBackAt = null;
        // Swallow the tail end of the same physical Back press after the
        // player route closes, so it cannot become an app-exit confirmation.
        _ignoreExitPromptUntil =
            DateTime.now().add(const Duration(milliseconds: 500));
        if (mounted) {
          ScaffoldMessenger.of(context).hideCurrentSnackBar();
        }
        CoverRequestScheduler.resumeBackground();
      }),
    );
  }

  void _ensureCatalogInitialized() {
    if (_catalogInitialized || !mounted || widget.store.sources.isEmpty) return;
    _catalogInitialized = true;
    unawaited(_load(useCache: true));
    unawaited(_loadCategories());
  }

  void _changeTab(int tab) {
    setState(() => _tab = tab);
    if (_isCatalogTab) _ensureCatalogInitialized();
  }

  void _dramaActions(Drama drama) => showDramaActions(
    context,
    drama: drama,
    store: widget.store,
    onContinue: () => _openDrama(drama, resume: true),
  );

  Widget _catalogTile(
    Drama drama, {
    FocusNode? focusNode,
    VoidCallback? onFocus,
  }) {
    final following = widget.store.following(drama.id);
    return DramaTile(
      key: ValueKey(drama.id),
      drama: drama,
      repository: widget.repository,
      focusNode: focusNode,
      onFocus: onFocus,
      onTap: () => _openDrama(drama),
      onMore: () => _dramaActions(drama),
      badge: following == null
          ? null
          : '${following.status.label}${following.hasUpdates ? ' · ${following.updateLabel}' : ''}',
    );
  }

  void _pauseCatalog() {
    _debounce?.cancel();
    _generation++;
    unawaited(_browser.cancel());
    unawaited(widget.repository.cancelSuggestions());
    setState(() {
      _loading = false;
      _loadingMore = false;
    });
  }

  bool get _supportsVipFilter =>
      _group.sources.any((source) => source.id == 'huangdou');
  bool get _hideVip => _supportsVipFilter && widget.store.hideVip;

  List<Drama> get _visible {
    final query = _search.text.trim().toLowerCase();
    final category = _category;
    final hideVip = _hideVip;
    final onlineSearch = _onlineSearch;
    final groupSources = _group.sources
        .map((source) => source.id)
        .join(',');
    final cacheKey = Object.hash(
      _group.id,
      groupSources,
      _sourceSignature,
      category,
      query,
      hideVip,
      onlineSearch,
      widget.store.profileEpoch,
    );
    if (identical(_visibleCacheItems, _items) &&
        _visibleCacheKey == cacheKey) {
      return _visibleCache;
    }

    final view = widget.store.catalogView.copyWith(
      sort: CatalogSort.date,
      release: '',
    );
    final result = sortCatalog(
      _items.where((drama) {
        if (!widget.store.allowsSource(drama.source)) return false;
        if (category.startsWith('local:') &&
            categoryName(drama.category) != category.substring(6)) {
          return false;
        }
        if (hideVip && drama.source == 'huangdou' && drama.vip) {
          return false;
        }
        return onlineSearch || query.isEmpty || matchesDramaQuery(drama, query);
      }),
      view,
    );
    _visibleCacheItems = _items;
    _visibleCacheKey = cacheKey;
    _visibleCache = result;
    return result;
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.store,
    builder: (context, _) => LayoutBuilder(
      builder: (context, constraints) {
        final television = AppLayout.isTelevision(context);
        final desktop = constraints.maxWidth >= 840;
        final compactBrandBar =
            television || (Platform.isAndroid && desktop);
        final wide = television || desktop;
        final bannerWidth = (constraints.maxWidth -
                (constraints.maxWidth >= 1450 ? 600.0 : 530.0))
            .clamp(300.0, 980.0)
            .toDouble();
        final bannerHeight = _brandBannerHeight(constraints.maxWidth);
        final scaffold = Scaffold(
          appBar: AppBar(
            toolbarHeight: compactBrandBar
                ? 86
                : wide
                ? _brandBannerHeight(constraints.maxWidth) + 12
                : null,
            titleSpacing: 12,
            title: compactBrandBar
                ? _compactBrandHeader(width: constraints.maxWidth - 220)
                : wide
                ? _homeBrandBanner(
                    width: bannerWidth,
                    height: bannerHeight,
                  )
                : (
                    _tab == 0 && widget.store.sources.isNotEmpty
                                    ? PopupMenuButton<SourceGroup>(
                                        key: const ValueKey('source-switch'),
                                        tooltip: '切換片源',
                                        enabled: _sourceGroups.length > 1,
                                        onOpened: _homePopupOpened,
                                        onCanceled: _homePopupCanceled,
                                        onSelected: (group) {
                                          _homePopupSelected();
                                          _changeGroup(group);
                                        },
                                        itemBuilder: (_) => [
                                          for (final group in _sourceGroups)
                                            PopupMenuItem(
                                              value: group,
                                              child: Row(
                                                children: [
                                                  Expanded(child: Text(uiTraditional(group.name))),
                                                  if (group.id == _group.id)
                                                    const Icon(Icons.check_rounded, size: 20),
                                                ],
                                              ),
                                            ),
                                        ],
                                        child: SizedBox(
                                          height: 48,
                                          child: Row(
                                            mainAxisSize: MainAxisSize.min,
                                            children: [
                                              _brandLogo(width: 126, height: 46),
                                              const SizedBox(width: 8),
                                              Flexible(
                                                child: Text(
                                                  uiTraditional(_group.name),
                                                  overflow: TextOverflow.ellipsis,
                                                  style: const TextStyle(
                                                    fontSize: 13,
                                                    fontWeight: FontWeight.w600,
                                                  ),
                                                ),
                                              ),
                                              if (_sourceGroups.length > 1)
                                                const Icon(Icons.expand_more_rounded),
                                            ],
                                          ),
                                        ),
                                      )
                                    : _brandTitle()
                  ),
            actions: [
                if (wide && (_isCatalogTab || _isRankingTab)) ...[
                  SizedBox(
                    width: constraints.maxWidth >= 1450 ? 340 : 260,
                    height: 48,
                    child: television
                        ? RemoteTarget(
                            key: const ValueKey('tv-search-trigger'),
                            label: '搜尋短劇',
                            onPressed: _televisionSearch,
                            padding: EdgeInsets.zero,
                            radius: 14,
                            borderWidth: 2,
                            outlined: true,
                            child: Container(
                              height: 48,
                              padding: const EdgeInsets.symmetric(horizontal: 14),
                              decoration: BoxDecoration(
                                color: Theme.of(context).colorScheme.surfaceContainerHigh,
                                borderRadius: BorderRadius.circular(14),
                              ),
                              child: Row(
                                children: [
                                  const Icon(Icons.search_rounded),
                                  const SizedBox(width: 10),
                                  Text(
                                    '搜尋短劇',
                                    style: TextStyle(
                                      fontSize: 16,
                                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                                    ),
                                  ),
                                ],
                              ),
                            ),
                          )
                        : TextField(
                            controller: _search,
                            textInputAction: TextInputAction.search,
                            onChanged: _searchChanged,
                            onSubmitted: _submitHeaderSearch,
                            decoration: InputDecoration(
                              hintText: '搜尋短劇',
                              prefixIcon: const Icon(Icons.search_rounded),
                              isDense: true,
                              filled: true,
                              fillColor: Theme.of(context).colorScheme.surfaceContainerHigh,
                              contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
                              border: OutlineInputBorder(
                                borderRadius: BorderRadius.circular(14),
                                borderSide: BorderSide.none,
                              ),
                              enabledBorder: OutlineInputBorder(
                                borderRadius: BorderRadius.circular(14),
                                borderSide: BorderSide.none,
                              ),
                              focusedBorder: OutlineInputBorder(
                                borderRadius: BorderRadius.circular(14),
                                borderSide: BorderSide(
                                  color: Theme.of(context).colorScheme.primary,
                                  width: 1.5,
                                ),
                              ),
                            ),
                          ),
                  ),
                  const SizedBox(width: 8),
                  SizedBox(
                    height: 48,
                    child: FilledButton.tonalIcon(
                      onPressed: _openPhoneSearch,
                      icon: const Icon(Icons.qr_code_2_rounded),
                      label: const Text('手機掃碼'),
                      style: FilledButton.styleFrom(
                        minimumSize: const Size(0, 48),
                        padding: const EdgeInsets.symmetric(horizontal: 16),
                        shape: RoundedRectangleBorder(
                          borderRadius: BorderRadius.circular(14),
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                ],
                if (_tab == 1)
                  IconButton(
                    key: const ValueKey('follow-lan-sync'),
                    tooltip: '收藏同步',
                    onPressed: () {
                      _resetBackConfirmation();
                      openLanSync(
                        context,
                        onReturn: () => _resetBackConfirmation(
                          ignoreTrailingBack: true,
                        ),
                      );
                    },
                    icon: const Icon(Icons.sync_rounded),
                  ),
                if (_isCatalogTab) ...[
                  if (!television && !desktop)
                    IconButton(
                      key: const ValueKey('open-rankings'),
                      tooltip: '熱播榜單',
                      onPressed: widget.store.sources.isEmpty
                          ? null
                          : _openRankings,
                      icon: const Icon(Icons.leaderboard_outlined),
                    ),
                  if (!wide)
                    IconButton(
                    key: const ValueKey('toggle-search'),
                    tooltip: _searchVisible ? '收起搜尋' : '搜尋',
                    icon: Icon(
                      _searchVisible
                          ? Icons.search_off_rounded
                          : Icons.search_rounded,
                    ),
                    onPressed: widget.store.sources.isEmpty
                        ? null
                        : _toggleSearch,
                  ),
                  if (television && !wide)
                    IconButton(
                      key: const ValueKey('phone-qr-search'),
                      tooltip: '手機掃描 QR Code',
                      icon: const Icon(Icons.qr_code_2_rounded),
                      onPressed: widget.store.sources.isEmpty
                          ? null
                          : _openPhoneSearch,
                    ),
                ],
                if (_isCatalogTab &&
                    !_showRecommendations &&
                    constraints.maxWidth >= 400 &&
                    !wide)
                  RefreshAction(
                    key: const ValueKey('catalog-refresh'),
                    loading:
                        _loading ||
                        _loadingMore ||
                        _categoriesLoading ||
                        _group.sources.any(
                          (source) => _updater.busy(source.id),
                        ),
                    tooltip: '更新劇庫',
                    onPressed: widget.store.sources.isEmpty
                        ? null
                        : _refreshCatalog,
                  ),
                PopupMenuButton<String>(
                  tooltip: '更多',
                  onOpened: _homePopupOpened,
                  onCanceled: _homePopupCanceled,
                  onSelected: (value) {
                    _homePopupSelected();
                    if (value == 'update') {
                      _refreshCatalog();
                    } else if (value == 'sources') {
                      _manageSources();
                    } else if (value == 'settings') {
                      Navigator.push<void>(
                        context,
                        MaterialPageRoute<void>(
                          builder: (_) => SettingsScreen(
                            repository: widget.repository,
                            store: widget.store,
                          ),
                        ),
                      ).whenComplete(
                        () => _resetBackConfirmation(ignoreTrailingBack: true),
                      );
                    } else if (value == 'users') {
                      Navigator.push<void>(
                        context,
                        MaterialPageRoute<void>(
                          builder: (_) => ProfilesScreen(store: widget.store),
                        ),
                      ).whenComplete(
                        () => _resetBackConfirmation(ignoreTrailingBack: true),
                      );
                    } else if (value == 'about') {
                      // showAboutDialog returns void (not a Future).
                      // The parent Back handler already ignores the underlying
                      // home route while this dialog is on top.
                      showAboutDialog(
                        context: context,
                        applicationName: appName,
                        applicationVersion: AppLayout.versionOf(context),
                        applicationIcon: const _DtvBrandMark(size: 56),
                        children: const [Text('作者：R15')],
                      );
                    }
                  },
                  itemBuilder: (_) => [
                    if (_isCatalogTab &&
                        !_showRecommendations &&
                        constraints.maxWidth < 400)
                      PopupMenuItem(
                        value: 'update',
                        enabled:
                            widget.store.sources.isNotEmpty &&
                            !_group.sources.any(
                              (source) => _updater.busy(source.id),
                            ),
                        child: const Text('更新劇庫'),
                      ),
                    if (widget.repository.supportsSourceManagement)
                      const PopupMenuItem(
                        value: 'sources',
                        child: Text('片源管理'),
                      ),
                    const PopupMenuItem(value: 'users', child: Text('使用者管理')),
                    const PopupMenuItem(
                      value: 'settings',
                      child: Text('設定與備份'),
                    ),
                    const PopupMenuItem(
                      value: 'about',
                      child: Text('關於$appName'),
                    ),
                  ],
                ),
              const SizedBox(width: 8),
            ],
          ),
          body: SafeArea(
            top: false,
            child: Row(
              children: [
                if (desktop || television) ...[
                  _sideNavigation(),
                  const VerticalDivider(width: 1, thickness: 1),
                ],
                Expanded(
                  child: _isCatalogTab
                      ? widget.store.sources.isEmpty
                            ? const StatusPanel(
                                title: '暫無可用片源',
                                message: '請到片源管理開啟片源，或請管理員確認使用權限。',
                              )
                            : _catalog()
                      : _isRankingTab
                      ? RankingsScreen(
                          key: ValueKey('tv-ranking-$_tab'),
                          repository: widget.repository,
                          store: widget.store,
                          initialGroup: 'hongguo',
                          initialBoardId: _rankingBoardId,
                          embedded: true,
                          onExitLeft: _focusCurrentNavigation,
                        )
                      : _tab == 9
                      ? SettingsScreen(
                          repository: widget.repository,
                          store: widget.store,
                          embedded: true,
                        )
                      : SavedLibrary(
                          key: ValueKey('saved-tab-$_tab'),
                          repository: widget.repository,
                          store: widget.store,
                          history: _tab == 2,
                          onOpen: _openDrama,
                          onContinue: (drama) =>
                              _openDrama(drama, resume: true),
                          gridFocusController: _savedGridFocus,
                          searchFocusNode: _savedSearchFocusNode,
                          onExitLeft: _focusCurrentNavigation,
                        ),
                ),
              ],
            ),
          ),
          bottomNavigationBar: desktop || television
              ? null
              : AppBottomNavigation(
                  selectedIndex: _tab,
                  onDestinationSelected: _changeTab,
                  destinations: [
                    NavigationDestination(
                      icon: Icon(Icons.explore_outlined),
                      selectedIcon: Icon(Icons.explore),
                      label: '分類',
                    ),
                    NavigationDestination(
                      icon: Icon(Icons.bookmark_border_rounded),
                      selectedIcon: Icon(Icons.bookmark_rounded),
                      label: '收藏',
                    ),
                    NavigationDestination(
                      icon: Icon(Icons.history_rounded),
                      label: '最近觀看',
                    ),
                  ],
                ),
        );
        if (!television) return scaffold;
        return PopScope(
          canPop: !television,
          onPopInvokedWithResult: (didPop, result) {
            if (!didPop) _televisionBack();
          },
          child: CallbackShortcuts(
            bindings: {
              const SingleActivator(LogicalKeyboardKey.escape): _televisionBack,
              const SingleActivator(LogicalKeyboardKey.goBack): _televisionBack,
            },
            child: scaffold,
          ),
        );
      },
    ),
  );

  Widget _brandLogo({required double width, required double height}) {
    final theme = Theme.of(context);
    return SizedBox(
      width: width,
      height: height,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          _DtvBrandMark(size: height * 0.92),
          const SizedBox(width: 8),
          Flexible(
            child: Text(
              'D TV',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                color: theme.colorScheme.onSurface,
                fontSize: height * .52,
                fontWeight: FontWeight.w900,
                letterSpacing: -0.7,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _brandTitle() => _brandLogo(width: 145, height: 52);

  /// Larger, balanced brand treatment for Android TV and the Android emulator.
  /// Uses the full title area without obscuring the first row of posters.
  Widget _compactBrandHeader({required double width}) {
    final theme = Theme.of(context);
    return SizedBox(
      width: width.clamp(220.0, 460.0).toDouble(),
      height: 72,
      child: Row(
        children: [
          const _DtvBrandMark(size: 58),
          const SizedBox(width: 14),
          Flexible(
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  'D TV',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    color: theme.colorScheme.onSurface,
                    fontSize: 30,
                    height: 1,
                    fontWeight: FontWeight.w900,
                    letterSpacing: -0.7,
                  ),
                ),
                const SizedBox(height: 7),
                Row(
                  crossAxisAlignment: CrossAxisAlignment.center,
                  children: [
                    const _DtvBrandMark(size: 14),
                    const SizedBox(width: 5),
                    Expanded(
                      child: Text(
                        '短劇隨選・精彩立即播放',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          color: theme.colorScheme.onSurfaceVariant,
                          fontSize: 13,
                          height: 1,
                          fontWeight: FontWeight.w600,
                          letterSpacing: .5,
                        ),
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  /// Persistent brand header shared by the TV / desktop views.
  /// The slogan is redrawn larger over the original banner so it remains
  /// readable at living-room viewing distance.
  double _brandBannerHeight(double width) {
    final bannerWidth = (width - (width >= 1450 ? 600.0 : 530.0))
        .clamp(300.0, 980.0)
        .toDouble();
    return (bannerWidth / (960 / 180)).clamp(100.0, 180.0).toDouble();
  }

  /// Desktop-only brand banner. Android TV uses a compact top bar so posters
  /// remain visible without a large hero panel.
  Widget _homeBrandBanner({required double width, required double height}) {
    final sloganSize = (height * .19).clamp(18.0, 34.0).toDouble();
    return Container(
      width: width,
      height: height,
      padding: const EdgeInsets.all(1),
      decoration: BoxDecoration(
        color: const Color(0xFF080910),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: const Color(0x66FF3658), width: 1),
        boxShadow: const [
          BoxShadow(
            color: Color(0x22F52245),
            blurRadius: 12,
            spreadRadius: 1,
          ),
        ],
      ),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(11),
        child: Stack(
          fit: StackFit.expand,
          children: [
            Image.asset(
              'assets/brand/dtv-home-header.webp',
              fit: BoxFit.fill,
              filterQuality: FilterQuality.high,
              errorBuilder: (_, error, stack) => Container(
                color: const Color(0xFF080910),
                alignment: Alignment.centerLeft,
                padding: const EdgeInsets.symmetric(horizontal: 24),
                child: _brandLogo(width: width * .42, height: height * .78),
              ),
            ),
            // Mask the small slogan baked into the image and redraw it larger
            // for Google TV viewing distance. Keep the DTV logo and portrait.
            Positioned(
              left: 0,
              bottom: 0,
              width: width * .78,
              height: height * .35,
              child: Container(
                alignment: Alignment.bottomLeft,
                padding: EdgeInsets.only(
                  left: width * .025,
                  right: width * .025,
                  bottom: height * .025,
                  top: height * .025,
                ),
                decoration: const BoxDecoration(
                  gradient: LinearGradient(
                    begin: Alignment.centerLeft,
                    end: Alignment.centerRight,
                    colors: [
                      Color(0xF5080910),
                      Color(0xE8080910),
                      Color(0x00080910),
                    ],
                    stops: [0, .68, 1],
                  ),
                ),
                child: Text(
                  '精彩隨時　大屏更過癮',
                  maxLines: 1,
                  softWrap: false,
                  overflow: TextOverflow.visible,
                  style: TextStyle(
                    color: Colors.white,
                    fontSize: sloganSize,
                    fontWeight: FontWeight.w900,
                    height: 1,
                    letterSpacing: .5,
                    shadows: const [
                      Shadow(
                        color: Color(0xFFFF243F),
                        blurRadius: 10,
                      ),
                      Shadow(
                        color: Color(0xFF000000),
                        blurRadius: 4,
                        offset: Offset(1, 2),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  void _selectCatalogTab(int tab) {
    final shouldReload =
        _category.isNotEmpty ||
        _showRecommendations ||
        _search.text.trim().isNotEmpty ||
        _submittedQuery.isNotEmpty;
    final wasInitialized = _catalogInitialized;
    _debounce?.cancel();
    setState(() {
      _tab = tab;
      _showRecommendations = false;
      _search.clear();
      _searchVisible = false;
      _submittedQuery = '';
      _categorySelections[_group.id] = '';
      _error = null;
    });
    if (_scroll.hasClients) _scroll.jumpTo(0);
    if (!wasInitialized) {
      _ensureCatalogInitialized();
    } else if (shouldReload) {
      _load(useCache: true);
    }
  }

  void _focusCurrentNavigation() {
    _navigationFocusNodes[_tab]?.requestFocus();
  }

  void _selectNavigationDestination(int tab) {
    if (tab == 0 || tab == 8) {
      _selectCatalogTab(tab);
      return;
    }
    _pauseCatalog();
    setState(() {
      _tab = tab;
    });
  }

  Widget _sideNavigation() {
    const tvEntries = <({int tab, IconData icon, String label})>[
      (tab: 2, icon: Icons.history_rounded, label: '最近觀看'),
      (tab: 1, icon: Icons.bookmark_border_rounded, label: '我的收藏'),
      (tab: 4, icon: Icons.local_fire_department_rounded, label: '紅果熱播榜'),
      (tab: 5, icon: Icons.people_alt_rounded, label: '真人熱播榜'),
      (tab: 6, icon: Icons.smart_toy_rounded, label: 'AI熱播榜'),
      (tab: 7, icon: Icons.auto_stories_rounded, label: '漫劇熱播榜'),
      (tab: 8, icon: Icons.category_rounded, label: '分類'),
      (tab: 9, icon: Icons.settings_rounded, label: '設定'),
    ];
    const desktopEntries = <({int tab, IconData icon, String label})>[
      (tab: 2, icon: Icons.history_rounded, label: '最近觀看'),
      (tab: 1, icon: Icons.bookmark_border_rounded, label: '我的收藏'),
      (tab: 4, icon: Icons.local_fire_department_rounded, label: '紅果熱播榜'),
      (tab: 5, icon: Icons.people_alt_rounded, label: '真人熱播榜'),
      (tab: 6, icon: Icons.smart_toy_rounded, label: 'AI熱播榜'),
      (tab: 7, icon: Icons.auto_stories_rounded, label: '漫劇熱播榜'),
      (tab: 0, icon: Icons.category_rounded, label: '分類'),
      (tab: 9, icon: Icons.settings_rounded, label: '設定'),
    ];
    final television = AppLayout.isTelevision(context);
    final entries = television ? tvEntries : desktopEntries;
    final theme = Theme.of(context);

    KeyEventResult handleNavigationKey(FocusNode node, KeyEvent event) {
      if (!television ||
          (event is! KeyDownEvent && event is! KeyRepeatEvent)) {
        return KeyEventResult.ignored;
      }
      final activeIndex = entries.indexWhere(
        (entry) => _navigationFocusNodes[entry.tab]?.hasPrimaryFocus ?? false,
      );
      if (activeIndex < 0) return KeyEventResult.ignored;
      final key = event.logicalKey;
      if (key == LogicalKeyboardKey.arrowUp || key == LogicalKeyboardKey.arrowDown) {
        final step = key == LogicalKeyboardKey.arrowDown ? 1 : -1;
        final nextIndex = (activeIndex + step).clamp(0, entries.length - 1);
        if (nextIndex != activeIndex) {
          final nextEntry = entries[nextIndex];
          _navigationFocusNodes[nextEntry.tab]?.requestFocus();
          _selectNavigationDestination(nextEntry.tab);
        }
        return KeyEventResult.handled;
      }
      if (key == LogicalKeyboardKey.arrowRight) {
        if (_tab == 1 || _tab == 2) {
          // Enter the first poster directly; only use search when there are no matches.
          if (_savedGridFocus.hasItems) {
            _savedGridFocus.focusFirst();
          } else {
            _savedSearchFocusNode.requestFocus();
          }
          return KeyEventResult.handled;
        }
        FocusScope.of(context).focusInDirection(TraversalDirection.right);
        return KeyEventResult.handled;
      }
      if (key == LogicalKeyboardKey.arrowLeft) return KeyEventResult.handled;
      return KeyEventResult.ignored;
    }

    return Focus(
      canRequestFocus: false,
      skipTraversal: true,
      onKeyEvent: handleNavigationKey,
      child: SizedBox(
        width: television ? 176 : 148,
        child: SingleChildScrollView(
          padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 8),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (final entry in entries)
                Padding(
                  padding: const EdgeInsets.only(bottom: 5),
                  child: RemoteTarget(
                    key: ValueKey('side-nav-${entry.tab}'),
                    focusNode: _navigationFocusNodes.putIfAbsent(
                      entry.tab,
                      () => FocusNode(debugLabel: 'D TV navigation ${entry.tab}'),
                    ),
                    autofocus: television && _tab == entry.tab,
                    selected: _tab == entry.tab,
                    // On real Android TV, keep focused navigation entries away
                    // from the top brand area when returning from the bottom.
                    scrollAlignment: television ? 0.32 : 0,
                    scrollAlignmentPolicy: television
                        ? ScrollPositionAlignmentPolicy.explicit
                        : null,
                    onFocus: () {
                      if (_tab != entry.tab) {
                        _selectNavigationDestination(entry.tab);
                      }
                    },
                    onPressed: () {
                      _navigationFocusNodes[entry.tab]?.requestFocus();
                      _selectNavigationDestination(entry.tab);
                    },
                    label: entry.label,
                    padding: EdgeInsets.symmetric(
                      horizontal: television ? 7 : 5,
                      vertical: 7,
                    ),
                    radius: 12,
                    borderWidth: 2,
                    outlined: true,
                    child: SizedBox(
                      height: television ? 62 : 55,
                      child: Column(
                        mainAxisAlignment: MainAxisAlignment.center,
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Icon(
                            entry.icon,
                            size: television ? 24 : 21,
                            color: _tab == entry.tab
                                ? theme.colorScheme.primary
                                : theme.colorScheme.onSurfaceVariant,
                          ),
                          const SizedBox(height: 3),
                          Text(
                            entry.label,
                            textAlign: TextAlign.center,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                              fontSize: television ? 16 : 11.5,
                              height: 1.12,
                              fontWeight: _tab == entry.tab
                                  ? FontWeight.w800
                                  : FontWeight.w500,
                              color: _tab == entry.tab
                                  ? theme.colorScheme.onPrimaryContainer
                                  : theme.colorScheme.onSurface,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _catalog() {
    final items = _reveal(_visible);
    final television = AppLayout.isTelevision(context);
    final desktopWide = !television && MediaQuery.sizeOf(context).width >= 840;
    return Column(
      children: [
        if (_searchVisible && !television)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 4, 16, 4),
            child: SearchInput(
              key: ValueKey('search-${_group.id}'),
              controller: _search,
              autofocus: true,
              hint: _searchHint,
              suggestions: _searchSuggestions
                  ? widget.repository.suggestions
                  : null,
              onChanged: _searchChanged,
              onCancel: () => unawaited(widget.repository.cancelSuggestions()),
              onSearch: _submitSearch,
            ),
          ),
        if (_searchVisible &&
            _search.text.trim().isEmpty &&
            widget.store.recentSearches.isNotEmpty)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: Row(
              children: [
                Expanded(
                  child: SingleChildScrollView(
                    scrollDirection: Axis.horizontal,
                    child: Row(
                      children: [
                        for (final query in widget.store.recentSearches)
                          Padding(
                            padding: const EdgeInsets.only(right: 8),
                            child: ActionChip(
                              avatar: const Icon(
                                Icons.history_rounded,
                                size: 16,
                              ),
                              label: Text(uiTraditional(query)),
                              onPressed: () => _submitSearch(query),
                            ),
                          ),
                      ],
                    ),
                  ),
                ),
                IconButton(
                  tooltip: '清空最近搜尋',
                  onPressed: () =>
                      saveUserChange(context, widget.store.clearRecentSearches),
                  icon: const Icon(Icons.delete_outline_rounded, size: 20),
                ),
              ],
            ),
          ),
        if (!television || _tab == 8)
          CatalogFilters(
            key: ValueKey('filters-${_group.id}'),
            categories: _displayCategories,
            category: _displayCategory,
            error: _categoriesError == null ? null : uiTraditional(_categoriesError!),
            onCategory: _changeCategory,
            onRetry: () => _loadCategories(force: true),
            trailing: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (_supportsVipFilter)
                  IconButton(
                    tooltip: widget.store.hideVip ? 'VIP：隱藏' : 'VIP：顯示',
                    onPressed: () => saveUserChange(
                      context,
                      () => widget.store.setHideVip(!widget.store.hideVip),
                    ),
                    icon: VipIcon(hidden: widget.store.hideVip),
                  ),
              ],
            ),
          ),
        if (_showRecommendations)
          Expanded(
            child: GestureDetector(
              onHorizontalDragEnd: television ? null : _swipeCategory,
              child: RecommendationsScreen(
                repository: widget.repository,
                store: widget.store,
                embedded: true,
              ),
            ),
          )
        else ...[
          if (_loading && _items.isNotEmpty)
            const LinearProgressIndicator(minHeight: 2),
          Expanded(
            child: GestureDetector(
              onHorizontalDragEnd: television ? null : _swipeCategory,
              child: _loading && _items.isEmpty
                  ? const Center(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          CircularProgressIndicator(),
                          SizedBox(height: 18),
                          Text('正在載入劇集'),
                        ],
                      ),
                    )
                  : _items.isEmpty && _error != null
                  ? StatusPanel(
                      title: '暫時無法載入',
                      message: uiTraditional(_error!),
                      onRetry: () => _load(force: true),
                      secondaryAction:
                          widget.repository.supportsSourceManagement
                          ? TextButton(
                              onPressed: _manageSources,
                              child: const Text('片源診斷'),
                            )
                          : null,
                      icon: Icons.wifi_off_rounded,
                    )
                  : items.isEmpty
                  ? StatusPanel(
                      title: '沒有找到符合的短劇',
                      message: _hideVip
                          ? '可以換個搜尋詞，或顯示 VIP 內容。'
                          : widget.store.sources.length > 1
                          ? '可以換個搜尋詞，或改用其他片源。'
                          : '可以換個搜尋詞，或重新整理後重試。',
                      onRetry:
                          _hasMore &&
                              !_loadingMore &&
                              (!_onlineSearch ||
                                  _search.text.isEmpty ||
                                  _group.sources.any(
                                    (source) => source.pagedSearch,
                                  ))
                          ? () => _load(more: true)
                          : null,
                      action: '載入更多',
                    )
                  : LayoutBuilder(
                      builder: (context, constraints) {
                        if (television) {
                          return _televisionGrid(
                            items,
                            constraints.maxWidth,
                            key:
                                'catalog-${_group.id}-$_category-$_submittedQuery',
                            controller: _scroll,
                            footer: Padding(
                              padding: const EdgeInsets.fromLTRB(18, 0, 18, 24),
                              child: Center(
                                child: _loadingMore
                                    ? const CircularProgressIndicator()
                                    : _hiddenItems || _hasMore
                                    ? RemoteButton(
                                        label: '載入更多',
                                        icon: Icons.expand_more,
                                        onPressed: _showMore,
                                      )
                                    : const Text('已經看到這裡的全部劇集'),
                              ),
                            ),
                          );
                        }
                        final padding = constraints.maxWidth < 600
                            ? 16.0
                            : 24.0;
                        final gridWidth = constraints.maxWidth - 2 * padding;
                        final gridColumns = _posterColumns(constraints.maxWidth);
                        const gridSpacing = 14.0;
                        final tileWidth =
                            (gridWidth - (gridColumns - 1) * gridSpacing) /
                                gridColumns;
                        return RefreshIndicator(
                          onRefresh: _refreshCatalog,
                          child: CustomScrollView(
                            controller: _scroll,
                            physics: const AlwaysScrollableScrollPhysics(),
                            slivers: [
                              SliverPadding(
                                padding: EdgeInsets.fromLTRB(
                                  padding,
                                  0,
                                  padding,
                                  16,
                                ),
                                sliver: SliverGrid(
                                  gridDelegate: desktopWide
                                      ? SliverGridDelegateWithFixedCrossAxisCount(
                                          crossAxisCount: gridColumns,
                                          mainAxisExtent: DramaTile.extentFor(
                                            context,
                                            tileWidth,
                                          ),
                                          crossAxisSpacing: gridSpacing,
                                          mainAxisSpacing: gridSpacing,
                                        )
                                      : dramaGridDelegate(
                                          context,
                                          gridWidth,
                                        ),
                                  delegate: SliverChildBuilderDelegate(
                                    (_, index) => _catalogTile(items[index]),
                                    childCount: items.length,
                                  ),
                                ),
                              ),
                              SliverToBoxAdapter(
                                child: Padding(
                                  padding: const EdgeInsets.only(bottom: 24),
                                  child: Center(
                                    child: _loadingMore
                                        ? const CircularProgressIndicator()
                                        : _hiddenItems || _hasMore
                                        ? OutlinedButton.icon(
                                            onPressed: _showMore,
                                            icon: const Icon(
                                              Icons.expand_more_rounded,
                                            ),
                                            label: const Text('載入更多'),
                                          )
                                        : Text(
                                            '已經看到這裡的全部劇集',
                                            style: TextStyle(
                                              color: Theme.of(
                                                context,
                                              ).colorScheme.onSurfaceVariant,
                                              fontSize: 12,
                                            ),
                                          ),
                                  ),
                                ),
                              ),
                            ],
                          ),
                        );
                      },
                    ),
            ),
          ),
        ],
      ],
    );
  }

  /// Wide category and hot-ranking pages use identical column breakpoints.
  int _posterColumns(double width) =>
      width >= 620 ? 5 : width >= 420 ? 3 : 2;

  Widget _televisionGrid(
    List<Drama> items,
    double width, {
    required String key,
    ScrollController? controller,
    Widget? footer,
  }) {
    final columns = _posterColumns(width);
    final tileWidth = (width - 36 - (columns - 1) * 14) / columns;
    return RemoteGrid(
      key: ValueKey('tv-grid-$key'),
      itemKeys: items.map((item) => item.id).toList(),
      columns: columns,
      itemExtent: DramaTile.extentFor(context, tileWidth - 14) + 14,
      controller: controller,
      footer: footer,
      onExitLeft: _focusCurrentNavigation,
      padding: const EdgeInsets.fromLTRB(18, 2, 18, 18),
      itemBuilder: (_, index, node, onFocus) =>
          _catalogTile(items[index], focusNode: node, onFocus: onFocus),
    );
  }
}


/// Reusable D TV mark rendered as vector artwork, so the UI never falls back
/// to the former HGTV bitmap logo on any theme.
class _DtvBrandMark extends StatelessWidget {
  const _DtvBrandMark({this.size = 48});

  final double size;

  @override
  Widget build(BuildContext context) => SizedBox(
        width: size,
        height: size,
        child: CustomPaint(painter: const _DtvMarkPainter()),
      );
}

class _DtvMarkPainter extends CustomPainter {
  const _DtvMarkPainter();

  @override
  void paint(Canvas canvas, Size size) {
    final side = size.width < size.height ? size.width : size.height;
    final dx = (size.width - side) / 2;
    final dy = (size.height - side) / 2;
    canvas.save();
    canvas.translate(dx, dy);
    final rect = Offset.zero & Size(side, side);
    final outer = Path()
      ..moveTo(side * .17, side * .10)
      ..lineTo(side * .52, side * .10)
      ..cubicTo(side * .86, side * .10, side * .93, side * .32,
          side * .93, side * .50)
      ..cubicTo(side * .93, side * .69, side * .86, side * .90,
          side * .52, side * .90)
      ..lineTo(side * .17, side * .90)
      ..close();

    canvas.saveLayer(rect, Paint());
    canvas.drawPath(
      outer,
      Paint()
        ..shader = const LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0xFFFF343E), Color(0xFFD80729), Color(0xFF8D061B)],
        ).createShader(rect),
    );
    canvas.save();
    canvas.clipPath(outer);
    final gloss = Path()
      ..moveTo(side * .17, side * .10)
      ..lineTo(side * .31, side * .10)
      ..lineTo(side * .63, side * .90)
      ..lineTo(side * .48, side * .90)
      ..close();
    canvas.drawPath(gloss, Paint()..color = const Color(0x2BFFFFFF));
    canvas.restore();

    final hole = Path()
      ..moveTo(side * .34, side * .25)
      ..lineTo(side * .52, side * .25)
      ..cubicTo(side * .72, side * .25, side * .78, side * .38,
          side * .78, side * .50)
      ..cubicTo(side * .78, side * .63, side * .72, side * .75,
          side * .52, side * .75)
      ..lineTo(side * .34, side * .75)
      ..close();
    canvas.drawPath(hole, Paint()..blendMode = BlendMode.clear);
    canvas.restore();

    final play = Path()
      ..moveTo(side * .45, side * .37)
      ..lineTo(side * .45, side * .63)
      ..lineTo(side * .63, side * .50)
      ..close();
    canvas.drawPath(play, Paint()..color = const Color(0xFFFF4057));
    canvas.restore();
  }

  @override
  bool shouldRepaint(covariant _DtvMarkPainter oldDelegate) => false;
}
