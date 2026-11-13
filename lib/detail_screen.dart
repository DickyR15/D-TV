import 'dart:async';
import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';

import 'app_layout.dart';
import 'core_bridge.dart';
import 'local_store.dart';
import 'models.dart';
import 'follow_state.dart';
import 'player_screen.dart';
import 'remote_widgets.dart';
import 'widgets.dart';
import 'ui_localization.dart';
import 'sources_screen.dart';
import 'episode_browser.dart';


class DetailScreen extends StatefulWidget {
  const DetailScreen({
    super.key,
    required this.drama,
    required this.repository,
    required this.store,
    this.resumeOnOpen = false,
  });
  final Drama drama;
  final AppRepository repository;
  final LocalStore store;
  final bool resumeOnOpen;
  @override
  State<DetailScreen> createState() => _DetailScreenState();
}

class _DetailScreenState extends State<DetailScreen> {
  DramaDetail? _detail;
  late Drama _activeDrama;
  List<DramaSeason> _seriesSeasons = const [];
  String? _seriesSeasonsError;
  bool _seriesSeasonsLoading = false;
  String? _error;
  bool _loading = true;
  int _generation = 0;
  int _episodePage = 0;
  String _selectedPlayLine = '';
  bool _expandedDescription = false;
  bool _episodesExpanded = false;
  final _episodeAnchor = GlobalKey();
  final _detailScroll = ScrollController();
  final _startPlayFocus = FocusNode(debugLabel: 'detail-start-play');
  bool _initialActionHandled = false;
  late final int _profileEpoch;
  Widget? get _sourceDiagnostics => widget.repository.supportsSourceManagement
      ? SourceDiagnosticsButton(
          repository: widget.repository,
          store: widget.store,
          drama: _activeDrama,
        )
      : null;

  @override
  void initState() {
    super.initState();
    _profileEpoch = widget.store.profileEpoch;
    _activeDrama = widget.drama;
    widget.store.addListener(_onStoreChanged);
    _load();
  }

  @override
  void didUpdateWidget(covariant DetailScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.store != widget.store) {
      oldWidget.store.removeListener(_onStoreChanged);
      widget.store.addListener(_onStoreChanged);
    }
    if (oldWidget.drama.id != widget.drama.id) {
      _activeDrama = widget.drama;
      unawaited(_load());
    }
  }

  @override
  void dispose() {
    _detailScroll.dispose();
    _startPlayFocus.dispose();
    widget.store.removeListener(_onStoreChanged);
    _generation++;
    super.dispose();
  }

  void _onStoreChanged() {
    if (mounted) {
      setState(() {});
    }
  }

  Future<void> _load() async {
    final generation = ++_generation;
    final requestedDrama = _activeDrama;
    setState(() {
      _loading = true;
      _error = null;
      _seriesSeasons = const [];
      _seriesSeasonsError = null;
      _seriesSeasonsLoading = requestedDrama.source == SourceSite.hongguo.id;
    });
    try {
      final detail = await widget.repository.detail(requestedDrama);
      if (!mounted ||
          generation != _generation ||
          _profileEpoch != widget.store.profileEpoch) {
        return;
      }
      final merged = widget.repository.catalogUpdates
          .current(requestedDrama)
          .merge(detail.drama);
      final availableLines = _playLineNames(detail.episodes);
      final selectedLine = availableLines.contains(_selectedPlayLine)
          ? _selectedPlayLine
          : availableLines.isEmpty
          ? ''
          : availableLines.first;
      final visibleEpisodes = _episodesForLine(
        detail.episodes,
        selectedLine,
      );
      setState(() {
        _selectedPlayLine = selectedLine;
        _detail = DramaDetail(merged, detail.episodes, warning: detail.warning);
        _loading = false;
        _episodePage =
            resumeEpisodeIndex(
              visibleEpisodes,
              widget.store.watched(merged.id),
            ) ~/
            episodePageSize;
      });
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted ||
            generation != _generation ||
            widget.resumeOnOpen ||
            _loading ||
            _error != null ||
            (_detail?.episodes.isEmpty ?? true)) {
          return;
        }
        if (_shouldAutofocusPlay(context)) {
          _startPlayFocus.requestFocus();
        }
      });
      widget.repository.catalogUpdates.publish(merged, retryCover: true);
      unawaited(_loadSeriesSeasons(merged, generation));
      unawaited(_supplement(merged, generation));
      await saveUserChange(context, () => widget.store.refreshDrama(merged));
      if (mounted &&
          generation == _generation &&
          _profileEpoch == widget.store.profileEpoch &&
          !_initialActionHandled &&
          detail.episodes.isNotEmpty) {
        _initialActionHandled = true;
        if (widget.resumeOnOpen) {
          unawaited(
            _play(
              resumeEpisodeIndex(
                visibleEpisodes,
                widget.store.watched(merged.id),
              ),
              resume: true,
            ),
          );
        }
      }
    } catch (error) {
      if (!mounted || generation != _generation) {
        return;
      }
      setState(() {
        _error = error.toString();
        _loading = false;
      });
      widget.repository.catalogUpdates.publish(
        widget.repository.catalogUpdates.current(requestedDrama),
        retryCover: true,
      );
    }
  }

  Future<void> _loadSeriesSeasons(Drama drama, int generation) async {
    if (drama.source != SourceSite.hongguo.id) {
      if (mounted && generation == _generation) {
        setState(() {
          _seriesSeasons = const [];
          _seriesSeasonsLoading = false;
          _seriesSeasonsError = null;
        });
      }
      return;
    }
    try {
      final seasons = await widget.repository.seasons(drama);
      if (!mounted ||
          generation != _generation ||
          _profileEpoch != widget.store.profileEpoch ||
          _detail?.drama.id != drama.id) {
        return;
      }
      setState(() {
        _seriesSeasons = seasons;
        _seriesSeasonsLoading = false;
        _seriesSeasonsError = null;
      });
    } catch (error) {
      if (!mounted ||
          generation != _generation ||
          _profileEpoch != widget.store.profileEpoch ||
          _detail?.drama.id != drama.id) {
        return;
      }
      setState(() {
        _seriesSeasons = const [];
        _seriesSeasonsLoading = false;
        _seriesSeasonsError = uiTraditional(error.toString());
      });
    }
  }

  Future<void> _supplement(Drama drama, int generation) async {
    try {
      final fresh = await widget.repository.supplementMetadata(drama);
      if (!mounted ||
          generation != _generation ||
          fresh == null ||
          _detail == null) {
        return;
      }
      final updated = _detail!.drama.merge(fresh);
      setState(() {
        _detail = DramaDetail(
          updated,
          _detail!.episodes,
          warning: _detail!.warning,
        );
      });
      widget.repository.catalogUpdates.publish(updated);
      await saveUserChange(context, () => widget.store.refreshDrama(updated));
    } catch (_) {}
  }

  Future<void> _play(int index, {bool resume = false}) async {
    final detail = _detail;
    if (detail == null ||
        _profileEpoch != widget.store.profileEpoch ||
        !widget.store.allowsSource(detail.drama.source)) {
      return;
    }
    final episodes = _episodesForLine(detail.episodes);
    if (index < 0 || index >= episodes.length) {
      return;
    }
    final playDetail = DramaDetail(
      detail.drama,
      episodes,
      warning: detail.warning,
    );
    if (episodes[index].vip &&
        detail.drama.source != SourceSite.dsd.id) {
      final accepted = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: const Text('這是一集 VIP 內容'),
          content: const Text('片源可能只提供試看或有播放限制。'),
          actions: [
            TextButton(
              autofocus: AppLayout.isTelevision(context),
              onPressed: () => Navigator.pop(context, false),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('嘗試播放'),
            ),
          ],
        ),
      );
      if (accepted != true || !mounted) {
        return;
      }
    }
    final saved = widget.store.watched(detail.drama.id);
    final position =
        resume &&
            saved?.episode == episodes[index].number &&
            !saved!.finished
        ? saved.position
        : 0.0;
    if (!mounted || _profileEpoch != widget.store.profileEpoch) {
      return;
    }
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => PlayerScreen(
          detail: playDetail,
          initialIndex: index,
          initialPosition: position,
          repository: widget.repository,
          store: widget.store,
        ),
      ),
    );
    if (mounted) {
      setState(() {});
    }
  }

  Future<void> _openSeriesDrama(Drama drama) async {
    final anchor = _detail?.drama ?? _activeDrama;
    if (_profileEpoch != widget.store.profileEpoch ||
        drama.id == anchor.id ||
        !widget.store.allowsSource(drama.source)) {
      return;
    }
    if (widget.store.following(anchor.id)?.seriesSeasons[drama.id]?.read ==
        false) {
      await saveUserChange(
        context,
        () => widget.store.markSeriesSeasonRead(anchor.id, drama.id),
      );
      if (!mounted ||
          _profileEpoch != widget.store.profileEpoch ||
          drama.id == _activeDrama.id) {
        return;
      }
    }
    setState(() {
      _activeDrama = drama;
      _detail = null;
      _error = null;
      _loading = true;
      _selectedPlayLine = '';
      _episodePage = 0;
      _episodesExpanded = false;
      _expandedDescription = false;
      _seriesSeasons = const [];
      _seriesSeasonsError = null;
      _seriesSeasonsLoading = drama.source == SourceSite.hongguo.id;
    });
    // Keep the same details route and reload only the selected season's data.
    // Watch progress and favorites remain keyed by each official season ID.
    unawaited(_load());
  }

  @override
  Widget build(BuildContext context) {
    final drama = _detail?.drama ?? _activeDrama;
    final watched = widget.store.watched(drama.id);
    final allEpisodes = _detail?.episodes ?? <Episode>[];
    final playLines = _playLineNames(allEpisodes);
    final episodes = _episodesForLine(allEpisodes);
    // 新劇預設第 1 集；有觀看記錄時使用續播集數（未看完則停在原進度）。
    final resumeIndex = resumeEpisodeIndex(episodes, watched);
    final resumeEpisodeNumber = episodes.isEmpty
        ? null
        : episodes[resumeIndex].number;
    final formFactor = AppLayout.formFactorOf(context);
    final television = formFactor == FormFactor.television;
    final playAutofocus = _shouldAutofocusPlay(context);
    final allowed =
        widget.store.profileEpoch == _profileEpoch &&
        widget.store.allowsSource(drama.source);
    return CallbackShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.escape): () =>
            Navigator.of(context).maybePop(),
        const SingleActivator(LogicalKeyboardKey.goBack): () =>
            Navigator.of(context).maybePop(),
      },
      child: Scaffold(
        appBar: AppBar(
          toolbarHeight: television ? 64 : null,
          title: Text(uiTraditional(drama.title), overflow: TextOverflow.ellipsis),
          actions: [
            RefreshAction(
              loading: _loading,
              tooltip: '更新劇集資訊',
              onPressed: allowed ? _load : null,
            ),
          ],
        ),
        body: SafeArea(
          top: false,
          bottom: false,
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 1200),
              child: LayoutBuilder(
                builder: (context, constraints) {
                  if (!allowed) {
                    return const StatusPanel(
                      title: '目前使用者無權存取',
                      message: '請返回劇庫後重新選擇。',
                    );
                  }
                  if (television ||
                      (formFactor != FormFactor.phone &&
                          constraints.maxWidth >= 840)) {
                    return Row(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        SizedBox(
                          width: (constraints.maxWidth * .34).clamp(
                            250.0,
                            360.0,
                          ),
                          child: SingleChildScrollView(
                            padding: const EdgeInsets.all(20),
                            child: _overview(drama),
                          ),
                        ),
                        const VerticalDivider(width: 1),
                        Expanded(
                          child: _loading || _error != null || episodes.isEmpty
                              ? _episodeStatus()
                              : Column(
                                  children: [
                                    _seriesSelector(drama),
                                    _playLineSelector(playLines),
                                    Expanded(
                                      child: EpisodeBrowser(
                                        episodes: episodes,
                                        currentNumber: resumeEpisodeNumber,
                                        onSelected: (index) => _play(index),
                                      ),
                                    ),
                                  ],
                                ),
                        ),
                      ],
                    );
                  }
                  final start = _episodePage * episodePageSize;
                  final visible = episodes
                      .skip(start)
                      .take(episodePageSize)
                      .toList();
                  final showEpisodes = _episodesExpanded;
                  return CustomScrollView(
                    controller: _detailScroll,
                    slivers: [
                      SliverToBoxAdapter(
                        child: Padding(
                          padding: const EdgeInsets.fromLTRB(20, 12, 20, 8),
                          child: _overview(drama),
                        ),
                      ),
                      if (_loading || _error != null || episodes.isEmpty)
                        SliverFillRemaining(
                          hasScrollBody: false,
                          child: _episodeStatus(),
                        )
                      else ...[
                        SliverToBoxAdapter(child: _seriesSelector(drama)),
                        SliverToBoxAdapter(child: _playLineSelector(playLines)),
                        SliverToBoxAdapter(
                          child: _episodeSummary(
                            episodes,
                            currentNumber: resumeEpisodeNumber,
                            expanded: showEpisodes,
                          ),
                        ),
                        if (showEpisodes) ...[
                          SliverToBoxAdapter(
                            child: EpisodeRangeBar(
                              key: _episodeAnchor,
                              episodes: episodes,
                              page: _episodePage,
                              title: '分組',
                              currentNumber: resumeEpisodeNumber,
                              onLocate: _locateEpisode,
                            ),
                          ),
                          SliverPadding(
                            padding: const EdgeInsets.fromLTRB(20, 4, 20, 24),
                            sliver: SliverGrid(
                              gridDelegate:
                                  SliverGridDelegateWithFixedCrossAxisCount(
                                    crossAxisCount: _episodeColumns(
                                      constraints.maxWidth,
                                      episodes,
                                    ),
                                    mainAxisExtent: math.max(
                                      54,
                                      MediaQuery.textScalerOf(
                                            context,
                                          ).scale(20) +
                                          30,
                                    ),
                                    crossAxisSpacing: 10,
                                    mainAxisSpacing: 10,
                                  ),
                              delegate: SliverChildBuilderDelegate((
                                context,
                                index,
                              ) {
                                final episode = visible[index];
                                return RemoteEpisodeButton(
                                  key: ValueKey('episode-${episode.number}'),
                                  number: episode.number,
                                  vip: episode.vip,
                                  current: episode.number == resumeEpisodeNumber,
                                  onPressed: () => _play(start + index),
                                );
                              }, childCount: visible.length),
                            ),
                          ),
                        ],
                      ],
                    ],
                  );
                },
              ),
            ),
          ),
        ),
        bottomNavigationBar: Material(
          color: Theme.of(context).colorScheme.surface,
          child: SafeArea(
            top: false,
            child: Center(
              heightFactor: 1,
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 1000),
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(16, 10, 16, 12),
                  child: Row(
                    children: [
                      Expanded(
                        child: FilledButton.icon(
                          key: const ValueKey('start-play'),
                          focusNode: playAutofocus ? _startPlayFocus : null,
                          autofocus: playAutofocus,
                          style: FilledButton.styleFrom(
                            minimumSize: const Size(0, 52),
                            padding: const EdgeInsets.symmetric(
                              horizontal: 16,
                              vertical: 12,
                            ),
                          ),
                          onPressed: !allowed || _loading || episodes.isEmpty
                              ? null
                              : () => _play(resumeIndex, resume: true),
                          icon: const Icon(Icons.play_arrow_rounded, size: 26),
                          label: Text(
                            watched != null && episodes.isNotEmpty
                                ? '繼續播放 · 第 ${episodes[resumeIndex].number} 集'
                                : '播放',
                            textAlign: TextAlign.center,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  List<String> _playLineNames(List<Episode> episodes) {
    final seen = <String>{};
    return [
      for (final episode in episodes)
        if (episode.playLine.trim().isNotEmpty &&
            seen.add(episode.playLine.trim()))
          episode.playLine.trim(),
    ];
  }

  List<Episode> _episodesForLine(
    List<Episode> episodes, [
    String? requestedLine,
  ]) {
    final lines = _playLineNames(episodes);
    if (lines.isEmpty) return episodes;
    final selected = requestedLine ?? _selectedPlayLine;
    final activeLine = lines.contains(selected) ? selected : lines.first;
    return episodes
        .where((episode) => episode.playLine.trim() == activeLine)
        .toList();
  }

  void _selectPlayLine(String? line) {
    if (line == null || line == _selectedPlayLine || _detail == null) return;
    final episodes = _episodesForLine(_detail!.episodes, line);
    setState(() {
      _selectedPlayLine = line;
      _episodePage =
          resumeEpisodeIndex(
            episodes,
            widget.store.watched(_detail!.drama.id),
          ) ~/
          episodePageSize;
    });
  }

  Widget _playLineSelector(List<String> lines) {
    if (lines.length < 2) return const SizedBox.shrink();
    final selected = lines.contains(_selectedPlayLine)
        ? _selectedPlayLine
        : lines.first;
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 8, 20, 12),
      child: Align(
        alignment: Alignment.centerLeft,
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 360),
          child: InputDecorator(
            decoration: const InputDecoration(
              labelText: '播放線路',
              border: OutlineInputBorder(),
              contentPadding: EdgeInsets.symmetric(
                horizontal: 12,
                vertical: 4,
              ),
            ),
            child: DropdownButtonHideUnderline(
              child: DropdownButton<String>(
                value: selected,
                isExpanded: true,
                items: [
                  for (final line in lines)
                    DropdownMenuItem<String>(
                      value: line,
                      child: Text(
                        uiTraditional(line),
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                ],
                onChanged: _loading ? null : _selectPlayLine,
              ),
            ),
          ),
        ),
      ),
    );
  }

  bool _shouldAutofocusPlay(BuildContext context) {
    return AppLayout.formFactorOf(context) == FormFactor.television;
  }

  Widget _episodeStatus() {
    if (_loading) {
      return const Center(
        child: Padding(
          padding: EdgeInsets.all(32),
          child: CircularProgressIndicator(),
        ),
      );
    }
    return StatusPanel(
      title: _error != null ? '劇集資訊暫時不可用' : '暫時沒有可播放的集數',
      message: _error == null ? '可以更新劇集資訊後重試。' : uiTraditional(_error!),
      onRetry: _load,
      secondaryAction: _sourceDiagnostics,
    );
  }

  Widget _episodeSummary(
    List<Episode> episodes, {
    required int? currentNumber,
    required bool expanded,
  }) {
    final colors = Theme.of(context).colorScheme;
    final current = currentNumber == null ? '' : ' · 目前第 $currentNumber 集';
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 8, 20, 8),
      child: Material(
        color: colors.surfaceContainer,
        borderRadius: BorderRadius.circular(8),
        child: InkWell(
          borderRadius: BorderRadius.circular(8),
          onTap: () => setState(() => _episodesExpanded = !expanded),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
            child: Row(
              children: [
                Icon(Icons.grid_view_rounded, color: colors.onSurfaceVariant),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    '選集 · ${episodes.length} 集$current',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.titleSmall?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                Text(expanded ? '收起' : '展開'),
                const SizedBox(width: 2),
                Icon(
                  expanded
                      ? Icons.expand_less_rounded
                      : Icons.expand_more_rounded,
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  void _locateEpisode(int index) {
    final episodes = _detail!.episodes;
    setState(() {
      _episodePage = index ~/ episodePageSize;
    });
    WidgetsBinding.instance.addPostFrameCallback((_) {
      final anchor = _episodeAnchor.currentContext;
      if (!mounted || anchor == null || !_detailScroll.hasClients) return;
      final render = anchor.findRenderObject();
      if (render is! RenderBox || !render.hasSize) return;
      final viewport = RenderAbstractViewport.maybeOf(render);
      if (viewport == null) return;
      final start = viewport.getOffsetToReveal(render, 0).offset;
      final extent = math.max(
        54,
        MediaQuery.textScalerOf(context).scale(20) + 30,
      );
      final columns = _episodeColumns(render.size.width, episodes);
      final target =
          start +
          render.size.height +
          (index % episodePageSize ~/ columns) * (extent + 10) -
          _detailScroll.position.viewportDimension * .3;
      _detailScroll.animateTo(
        target.clamp(0.0, _detailScroll.position.maxScrollExtent),
        duration: const Duration(milliseconds: 180),
        curve: Curves.easeOut,
      );
    });
  }

  int _episodeColumns(double width, List<Episode> episodes) {
    final digits = episodes.fold<int>(
      1,
      (value, episode) => math.max(value, episode.number.toString().length),
    );
    final minimum = math.max(
      82,
      MediaQuery.textScalerOf(context).scale(20) * digits * .65 + 40,
    );
    return ((width - 40) / minimum).floor().clamp(1, 12);
  }

  Widget _overview(Drama drama) {
    final colors = Theme.of(context).colorScheme;
    final meta = [
      SourceSite.byId(drama.source).name,
      if (drama.episodes > 0) '共 ${drama.episodes} 集',
      if (drama.releaseStatus.isNotEmpty && drama.releaseStatus != 'unknown')
        uiTraditional(drama.releaseLabel),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SizedBox(
              width: 92,
              height: 138,
              child: DramaCover(
                drama: drama,
                repository: widget.repository,
                coverPriority: true,
              ),
            ),
            const SizedBox(width: 16),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    uiTraditional(drama.title),
                    style: Theme.of(context).textTheme.titleLarge?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  const SizedBox(height: 10),
                  Text(
                    meta.join(' · '),
                    style: TextStyle(
                      color: colors.onSurfaceVariant,
                      height: 1.5,
                    ),
                  ),
                  if (drama.category.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(top: 6),
                      child: Text(
                        uiTraditional(drama.category),
                        style: TextStyle(color: colors.onSurfaceVariant),
                      ),
                    ),
                  if (drama.source == 'huangdou')
                    Padding(
                      padding: const EdgeInsets.only(top: 6),
                      child: Text(
                        drama.vipStatus == null
                            ? 'VIP 狀態待補齊'
                            : drama.vip
                            ? 'VIP 內容'
                            : '免費內容',
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                    ),
                ],
              ),
            ),
          ],
        ),
        const SizedBox(height: 12),
        _followingControls(drama),
        if (drama.onlineDate.isNotEmpty ||
            drama.heat.isNotEmpty ||
            drama.displayViews.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(top: 10),
            child: Text(
              [
                if (drama.onlineDate.isNotEmpty) '${uiTraditional(drama.onlineDate)} 上線',
                if (drama.heat.isNotEmpty) '熱度 ${uiTraditional(drama.heat)}',
                if (drama.displayViews.isNotEmpty) '播放 ${uiTraditional(drama.displayViews)}',
              ].join(' · '),
              style: TextStyle(color: colors.onSurfaceVariant, fontSize: 12),
            ),
          ),
        if (drama.tags.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(top: 12),
            child: Wrap(
              spacing: 6,
              runSpacing: 6,
              children: [
                for (final tag in drama.tags.take(12))
                  DecoratedBox(
                    decoration: BoxDecoration(
                      color: colors.surfaceContainerHighest,
                      borderRadius: BorderRadius.circular(6),
                    ),
                    child: Padding(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 8,
                        vertical: 5,
                      ),
                      child: Text(
                        uiTraditional(tag),
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                    ),
                  ),
              ],
            ),
          ),
        if (drama.description.isNotEmpty) ...[
          const SizedBox(height: 12),
          Text(
            uiTraditional(drama.description),
            maxLines: _expandedDescription ? null : 3,
            overflow: _expandedDescription
                ? TextOverflow.visible
                : TextOverflow.ellipsis,
            style: TextStyle(color: colors.onSurfaceVariant, height: 1.6),
          ),
          Align(
            alignment: Alignment.centerRight,
            child: TextButton(
              onPressed: () =>
                  setState(() => _expandedDescription = !_expandedDescription),
              child: Text(_expandedDescription ? '收起簡介' : '展開簡介'),
            ),
          ),
        ],
        if (_detail?.warning.isNotEmpty == true)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(
              uiTraditional(_detail!.warning),
              style: TextStyle(color: colors.error),
            ),
          ),
      ],
    );
  }

  Widget _seriesSelector(Drama drama) {
    if (drama.source != SourceSite.hongguo.id) {
      return const SizedBox.shrink();
    }
    if (_seriesSeasonsLoading && _seriesSeasons.isEmpty) {
      return const Padding(
        padding: EdgeInsets.fromLTRB(20, 10, 20, 8),
        child: LinearProgressIndicator(),
      );
    }
    final seasons = _seriesSeasons;
    if (_seriesSeasonsError != null && seasons.isEmpty) {
      return Padding(
        padding: const EdgeInsets.fromLTRB(20, 10, 20, 8),
        child: Row(
          children: [
            Expanded(
              child: Text(
                '系列季別暫時無法載入：${_seriesSeasonsError!}',
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
            TextButton.icon(
              onPressed: _seriesSeasonsLoading
                  ? null
                  : () => unawaited(
                      _loadSeriesSeasons(drama, _generation),
                    ),
              icon: const Icon(Icons.refresh_rounded),
              label: const Text('重試'),
            ),
          ],
        ),
      );
    }
    if (seasons.length < 2) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 10, 20, 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('系列劇', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final season in seasons)
                _seasonChip(drama, season),
            ],
          ),
          if (_seriesSeasonsLoading)
            const Padding(
              padding: EdgeInsets.only(top: 8),
              child: LinearProgressIndicator(),
            ),
        ],
      ),
    );
  }

  Widget _seasonChip(Drama currentDrama, DramaSeason season) {
    final current = season.drama.id == currentDrama.id;
    final watched = widget.store.watched(season.drama.id);
    final label = season.label.isNotEmpty
        ? uiTraditional(season.label)
        : season.number > 0
        ? '第 ${season.number} 季'
        : current
        ? '目前季別'
        : '其他季別';
    final details = <String>[
      if (season.drama.episodes > 0) '共 ${season.drama.episodes} 集',
      if (season.upcoming) '即將上線',
      if (!season.playable && !season.upcoming) '暫不可播放',
      if (watched != null && !current) '有觀看紀錄',
    ];
    final text = [
      label,
      ...details,
    ].join(' · ');
    return Tooltip(
      message: [
        uiTraditional(season.drama.title),
        if (season.subtitle.isNotEmpty) uiTraditional(season.subtitle),
      ].where((value) => value.isNotEmpty).join('\n'),
      child: ChoiceChip(
        key: ValueKey('official-season-${season.drama.id}'),
        label: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 260),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                text,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
              if (season.subtitle.isNotEmpty)
                Text(
                  uiTraditional(season.subtitle),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context).textTheme.labelSmall,
                ),
            ],
          ),
        ),
        selected: current,
        onSelected: current || !season.playable
            ? null
            : (_) => _openSeriesDrama(season.drama),
      ),
    );
  }

  Widget _followingControls(Drama drama) {
    final following = widget.store.following(drama.id);
    return Wrap(
      spacing: 8,
      runSpacing: 4,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        FilledButton.tonalIcon(
          key: const ValueKey('favorite-toggle'),
          onPressed: () async {
            if (_profileEpoch != widget.store.profileEpoch) return;
            await saveUserChange(
              context,
              () => widget.store.toggleFavorite(drama),
            );
          },
          icon: Icon(
            following == null
                ? Icons.bookmark_add_outlined
                : Icons.bookmark_remove_outlined,
          ),
          label: Text(following == null ? '加入收藏' : '取消收藏'),
        ),
        if (following != null && following.hasUpdates)
          ActionChip(
            label: Text('${following.updateLabel} · 標為已讀'),
            onPressed: () => saveUserChange(
              context,
              () => widget.store.markUpdatesRead(drama.id),
            ),
          ),
      ],
    );
  }
}
