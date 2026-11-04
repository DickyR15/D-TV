import 'dart:convert';

import 'package:desktop_multi_window/desktop_multi_window.dart';

import 'models.dart';

const desktopMiniPlayerChannelName = 'dtv.desktop.mini.player';

const desktopMiniPlayerChannel = WindowMethodChannel(
  desktopMiniPlayerChannelName,
);

/// Immutable playback handoff passed to the independent desktop engine.
class DesktopMiniPlayerArguments {
  const DesktopMiniPlayerArguments({
    required this.detail,
    required this.initialIndex,
    required this.initialPosition,
  });

  static const businessId = 'dtv_mini_player';

  final DramaDetail detail;
  final int initialIndex;
  final double initialPosition;

  factory DesktopMiniPlayerArguments.fromJson(Map<String, dynamic> json) {
    final index = intValue(json['initialIndex']);
    final position = (json['initialPosition'] as num?)?.toDouble() ?? 0;
    return DesktopMiniPlayerArguments(
      detail: DramaDetail.fromJson(
        Map<String, dynamic>.from(json['detail'] as Map),
      ),
      initialIndex: index,
      initialPosition: position.isFinite && position > 0 ? position : 0,
    );
  }

  Map<String, dynamic> toJson() => {
    'businessId': businessId,
    'detail': {
      'drama': detail.drama.toJson(),
      'chapters': detail.episodes.map((episode) => episode.raw).toList(),
      'warning': detail.warning,
    },
    'initialIndex': initialIndex,
    'initialPosition': initialPosition,
  };

  static DesktopMiniPlayerArguments? tryParse(String encoded) {
    if (encoded.isEmpty) return null;
    try {
      final json = jsonDecode(encoded);
      if (json is! Map) return null;
      final data = Map<String, dynamic>.from(json);
      if (data['businessId'] != businessId) return null;
      return DesktopMiniPlayerArguments.fromJson(data);
    } catch (_) {
      return null;
    }
  }
}

/// Creates the native child window using a serialized playback snapshot.
Future<WindowController> createDesktopMiniPlayerWindow({
  required DramaDetail detail,
  required int index,
  required double position,
}) async {
  final arguments = DesktopMiniPlayerArguments(
    detail: detail,
    initialIndex: index,
    initialPosition: position,
  );
  final controller = await WindowController.create(
    WindowConfiguration(
      arguments: jsonEncode(arguments.toJson()),
      hiddenAtLaunch: true,
    ),
  );
  await controller.show();
  return controller;
}
