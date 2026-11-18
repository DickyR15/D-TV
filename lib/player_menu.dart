import 'dart:math' as math;

import 'package:flutter/material.dart';

import 'models.dart';
import 'episode_browser.dart';
import 'playback_preferences.dart';
import 'video_enhancement.dart';
import 'video_enhancement_settings.dart';
import 'diary_service.dart';

enum PlayerMenuSection { episodes, speed, quality, aspectRatio, sleepTimer, settings }

/// Positions a compact player menu relative to the control that opened it.
/// The menu prefers the space above the anchor and flips below only when
/// there is not enough room, while keeping the popup inside the viewport.
Rect playerMenuPopoverRect({
  required Rect anchor,
  required Size viewport,
  required Size menuSize,
  double gap = 8,
  double margin = 8,
}) {
  final availableWidth = math.max(0.0, viewport.width - margin * 2);
  final availableHeight = math.max(0.0, viewport.height - margin * 2);
  final width = menuSize.width.clamp(0.0, availableWidth).toDouble();
  final height = menuSize.height.clamp(0.0, availableHeight).toDouble();
  final minLeft = math.min(margin, viewport.width - width);
  final maxLeft = math.max(minLeft, viewport.width - width - margin);
  final left = (anchor.center.dx - width / 2)
      .clamp(minLeft, maxLeft)
      .toDouble();
  final minTop = math.min(margin, viewport.height - height);
  final maxTop = math.max(minTop, viewport.height - height - margin);
  final above = anchor.top - gap - height;
  final below = anchor.bottom + gap;
  final top = (above >= minTop ? above : below)
      .clamp(minTop, maxTop)
      .toDouble();
  return Rect.fromLTWH(left, top, width, height);
}

class PlayerMenu extends StatefulWidget {
  const PlayerMenu({
    super.key,
    required this.section,
    this.compactPopover = false,
    required this.episodes,
    required this.currentIndex,
    required this.preferences,
    required this.qualities,
    required this.actualQuality,
    required this.local,
    required this.favorite,
    required this.mobile,
    required this.onEpisode,
    required this.onPreferences,
    required this.onFavorite,
    this.preloadStatus = '',
    this.enhancement,
    this.onCompareEnhancement,
    this.sleepMinutes = 0,
    this.sleepAtEpisodeEnd = false,
    this.onSleepTimer,
  });

  final PlayerMenuSection section;
  final bool compactPopover;
  final List<Episode> episodes;
  final int currentIndex;
  final PlaybackPreferences preferences;
  final List<int> qualities;
  final int actualQuality;
  final bool local;
  final bool favorite;
  final bool mobile;
  final String preloadStatus;
  final VideoEnhancementController? enhancement;
  final VoidCallback? onCompareEnhancement;
  final int sleepMinutes;
  final bool sleepAtEpisodeEnd;
  final Future<void> Function(int minutes, bool episodeEnd)? onSleepTimer;
  final ValueChanged<int> onEpisode;
  final Future<void> Function(PlaybackPreferences) onPreferences;
  final Future<void> Function() onFavorite;

  static double compactPopoverHeight(
    PlayerMenuSection section,
    List<int> qualities,
  ) {
    final optionCount = switch (section) {
      PlayerMenuSection.speed => playbackSpeeds.length,
      PlayerMenuSection.quality =>
        {0, ...qualities.where((quality) => quality > 0)}.length,
      PlayerMenuSection.aspectRatio => PlaybackAspectRatio.values.length,
      PlayerMenuSection.sleepTimer => 6,
      _ => 1,
    };
    return optionCount * 42.0 + 8.0;
  }

  @override
  State<PlayerMenu> createState() => _PlayerMenuState();
}

class _PlayerMenuState extends State<PlayerMenu> {
  bool _busy = false;
  String? _error;

  Future<void> _run(Future<void> Function() action) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
    } catch (_) {
      if (mounted) setState(() => _error = '未能儲存設定，請檢查儲存空間後重試。');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (widget.compactPopover) return _buildCompactPopover();
    final size = MediaQuery.sizeOf(context);
    final viewPadding = MediaQuery.viewPaddingOf(context);
    final landscape = size.width > size.height;
    final maxMenuHeight = math.max(
      180.0,
      math.min(size.height - viewPadding.vertical - 24, size.height * .84),
    );
    final episodeColumns = size.width >= 720 ? 10 : size.width >= 480 ? 8 : 6;
    final episodeRows = (widget.episodes.length / episodeColumns).ceil();
    final contentHeight = switch (widget.section) {
      PlayerMenuSection.episodes =>
        124.0 + episodeRows * 54.0 + (widget.episodes.length > 30 ? 42.0 : 0.0),
      PlayerMenuSection.speed => 204.0,
      PlayerMenuSection.quality => 224.0,
      PlayerMenuSection.aspectRatio => 196.0,
      PlayerMenuSection.sleepTimer => 196.0,
      PlayerMenuSection.settings => maxMenuHeight,
    };
    final colors = Theme.of(context).colorScheme;
    final title = switch (widget.section) {
      PlayerMenuSection.episodes => '選集 · 共 ${widget.episodes.length} 集',
      PlayerMenuSection.speed => '播放倍速',
      PlayerMenuSection.quality => '畫質',
      PlayerMenuSection.aspectRatio => '畫面比例',
      PlayerMenuSection.sleepTimer => '睡眠計時',
      PlayerMenuSection.settings => '播放設定',
    };
    return Dialog(
      key: const ValueKey('player-menu'),
      alignment: landscape ? Alignment.centerRight : Alignment.bottomCenter,
      insetPadding: const EdgeInsets.all(12),
      backgroundColor: colors.surface,
      child: SizedBox(
        width: landscape ? math.min(440, size.width * .6) : math.min(600, size.width - 24),
        height: math.min(maxMenuHeight, contentHeight),
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(18, 8, 4, 4),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      title,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                  ),
                  IconButton(
                    tooltip: '關閉選單',
                    onPressed: () => Navigator.pop(context),
                    icon: const Icon(Icons.close_rounded),
                  ),
                ],
              ),
            ),
            if (_busy) const LinearProgressIndicator(minHeight: 2),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 18,
                  vertical: 8,
                ),
                child: Text(
                  _error!,
                  style: const TextStyle(color: Color(0xFFFFB4A8)),
                ),
              ),
            Expanded(
              child: widget.section == PlayerMenuSection.episodes
                  ? PlayerEpisodeGrid(
                      episodes: widget.episodes,
                      currentIndex: widget.currentIndex,
                      keyPrefix: 'menu-episode',
                      onSelected: widget.onEpisode,
                    )
                  : SingleChildScrollView(
                      padding: const EdgeInsets.fromLTRB(18, 4, 18, 20),
                      child: _settings(),
                    ),
            ),
          ],
        ),
      ),
    );
  }


  Widget _buildCompactPopover() {
    final maxHeight = math.max(80.0, MediaQuery.sizeOf(context).height - 16);
    return Material(
      key: const ValueKey('player-quick-menu'),
      color: const Color(0xF022262D),
      elevation: 16,
      borderRadius: BorderRadius.circular(12),
      clipBehavior: Clip.antiAlias,
      child: ConstrainedBox(
        constraints: BoxConstraints(maxHeight: maxHeight),
        child: SingleChildScrollView(
          padding: const EdgeInsets.symmetric(vertical: 4),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: _compactOptions(),
          ),
        ),
      ),
    );
  }

  List<Widget> _compactOptions() {
    final preferences = widget.preferences;
    switch (widget.section) {
      case PlayerMenuSection.speed:
        return playbackSpeeds.reversed
            .map(
              (speed) => _quickOption(
                key: ValueKey('menu-speed-$speed'),
                label: '${speed}x',
                selected: speed == preferences.speed,
                onSelected: () => widget.onPreferences(
                  preferences.copyWith(speed: speed),
                ),
              ),
            )
            .toList();
      case PlayerMenuSection.quality:
        final qualities = <int>{
          0,
          ...widget.qualities.where((quality) => quality > 0),
          if (preferences.quality > 0) preferences.quality,
        }.toList()
          ..sort((a, b) {
            if (a == 0) return 1;
            if (b == 0) return -1;
            return b.compareTo(a);
          });
        return qualities
            .map(
              (quality) => _quickOption(
                key: ValueKey('menu-quality-$quality'),
                label: quality == 0 ? '自動' : '${quality}P',
                selected: quality == preferences.quality,
                onSelected: () => widget.onPreferences(
                  preferences.copyWith(quality: quality),
                ),
              ),
            )
            .toList();
      case PlayerMenuSection.aspectRatio:
        return PlaybackAspectRatio.values
            .map(
              (ratio) => _quickOption(
                key: ValueKey('menu-aspect-${ratio.name}'),
                label: playbackAspectRatioLabel(ratio),
                selected: ratio == preferences.aspectRatio,
                onSelected: () => widget.onPreferences(
                  preferences.copyWith(aspectRatio: ratio),
                ),
              ),
            )
            .toList();
      case PlayerMenuSection.sleepTimer:
        return [
          _quickOption(
            key: const ValueKey('sleep-timer-off'),
            label: '關閉',
            selected: widget.sleepMinutes == 0 && !widget.sleepAtEpisodeEnd,
            onSelected: () => widget.onSleepTimer!(0, false),
          ),
          _quickOption(
            key: const ValueKey('sleep-timer-episode'),
            label: '播完本集',
            selected: widget.sleepAtEpisodeEnd,
            onSelected: () => widget.onSleepTimer!(0, true),
          ),
          for (final minutes in [15, 30, 60, 90])
            _quickOption(
              key: ValueKey('sleep-timer-$minutes'),
              label: '$minutes 分鐘',
              selected: widget.sleepMinutes == minutes &&
                  !widget.sleepAtEpisodeEnd,
              onSelected: () => widget.onSleepTimer!(minutes, false),
            ),
        ];
      case PlayerMenuSection.episodes:
      case PlayerMenuSection.settings:
        return const [];
    }
  }

  Widget _quickOption({
    required Key key,
    required String label,
    required bool selected,
    required Future<void> Function() onSelected,
  }) {
    final foreground = selected
        ? const Color(0xFF9FC8FF)
        : Colors.white.withValues(alpha: .94);
    return InkWell(
      key: key,
      onTap: _busy ? null : () => _run(onSelected),
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 100),
        height: 42,
        padding: const EdgeInsets.symmetric(horizontal: 14),
        decoration: BoxDecoration(
          color: selected ? const Color(0x443B82D6) : Colors.transparent,
          borderRadius: BorderRadius.circular(8),
        ),
        child: Row(
          children: [
            Expanded(
              child: Text(
                label,
                textAlign: TextAlign.center,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  color: foreground,
                  fontSize: 14,
                  fontWeight: selected ? FontWeight.w700 : FontWeight.w500,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _settings() {
    final preferences = widget.preferences;
    final all = widget.section == PlayerMenuSection.settings;
    final showAspectRatio =
        all || widget.section == PlayerMenuSection.aspectRatio;
    final showSleepTimer =
        all || widget.section == PlayerMenuSection.sleepTimer;
    final colors = Theme.of(context).colorScheme;
    final helperStyle = TextStyle(
      fontSize: 13,
      color: colors.onSurfaceVariant,
      height: 1.5,
    );
    final qualities = {
      0,
      ...widget.qualities.where((quality) => quality > 0),
    }.toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (all || widget.section == PlayerMenuSection.speed) ...[
          const Text('倍速'),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final speed in playbackSpeeds)
                ChoiceChip(
                  key: ValueKey('menu-speed-$speed'),
                  label: Text('${speed}x'),
                  selected: speed == preferences.speed,
                  onSelected: _busy
                      ? null
                      : (_) => _run(
                          () => widget.onPreferences(
                            preferences.copyWith(speed: speed),
                          ),
                        ),
                ),
            ],
          ),
          const SizedBox(height: 20),
        ],
        if (all || widget.section == PlayerMenuSection.quality) ...[
          const Text('畫質'),
          const SizedBox(height: 8),
          Text(
            widget.local
                ? '本集正在播放本地原畫，畫質偏好用於後續線上播放。'
                : widget.actualQuality > 0
                ? '目前播放畫質 ${widget.actualQuality}P'
                : '使用源站可用畫質',
            style: helperStyle,
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final quality in qualities)
                ChoiceChip(
                  key: ValueKey('menu-quality-$quality'),
                  label: Text(quality == 0 ? '自動（優先高畫質）' : '${quality}P'),
                  selected: quality == preferences.quality,
                  onSelected: _busy
                      ? null
                      : (_) => _run(
                          () => widget.onPreferences(
                            preferences.copyWith(quality: quality),
                          ),
                        ),
                ),
            ],
          ),
          if (!qualities.contains(preferences.quality))
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                '已儲存 ${preferences.quality}P 偏好，本集暫無此畫質。',
                style: helperStyle,
              ),
            ),
          const SizedBox(height: 20),
        ],
        if (showAspectRatio) ...[
          const Text('畫面比例'),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final ratio in PlaybackAspectRatio.values)
                ChoiceChip(
                  key: ValueKey('menu-aspect-${ratio.name}'),
                  label: Text(playbackAspectRatioLabel(ratio)),
                  selected: ratio == preferences.aspectRatio,
                  onSelected: _busy
                      ? null
                      : (_) => _run(
                          () => widget.onPreferences(
                            preferences.copyWith(aspectRatio: ratio),
                          ),
                        ),
                ),
            ],
          ),
          const SizedBox(height: 20),
        ],
        if (showSleepTimer && widget.onSleepTimer != null) ...[
          const Text('睡眠計時'),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              ChoiceChip(
                key: const ValueKey('sleep-timer-off'),
                label: const Text('關閉'),
                selected: widget.sleepMinutes == 0 && !widget.sleepAtEpisodeEnd,
                onSelected: _busy
                    ? null
                    : (_) => _run(() => widget.onSleepTimer!(0, false)),
              ),
              ChoiceChip(
                key: const ValueKey('sleep-timer-episode'),
                label: const Text('本集結束'),
                selected: widget.sleepAtEpisodeEnd,
                onSelected: _busy
                    ? null
                    : (_) => _run(() => widget.onSleepTimer!(0, true)),
              ),
              for (final minutes in [15, 30, 60, 90])
                ChoiceChip(
                  key: ValueKey('sleep-timer-$minutes'),
                  label: Text('$minutes 分鐘'),
                  selected: widget.sleepMinutes == minutes &&
                      !widget.sleepAtEpisodeEnd,
                  onSelected: _busy
                      ? null
                      : (_) => _run(() => widget.onSleepTimer!(minutes, false)),
                ),
            ],
          ),
          const SizedBox(height: 20),
        ],
        if ((all || widget.section == PlayerMenuSection.quality) &&
            widget.enhancement != null)
          VideoEnhancementSettings(
            controller: widget.enhancement!,
            busy: _busy,
            onChanged: (enhancement) => _run(
              () => widget.onPreferences(
                preferences.copyWith(enhancement: enhancement),
              ),
            ),
            onCompare: widget.onCompareEnhancement ?? () {},
          ),
        if (all) ...[
          SwitchListTile.adaptive(
            key: const ValueKey('player-preload-enabled'),
            contentPadding: EdgeInsets.zero,
            title: const Text('下一集預載入'),
            subtitle: Text(widget.local ? '本地播放不預取網路影片' : widget.preloadStatus),
            value: preferences.preload ?? true,
            onChanged: _busy
                ? null
                : (value) => _run(
                    () => widget.onPreferences(
                      preferences.copyWith(preload: value),
                    ),
                  ),
          ),
          const SizedBox(height: 8),
          SwitchListTile.adaptive(
            key: const ValueKey('player-auto-advance'),
            contentPadding: EdgeInsets.zero,
            title: const Text('自動連播'),
            subtitle: const Text('關閉後，播放結束後停留在目前集數'),
            value: preferences.autoAdvance,
            onChanged: _busy
                ? null
                : (value) => _run(
                    () => widget.onPreferences(
                      preferences.copyWith(autoAdvance: value),
                    ),
                  ),
          ),
          const SizedBox(height: 8),
          OutlinedButton.icon(
            key: const ValueKey('player-favorite'),
            onPressed: _busy ? null : () => _run(widget.onFavorite),
            icon: Icon(
              widget.favorite
                  ? Icons.bookmark_rounded
                  : Icons.bookmark_border_rounded,
            ),
            label: Text(widget.favorite ? '取消收藏' : '加入收藏'),
          ),
          const SizedBox(height: 12),
          OutlinedButton.icon(
            key: const ValueKey('player-diary-btn'),
            onPressed: () => DiaryService.showDiaryDialog(context),
            icon: const Icon(Icons.receipt_long_rounded),
            label: const Text('檢視播放除錯紀錄'),
          ),
          const SizedBox(height: 20),
          Text(
            widget.mobile
                ? '上下滑切集；長按畫面臨時 3 倍速，鬆開恢復。豎屏輕點暫停，橫屏輕點顯示控制；雙擊播放或暫停。'
                : '空格：播放 / 暫停\n左右鍵：後退 / 快進 5 秒\n長按右鍵或畫面：臨時 3 倍速\n上下鍵：音量 ±5%，M：靜音\nF、F11、Ctrl+F：全螢幕\nEsc：先關閉選單，再退出全螢幕',
            style: helperStyle.copyWith(height: 1.6),
          ),
        ],
      ],
    );
  }
}

class PlayerEpisodeGrid extends StatelessWidget {
  const PlayerEpisodeGrid({
    super.key,
    required this.episodes,
    required this.currentIndex,
    required this.onSelected,
    this.keyPrefix = 'play-episode',
    this.compact = false,
    this.title = '選集',
    this.note = '',
  });
  final List<Episode> episodes;
  final int currentIndex;
  final ValueChanged<int> onSelected;
  final String keyPrefix;
  final bool compact;
  final String title;
  final String note;
  @override
  Widget build(BuildContext context) => EpisodeBrowser(
    episodes: episodes,
    currentNumber: episodes.isEmpty
        ? null
        : episodes[currentIndex.clamp(0, episodes.length - 1)].number,
    onSelected: onSelected,
    keyPrefix: keyPrefix,
    compact: compact,
    title: title,
    note: note,
  );
}
