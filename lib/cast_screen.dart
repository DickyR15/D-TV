import 'dart:async';

import 'package:flutter/material.dart';

import 'core_bridge.dart';

class CastDevice {
  const CastDevice({required this.id, required this.name});

  final String id;
  final String name;

  factory CastDevice.fromJson(Map<String, dynamic> json) => CastDevice(
    id: json['id'] as String? ?? '',
    name: json['name'] as String? ?? '未知裝置',
  );
}

Future<CastDevice?> showCastDevicePicker(
  BuildContext context, {
  required Future<Map<String, dynamic>> discovery,
}) => showDialog<CastDevice>(
  context: context,
  builder: (_) => _CastDevicePicker(discovery: discovery),
);

class _CastDevicePicker extends StatelessWidget {
  const _CastDevicePicker({required this.discovery});

  final Future<Map<String, dynamic>> discovery;

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('選擇投屏裝置'),
    content: SizedBox(
      width: 420,
      child: FutureBuilder<Map<String, dynamic>>(
        future: discovery,
        builder: (context, snapshot) {
          if (snapshot.connectionState != ConnectionState.done) {
            return const Padding(
              padding: EdgeInsets.symmetric(vertical: 28),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  CircularProgressIndicator(),
                  SizedBox(height: 16),
                  Text('正在搜尋區域網路中的 DLNA 裝置…'),
                ],
              ),
            );
          }
          if (snapshot.hasError) {
            return Padding(
              padding: const EdgeInsets.symmetric(vertical: 12),
              child: Text('搜尋失敗：${snapshot.error}'),
            );
          }
          final rows = (snapshot.data?['items'] as List? ?? const [])
              .whereType<Map>()
              .map((row) => CastDevice.fromJson(
                    Map<String, dynamic>.from(row),
                  ))
              .where((device) => device.id.isNotEmpty)
              .toList();
          if (rows.isEmpty) {
            return const Padding(
              padding: EdgeInsets.symmetric(vertical: 16),
              child: Text(
                '找不到支援 DLNA／UPnP 的裝置。請確認播放裝置與電視連線至同一個區域網路，並確認電視已開啟投屏或媒體接收功能。',
              ),
            );
          }
          return ConstrainedBox(
            constraints: const BoxConstraints(maxHeight: 360),
            child: ListView.separated(
              shrinkWrap: true,
              itemCount: rows.length,
              separatorBuilder: (_, _) => const Divider(height: 1),
              itemBuilder: (context, index) {
                final device = rows[index];
                return ListTile(
                  key: ValueKey('cast-device-${device.id}'),
                  leading: const Icon(Icons.tv_rounded),
                  title: Text(device.name),
                  subtitle: const Text('DLNA／UPnP'),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: () => Navigator.pop(context, device),
                );
              },
            ),
          );
        },
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('取消'),
      ),
    ],
  );
}

Future<void> showCastPlaybackControls(
  BuildContext context, {
  required AppRepository repository,
  required CastDevice device,
}) => showDialog<void>(
  context: context,
  barrierDismissible: true,
  builder: (_) => _CastPlaybackDialog(repository: repository, device: device),
);

class _CastPlaybackDialog extends StatefulWidget {
  const _CastPlaybackDialog({required this.repository, required this.device});

  final AppRepository repository;
  final CastDevice device;

  @override
  State<_CastPlaybackDialog> createState() => _CastPlaybackDialogState();
}

class _CastPlaybackDialogState extends State<_CastPlaybackDialog> {
  Timer? _poll;
  bool _busy = false;
  String _state = 'loading';
  String? _error;
  int _positionMs = 0;
  int _durationMs = 0;
  double? _dragValue;

  @override
  void initState() {
    super.initState();
    unawaited(_refresh());
    _poll = Timer.periodic(
      const Duration(seconds: 3),
      (_) => unawaited(_refresh()),
    );
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _refresh() async {
    try {
      final result = await widget.repository.cast('status');
      if (!mounted) return;
      setState(() {
        _state = result['state'] as String? ?? 'stopped';
        _positionMs = (result['positionMs'] as num? ?? 0).round().clamp(0, 1 << 52).toInt();
        _durationMs = (result['durationMs'] as num? ?? 0).round().clamp(0, 1 << 52).toInt();
        _error = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = error.toString());
    }
  }

  Future<void> _command(String command, {int? startMs}) async {
    if (_busy) return;
    setState(() => _busy = true);
    try {
      await widget.repository.cast(command, startMs: startMs ?? 0);
      if (command == 'stop') {
        if (mounted) Navigator.pop(context);
        return;
      }
      await _refresh();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  String _clock(int milliseconds) {
    final seconds = (milliseconds ~/ 1000).clamp(0, 359999);
    final minutes = seconds ~/ 60;
    final tail = (seconds % 60).toString().padLeft(2, '0');
    return minutes >= 60
        ? '${minutes ~/ 60}:${(minutes % 60).toString().padLeft(2, '0')}:$tail'
        : '$minutes:$tail';
  }

  @override
  Widget build(BuildContext context) {
    final playing = _state == 'playing';
    final max = _durationMs > 0 ? _durationMs.toDouble() : 1.0;
    final value = (_dragValue ?? _positionMs.toDouble()).clamp(0.0, max);
    return AlertDialog(
      title: const Text('傳送播放'),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Row(
              children: [
                const Icon(Icons.cast_connected_rounded, size: 30),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        widget.device.name,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      Text(
                        switch (_state) {
                          'playing' => '正在播放',
                          'paused' => '已暫停',
                          'loading' => '連線中',
                          _ => '播放已停止',
                        },
                      ),
                    ],
                  ),
                ),
              ],
            ),
            if (_error case final error?)
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: Text(
                  error,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ),
            const SizedBox(height: 16),
            Slider(
              value: value,
              max: max,
              onChanged: !_busy && _durationMs > 0
                  ? (next) => setState(() => _dragValue = next)
                  : null,
              onChangeEnd: (next) {
                setState(() {
                  _dragValue = null;
                  _positionMs = next.round();
                });
                unawaited(_command('seek', startMs: next.round()));
              },
            ),
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Text(_clock(value.round())),
                Text(_clock(_durationMs)),
              ],
            ),
            const SizedBox(height: 12),
            Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                IconButton.filled(
                  tooltip: playing ? '暫停' : '繼續播放',
                  onPressed: _busy || _state == 'loading'
                      ? null
                      : () => unawaited(_command(playing ? 'pause' : 'resume')),
                  icon: Icon(
                    playing ? Icons.pause_rounded : Icons.play_arrow_rounded,
                  ),
                  iconSize: 30,
                ),
                const SizedBox(width: 24),
                IconButton(
                  tooltip: '結束投屏',
                  onPressed: _busy ? null : () => unawaited(_command('stop')),
                  icon: const Icon(Icons.stop_circle_outlined),
                  iconSize: 30,
                ),
              ],
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('關閉'),
        ),
      ],
    );
  }
}
