import 'dart:async';

import 'package:flutter/material.dart';

import 'core_bridge.dart';
import 'follow_state.dart';
import 'local_store.dart';
import 'models.dart';
import 'player_screen.dart';
import 'widgets.dart';
import 'ui_localization.dart';

Future<void> openPlaybackDirectly(
  BuildContext context, {
  required Drama drama,
  required AppRepository repository,
  required LocalStore store,
}) async {
  if (!context.mounted) return;
  // Acknowledge the tap immediately. The launch page resolves metadata and
  // replaces itself with the player, instead of waiting on the catalog page.
  await Navigator.of(context).push<void>(
    MaterialPageRoute<void>(
      builder: (_) => PlaybackLaunchScreen(
        drama: drama,
        repository: repository,
        store: store,
      ),
    ),
  );
}

class PlaybackLaunchScreen extends StatefulWidget {
  const PlaybackLaunchScreen({
    super.key,
    required this.drama,
    required this.repository,
    required this.store,
  });

  final Drama drama;
  final AppRepository repository;
  final LocalStore store;

  @override
  State<PlaybackLaunchScreen> createState() => _PlaybackLaunchScreenState();
}

class _PlaybackLaunchScreenState extends State<PlaybackLaunchScreen> {
  late final int _profileEpoch;
  int _generation = 0;
  String? _error;
  bool _loading = true;
  bool _showLoadingIndicator = false;
  Timer? _loadingTimer;

  @override
  void initState() {
    super.initState();
    _profileEpoch = widget.store.profileEpoch;
    _scheduleLoadingIndicator(_generation);
    unawaited(_load());
  }

  void _scheduleLoadingIndicator(int generation) {
    _loadingTimer?.cancel();
    _showLoadingIndicator = false;
    _loadingTimer = Timer(const Duration(milliseconds: 400), () {
      if (!mounted ||
          generation != _generation ||
          !_loading ||
          _error != null) {
        return;
      }
      setState(() => _showLoadingIndicator = true);
    });
  }

  @override
  void dispose() {
    _generation++;
    super.dispose();
  }

  Future<void> _load() async {
    final generation = ++_generation;
    setState(() {
      _loading = true;
      _showLoadingIndicator = false;
      _error = null;
    });
    _scheduleLoadingIndicator(generation);
    try {
      final detail = await widget.repository.detail(widget.drama);
      if (!mounted ||
          generation != _generation ||
          _profileEpoch != widget.store.profileEpoch) {
        return;
      }
      final drama = widget.repository.catalogUpdates
          .current(widget.drama)
          .merge(detail.drama);
      final merged = DramaDetail(
        drama,
        detail.episodes,
        warning: detail.warning,
      );
      widget.repository.catalogUpdates.publish(drama, retryCover: true);
      if (!mounted ||
          generation != _generation ||
          _profileEpoch != widget.store.profileEpoch) {
        return;
      }
      // Metadata persistence is useful but should not delay the first frame.
      unawaited(widget.store.refreshDrama(drama).catchError((Object _) {}));
      if (merged.episodes.isEmpty) {
        _loadingTimer?.cancel();
        setState(() {
          _loading = false;
          _showLoadingIndicator = false;
          _error = '暫時沒有可播放的集數';
        });
        return;
      }
      final watched = widget.store.watched(drama.id);
      final index = resumeEpisodeIndex(merged.episodes, watched);
      final episode = merged.episodes[index];
      if (episode.vip && drama.source != SourceSite.dsd.id) {
        if (!mounted) return;
        final accepted = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: const Text('這是一集 VIP 內容'),
            content: const Text('站源可能只提供試看或限制播放。'),
            actions: [
              TextButton(
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
        if (accepted != true) {
          if (mounted) Navigator.of(context).maybePop();
          return;
        }
        if (!mounted) {
          return;
        }
      }
      final position =
          watched?.episode == episode.number && watched?.finished == false
          ? watched!.position
          : 0.0;
      if (!mounted ||
          generation != _generation ||
          _profileEpoch != widget.store.profileEpoch) {
        return;
      }
      _loadingTimer?.cancel();
      setState(() {
        _loading = false;
        _showLoadingIndicator = false;
      });
      Navigator.of(context).pushReplacement(
        MaterialPageRoute<void>(
          builder: (_) => PlayerScreen(
            detail: merged,
            initialIndex: index,
            initialPosition: position,
            repository: widget.repository,
            store: widget.store,
          ),
        ),
      );
    } catch (error) {
      if (!mounted || generation != _generation) return;
      _loadingTimer?.cancel();
      setState(() {
        _loading = false;
        _showLoadingIndicator = false;
        _error = error.toString();
      });
      widget.repository.catalogUpdates.publish(
        widget.repository.catalogUpdates.current(widget.drama),
        retryCover: true,
      );
    }
  }

  @override
  void dispose() {
    _generation++;
    _loadingTimer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(uiTraditional(widget.drama.title))),
    body: SafeArea(
      top: false,
      child: Stack(
        fit: StackFit.expand,
        children: [
          const ColoredBox(color: Colors.black),
          Center(
            child: SizedBox(
              width: 156,
              height: 234,
              child: DramaCover(
                drama: widget.drama,
                repository: widget.repository,
                radius: 12,
                showGradient: false,
                showEpisodeBadge: false,
                coverPriority: true,
              ),
            ),
          ),
          if (_error != null)
            ColoredBox(
              color: Colors.black.withValues(alpha: .88),
              child: StatusPanel(
                title: '暫時無法播放',
                message: uiTraditional(_error!),
                onRetry: _load,
                action: '重試',
                icon: Icons.play_disabled_rounded,
              ),
            )
          else if (_showLoadingIndicator)
            Align(
              alignment: Alignment.bottomCenter,
              child: Padding(
                padding: const EdgeInsets.only(bottom: 36),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                    const SizedBox(width: 12),
                    Text(
                      '正在讀取影片',
                      style: TextStyle(
                        color: Colors.white.withValues(alpha: .9),
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ],
                ),
              ),
            ),
        ],
      ),
    ),
  );
}
