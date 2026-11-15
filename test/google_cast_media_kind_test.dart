import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/google_cast.dart';

void main() {
  test('Google Cast identifies HLS streams', () {
    expect(
      googleCastMediaKindForUrl('https://media.example.test/episode.m3u8'),
      GoogleCastMediaKind.hls,
    );
    expect(
      googleCastMediaKindForUrl(
        'https://media.example.test/manifest',
        contentType: 'application/vnd.apple.mpegurl',
      ),
      GoogleCastMediaKind.hls,
    );
  });

  test('Google Cast identifies common direct video formats', () {
    expect(
      googleCastMediaKindForUrl('https://media.example.test/movie.mp4'),
      GoogleCastMediaKind.mp4,
    );
    expect(
      googleCastMediaKindForUrl('https://media.example.test/movie.mkv'),
      GoogleCastMediaKind.mkv,
    );
    expect(
      googleCastMediaKindForUrl('https://media.example.test/segment.ts'),
      GoogleCastMediaKind.mpegTs,
    );
  });

  test('unknown extension defaults to MP4 only when no content type is known', () {
    expect(
      googleCastMediaKindForUrl('https://media.example.test/play'),
      GoogleCastMediaKind.mp4,
    );
  });
}
