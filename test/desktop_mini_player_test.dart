import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/desktop_mini_player_bridge.dart';
import 'package:hgtv/models.dart';

void main() {
  const drama = Drama(
    id: 'duoduo:1192',
    source: 'duoduo',
    sourceId: '1192',
    title: '奉子成婚',
    cover: 'https://images.example.test/shotgun-marriage.jpg',
  );

  test('mini-player arguments preserve exact detail and playback resume state', () {
    final detail = DramaDetail(
      drama,
      [
        Episode(
          {
            'id': 'episode-1',
            'title': '第1集',
            'currentEpisode': 1,
            'playLine': '播放線一',
            'url': 'https://media.example.test/episode-1.m3u8',
          },
          1,
        ),
        Episode(
          {
            'id': 'episode-2',
            'title': '第2集',
            'currentEpisode': 2,
            'playLine': '播放線二',
            'url': 'https://media.example.test/episode-2.m3u8',
          },
          2,
        ),
      ],
      warning: '測試警告',
    );
    final original = DesktopMiniPlayerArguments(
      detail: detail,
      initialIndex: 1,
      initialPosition: 125.5,
      authorizedSession: true,
    );

    final restored = DesktopMiniPlayerArguments.tryParse(
      jsonEncode(original.toJson()),
    );
    expect(restored, isNotNull);
    expect(restored!.detail.drama.id, drama.id);
    expect(restored.detail.drama.title, drama.title);
    expect(restored.detail.episodes, hasLength(2));
    expect(restored.detail.episodes.last.id, 'episode-2');
    expect(restored.detail.warning, '測試警告');
    expect(restored.initialIndex, 1);
    expect(restored.initialPosition, 125.5);
    expect(restored.authorizedSession, isTrue);
  });

  test('mini-player parser rejects the main window and malformed arguments', () {
    expect(DesktopMiniPlayerArguments.tryParse(''), isNull);
    expect(DesktopMiniPlayerArguments.tryParse('{broken'), isNull);
    expect(
      DesktopMiniPlayerArguments.tryParse('{"businessId":"unknown"}'),
      isNull,
    );
    expect(
      DesktopMiniPlayerArguments.tryParse('{"businessId":"dtv_mini_player"}'),
      isNull,
    );
  });

  test('closing the mini-player before initialization blocks late player attachment', () {
    final gate = DesktopMiniPlayerStartupGate();
    expect(gate.canAttachPlayer, isTrue);

    gate.close();

    expect(gate.canAttachPlayer, isFalse);
    gate.close();
    expect(gate.canAttachPlayer, isFalse);
  });

  test('restore disposes the PiP player before resuming the main window', () async {
    final shutdown = DesktopMiniPlayerShutdown();
    final disposeGate = Completer<void>();
    final events = <String>[];
    var disposeCalls = 0;
    var notifyCalls = 0;
    var closeCalls = 0;

    final first = shutdown.close(
      disposePlayer: () async {
        disposeCalls++;
        events.add('dispose-start');
        await disposeGate.future;
        events.add('dispose-complete');
      },
      notifyMainWindow: () async {
        notifyCalls++;
        events.add('notify-main');
      },
      closeWindow: () async {
        closeCalls++;
        events.add('close-window');
      },
    );
    final second = shutdown.close(
      disposePlayer: () async {
        disposeCalls++;
      },
      notifyMainWindow: () async {
        notifyCalls++;
      },
      closeWindow: () async {
        closeCalls++;
      },
    );

    expect(identical(first, second), isTrue);
    expect(events, ['dispose-start']);
    expect(notifyCalls, 0);
    expect(closeCalls, 0);

    disposeGate.complete();
    await first;

    expect(events, [
      'dispose-start',
      'dispose-complete',
      'notify-main',
      'close-window',
    ]);
    expect(disposeCalls, 1);
    expect(notifyCalls, 1);
    expect(closeCalls, 1);
  });

  test('a failed PiP disposal never starts the main player', () async {
    final shutdown = DesktopMiniPlayerShutdown();
    final events = <String>[];

    await expectLater(
      shutdown.close(
        disposePlayer: () async {
          events.add('dispose-start');
          throw StateError('native MPV did not stop');
        },
        notifyMainWindow: () async {
          events.add('notify-main');
        },
        closeWindow: () async {
          events.add('close-window');
        },
      ),
      throwsA(isA<StateError>()),
    );

    expect(events, ['dispose-start']);
  });

  test('mini-player parser sanitizes an invalid saved position', () {
    final encoded = jsonEncode({
      'businessId': DesktopMiniPlayerArguments.businessId,
      'detail': {
        'drama': drama.toJson(),
        'chapters': [
          {'title': '第1集', 'currentEpisode': 1},
        ],
      },
      'initialIndex': 0,
      'initialPosition': -10,
      'authorizedSession': true,
    });
    final restored = DesktopMiniPlayerArguments.tryParse(encoded);
    expect(restored, isNotNull);
    expect(restored!.initialPosition, 0);
  });
}
