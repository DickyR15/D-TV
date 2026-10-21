import 'package:flutter/material.dart';

import 'remote_widgets.dart';
import 'video_enhancement.dart';
import 'video_enhancement_preferences.dart';

class VideoEnhancementSettings extends StatelessWidget {
  const VideoEnhancementSettings({
    super.key,
    required this.controller,
    required this.onChanged,
    required this.onCompare,
    this.television = false,
    this.busy = false,
  });

  final VideoEnhancementController controller;
  final ValueChanged<VideoEnhancementPreferences> onChanged;
  final VoidCallback onCompare;
  final bool television;
  final bool busy;

  Widget _choice({
    required String id,
    required String label,
    required bool selected,
    required VoidCallback action,
    bool enabled = true,
  }) => television
      ? RemoteButton(
          key: ValueKey(id),
          label: label,
          selected: selected,
          onPressed: busy || !enabled ? null : action,
        )
      : ChoiceChip(
          key: ValueKey(id),
          label: Text(label),
          selected: selected,
          onSelected: busy || !enabled ? null : (_) => action(),
        );

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller,
    builder: (context, _) {
      final preferences = controller.preferences;
      final colors = Theme.of(context).colorScheme;
      final helperStyle = TextStyle(
        fontSize: 13,
        color: colors.onSurfaceVariant,
        height: 1.5,
      );
      final enabled = preferences.mode != VideoEnhancementMode.off;
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '畫質增強',
            style: television ? const TextStyle(fontSize: 18) : null,
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final mode in VideoEnhancementMode.values)
                _choice(
                  id: 'enhancement-mode-${mode.name}',
                  label: mode.label,
                  selected: preferences.mode == mode,
                  enabled:
                      controller.supported || mode == VideoEnhancementMode.off,
                  action: () => onChanged(preferences.copyWith(mode: mode)),
                ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            enabled
                ? '自動按畫面和裝置選擇增強；省電使用輕量縮放；清晰優先允許更高負載。建議先選擇源站高畫質畫質。'
                : '預設關閉，不改變影片輸出；開啟後再配置畫面型別和原畫對比。',
            style: helperStyle,
          ),
          if (enabled) ...[
            const SizedBox(height: 16),
            const Text('畫面型別'),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final content in VideoEnhancementContent.values)
                  _choice(
                    id: 'enhancement-content-${content.name}',
                    label: content.label,
                    selected: preferences.content == content,
                    enabled: controller.supported,
                    action: () =>
                        onChanged(preferences.copyWith(content: content)),
                  ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              preferences.content == VideoEnhancementContent.automatic
                  ? '${controller.contentLabel}；分類不準確時可手動切換。'
                  : '動漫檔適用於動畫和 AI 動漫；寫實 AI 畫面建議使用通用檔。',
              style: helperStyle,
            ),
            const SizedBox(height: 12),
            Text(controller.status, key: const ValueKey('enhancement-status')),
            if (controller.detail.isNotEmpty) ...[
              const SizedBox(height: 4),
              Text(controller.detail, style: helperStyle),
            ],
            const SizedBox(height: 8),
            if (television)
              RemoteButton(
                key: const ValueKey('enhancement-compare'),
                label: controller.comparing ? '恢復增強' : '原畫對比',
                icon: Icons.compare_rounded,
                onPressed: controller.canCompare && !busy ? onCompare : null,
              )
            else
              OutlinedButton.icon(
                key: const ValueKey('enhancement-compare'),
                onPressed: controller.canCompare && !busy ? onCompare : null,
                icon: const Icon(Icons.compare_rounded),
                label: Text(controller.comparing ? '恢復增強' : '原畫對比'),
              ),
          ] else if (!controller.supported) ...[
            const SizedBox(height: 12),
            Text(controller.status, key: const ValueKey('enhancement-status')),
          ],
          const SizedBox(height: 20),
        ],
      );
    },
  );
}
