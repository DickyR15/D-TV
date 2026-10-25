import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';

import 'back_safe_dialog.dart';
import 'package:flutter/services.dart';

import 'package:path_provider/path_provider.dart';

/// 應用內「紀錄」紀錄服務 — 實時記錄播放與系統事件，方便在電視與手機端快速排查問題。
class DiaryService {
  static final List<String> _entries = <String>[];
  static const int maxEntries = 500;
  static String? nativeLogPath;
  static String _nativeText = '';
  static final ValueNotifier<int> revision = ValueNotifier<int>(0);

  /// 記錄一條紀錄
  static void add(String message) {
    final now = DateTime.now();
    final timeStr =
        '${now.hour.toString().padLeft(2, '0')}:${now.minute.toString().padLeft(2, '0')}:${now.second.toString().padLeft(2, '0')}.${(now.millisecond ~/ 100)}';
    final entry = '[$timeStr] $message';
    _entries.add(entry);
    if (_entries.length > maxEntries) {
      _entries.removeRange(0, _entries.length - maxEntries);
    }
    revision.value++;
    debugPrint('[Diary] $message');
  }

  /// 獲取當前所有紀錄副本
  static List<String> get entries => List<String>.unmodifiable([
    ..._entries,
    if (_nativeText.isNotEmpty) ..._nativeText.split('\n'),
  ]);

  /// 清空紀錄
  static void clear() {
    _entries.clear();
    _nativeText = '';
    revision.value++;
  }

  /// 獲取合併後的完整紀錄文字
  static String get fullText => [_entries.join('\n'), _nativeText].join('\n');

  static Future<void> refreshNativeLogs() async {
    final path = nativeLogPath;
    if (path == null) return;
    final lines = <String>[];
    for (final name in ['$path.1', path]) {
      RandomAccessFile? file;
      try {
        file = await File(name).open();
        final size = await file.length();
        final start = size > 128 * 1024 ? size - 128 * 1024 : 0;
        await file.setPosition(start);
        final text = utf8.decode(
          await file.read(size - start),
          allowMalformed: true,
        );
        for (final line in text.split('\n').skip(start > 0 ? 1 : 0)) {
          try {
            final event = jsonDecode(line) as Map<String, dynamic>;
            if ((event['event'] as String? ?? '').startsWith('stream.')) {
              lines.add(line);
            }
          } catch (_) {}
        }
      } on FileSystemException {
        continue;
      } finally {
        await file?.close();
      }
    }
    _nativeText = lines
        .skip(lines.length > 500 ? lines.length - 500 : 0)
        .join('\n');
  }

  /// 複製全部紀錄到剪貼簿
  static Future<void> copyToClipboard(BuildContext context) async {
    await refreshNativeLogs();
    final text = fullText;
    await Clipboard.setData(ClipboardData(text: text));
    if (context.mounted) {
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(
        const SnackBar(
          content: Text('已複製播放紀錄到剪貼簿'),
          duration: Duration(seconds: 2),
        ),
      );
    }
  }

  /// 匯出完整播放紀錄。Android 會存入公開的 Downloads/D-TV 資料夾。
  static Future<void> exportToTxt(BuildContext context) async {
    await refreshNativeLogs();
    final content = fullText.trim();
    if (!context.mounted) return;
    if (content.isEmpty) {
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(
        const SnackBar(content: Text('目前沒有可匯出的紀錄')),
      );
      return;
    }

    final stamp = DateTime.now()
        .toIso8601String()
        .replaceAll(':', '-')
        .split('.')
        .first;
    final fileName = 'D-TV-playback-log_$stamp.txt';

    try {
      String? savedPath;
      if (Platform.isAndroid) {
        final result = await const MethodChannel('duanju/device')
            .invokeMapMethod<String, dynamic>(
          'exportPlaybackLog',
          <String, dynamic>{'text': '$content\n'},
        );
        savedPath = result?['path'] as String?;
      } else {
        final directory = await getApplicationDocumentsDirectory();
        final file = File('${directory.path}${Platform.pathSeparator}$fileName');
        await file.writeAsString('$content\n', encoding: utf8, flush: true);
        savedPath = file.path;
      }

      if (!context.mounted) return;
      final location = savedPath?.isNotEmpty == true
          ? savedPath!
          : 'Download/D-TV/$fileName';
      await showDialog<void>(
        context: context,
        builder: (dialogContext) => AlertDialog(
          title: const Text('紀錄匯出成功'),
          content: SelectableText(
            '已儲存播放診斷紀錄：\n$location\n\n請從檔案管理器開啟 Downloads/D-TV 資料夾，將 TXT 檔傳到電腦後上傳到 ChatGPT。',
          ),
          actions: [
            TextButton(
              onPressed: () {
                Clipboard.setData(ClipboardData(text: location));
                Navigator.of(dialogContext).pop();
              },
              child: const Text('複製檔案位置'),
            ),
            FilledButton(
              onPressed: () => Navigator.of(dialogContext).pop(),
              child: const Text('完成'),
            ),
          ],
        ),
      );
    } on PlatformException catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(
        SnackBar(content: Text('匯出紀錄失敗：${error.message ?? error.code}')),
      );
    } catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(
        SnackBar(content: Text('匯出紀錄失敗：$error')),
      );
    }
  }

  /// 在介面上彈出互動式紀錄彈窗（相容 TV 遙控器與手機觸屏）
  static Future<void> showDiaryDialog(
    BuildContext context, {
    VoidCallback? onDialogDismissed,
  }) async {
    await refreshNativeLogs();
    if (!context.mounted) return;
    return showDialog<void>(
      context: context,
      builder: (dialogContext) {
        return BackSafeDialog(
          onDismissed: onDialogDismissed,
          child: AlertDialog(
          backgroundColor: const Color(0xFF1E1E24),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(16),
            side: BorderSide(color: Colors.white.withValues(alpha: 0.1)),
          ),
          title: Row(
            children: [
              const Icon(Icons.receipt_long_rounded, color: Colors.amberAccent),
              const SizedBox(width: 8),
              const Expanded(
                child: Text(
                  '播放除錯紀錄',
                  style: TextStyle(
                    color: Colors.white,
                    fontSize: 18,
                    fontWeight: FontWeight.bold,
                  ),
                ),
              ),
              IconButton(
                icon: const Icon(Icons.copy_rounded, color: Colors.white70),
                tooltip: '複製全部紀錄',
                onPressed: () => copyToClipboard(dialogContext),
              ),
              IconButton(
                icon: const Icon(
                  Icons.delete_outline_rounded,
                  color: Colors.white70,
                ),
                tooltip: '清空紀錄',
                onPressed: () => clear(),
              ),
            ],
          ),
          content: SizedBox(
            width: double.maxFinite,
            height: MediaQuery.of(dialogContext).size.height * 0.65,
            child: AnimatedBuilder(
              animation: revision,
              builder: (context, _) {
                final list = entries;
                if (list.isEmpty) {
                  return const Center(
                    child: Text(
                      '暫無紀錄',
                      style: TextStyle(color: Colors.white38),
                    ),
                  );
                }
                return Container(
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: Colors.black.withValues(alpha: 0.4),
                    borderRadius: BorderRadius.circular(8),
                    border: Border.all(
                      color: Colors.white.withValues(alpha: 0.08),
                    ),
                  ),
                  child: _PlaybackLogList(entries: list),
                );
              },
            ),
          ),
          actions: [
            TextButton.icon(
              autofocus: true,
              icon: const Icon(Icons.file_download_outlined, size: 16),
              label: const Text('匯出紀錄 TXT'),
              onPressed: () => exportToTxt(dialogContext),
            ),
            TextButton.icon(
              icon: const Icon(Icons.copy_rounded, size: 16),
              label: const Text('一鍵複製'),
              onPressed: () => copyToClipboard(dialogContext),
            ),
            FilledButton(
              onPressed: () => Navigator.of(dialogContext).pop(),
              child: const Text('關閉'),
            ),
          ],
          ),
        );
      },
    );
  }
}

/// 可用 Android TV 遙控器上下鍵捲動的播放紀錄清單。
class _PlaybackLogList extends StatefulWidget {
  const _PlaybackLogList({required this.entries});

  final List<String> entries;

  @override
  State<_PlaybackLogList> createState() => _PlaybackLogListState();
}

class _PlaybackLogListState extends State<_PlaybackLogList> {
  final ScrollController _scrollController = ScrollController();
  final FocusNode _focusNode = FocusNode(debugLabel: 'playback-debug-log-list');

  KeyEventResult _onKeyEvent(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    final key = event.logicalKey;
    if (key != LogicalKeyboardKey.arrowUp &&
        key != LogicalKeyboardKey.arrowDown) {
      return KeyEventResult.ignored;
    }
    if (!_scrollController.hasClients) return KeyEventResult.handled;

    final position = _scrollController.position;
    final movingDown = key == LogicalKeyboardKey.arrowDown;
    final current = position.pixels;
    final edge = movingDown
        ? position.maxScrollExtent
        : position.minScrollExtent;
    final canScroll = movingDown ? current < edge - 1 : current > edge + 1;
    if (canScroll) {
      final next = (current + (movingDown ? 120 : -120))
          .clamp(position.minScrollExtent, position.maxScrollExtent)
          .toDouble();
      _scrollController.jumpTo(next);
    } else {
      // At either end, let focus traversal reach the toolbar/action buttons
      // rather than trapping the remote inside the log area.
      final scope = FocusScope.of(context);
      if (movingDown) {
        scope.nextFocus();
      } else {
        scope.previousFocus();
      }
    }
    return KeyEventResult.handled;
  }

  @override
  void dispose() {
    _scrollController.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Focus(
      focusNode: _focusNode,
      onKeyEvent: _onKeyEvent,
      child: Scrollbar(
        controller: _scrollController,
        thumbVisibility: true,
        child: ListView.builder(
          key: const ValueKey('playback-log-list'),
          controller: _scrollController,
          itemCount: widget.entries.length,
          itemBuilder: (context, index) {
            final item = widget.entries[widget.entries.length - 1 - index];
            Color textColor = Colors.white70;
            final lower = item.toLowerCase();
            if (lower.contains('err') ||
                lower.contains('fail') ||
                lower.contains('異常') ||
                lower.contains('失敗') ||
                lower.contains('exception')) {
              textColor = const Color(0xFFFF6B6B);
            } else if (lower.contains('成功') ||
                lower.contains('ok') ||
                lower.contains('success')) {
              textColor = const Color(0xFF51CF66);
            } else if (lower.contains('open') ||
                lower.contains('initialize') ||
                lower.contains('play') ||
                lower.contains('url')) {
              textColor = const Color(0xFF74C0FC);
            } else if (lower.contains('warn') ||
                lower.contains('403') ||
                lower.contains('404')) {
              textColor = const Color(0xFFFCC419);
            }
            return Padding(
              padding: const EdgeInsets.symmetric(vertical: 2.0),
              child: SelectableText(
                item,
                style: TextStyle(
                  fontFamily: 'monospace',
                  fontSize: 12,
                  color: textColor,
                ),
              ),
            );
          },
        ),
      ),
    );
  }
}

