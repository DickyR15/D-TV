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
    required this.authorizedSession,
  });

  static const businessId = 'dtv_mini_player';

  final DramaDetail detail;
  final int initialIndex;
  final double initialPosition;
  final bool authorizedSession;

  factory DesktopMiniPlayerArguments.fromJson(Map<String, dynamic> json) {
    final index = intValue(json['initialIndex']);
    final position = (json['initialPosition'] as num?)?.toDouble() ?? 0;
    return DesktopMiniPlayerArguments(
      detail: DramaDetail.fromJson(
        Map<String, dynamic>.from(json['detail'] as Map),
      ),
      initialIndex: index,
      initialPosition: position.isFinite && position > 0 ? position : 0,
      authorizedSession: json['authorizedSession'] == true,
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
    'authorizedSession': authorizedSession,
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
  required bool authorizedSession,
}) async {
  final arguments = DesktopMiniPlayerArguments(
    detail: detail,
    initialIndex: index,
    initialPosition: position,
    authorizedSession: authorizedSession,
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


/// Serializes shutdown of the independent PiP engine. The main window must not
/// resume its MPV instance until the mini-player has finished disposing its
/// own native player; closing the OS window is always the final step.
class DesktopMiniPlayerShutdown {
  Future<void>? _closing;

  Future<void> close({
    required Future<void> Function() disposePlayer,
    required Future<void> Function() notifyMainWindow,
    required Future<void> Function() closeWindow,
  }) => _closing ??= _run(
    disposePlayer: disposePlayer,
    notifyMainWindow: notifyMainWindow,
    closeWindow: closeWindow,
  );

  Future<void> _run({
    required Future<void> Function() disposePlayer,
    required Future<void> Function() notifyMainWindow,
    required Future<void> Function() closeWindow,
  }) async {
    await disposePlayer();
    await notifyMainWindow();
    await closeWindow();
  }
}
