import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/follow_state.dart';
import 'package:hgtv/models.dart';

void main() {
  final drama = Drama(
    id: 'hongguo:test-resume',
    source: 'hongguo',
    title: '測試短劇',
    episodes: 5,
  );
  final episodes = List<Episode>.generate(
    5,
    (index) => Episode(
      <String, dynamic>{'currentEpisode': index + 1},
      index + 1,
    ),
  );

  test('new drama defaults to the first episode', () {
    expect(resumeEpisodeIndex(episodes, null), 0);
  });

  test('unfinished viewing record resumes the same episode', () {
    final watch = WatchEntry(
      drama: drama,
      episode: 3,
      position: 42,
      duration: 120,
      updatedAt: DateTime.utc(2026, 10, 23),
    );
    expect(resumeEpisodeIndex(episodes, watch), 2);
    expect(episodes[resumeEpisodeIndex(episodes, watch)].number, 3);
  });

  test('finished episode advances to the next available episode', () {
    final watch = WatchEntry(
      drama: drama,
      episode: 3,
      position: 120,
      duration: 120,
      updatedAt: DateTime.utc(2026, 10, 23),
    );
    expect(resumeEpisodeIndex(episodes, watch), 3);
    expect(episodes[resumeEpisodeIndex(episodes, watch)].number, 4);
  });
}
