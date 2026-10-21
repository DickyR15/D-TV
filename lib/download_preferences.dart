class DownloadPreferences {
  const DownloadPreferences({this.quality = 0, this.includeVip = false});
  static const qualities = [0, 1080, 720, 480];
  final int quality;
  final bool includeVip;
  String get qualityLabel => quality == 0 ? '自動 · 優先高畫質' : '${quality}P';

  DownloadPreferences copyWith({int? quality, bool? includeVip}) =>
      DownloadPreferences(
        quality: quality ?? this.quality,
        includeVip: includeVip ?? this.includeVip,
      );

  factory DownloadPreferences.fromJson(Map<String, dynamic> json) {
    final quality = json['quality'] ?? 0;
    final includeVip = json['includeVip'] ?? false;
    if (quality is! int ||
        !qualities.contains(quality) ||
        includeVip is! bool) {
      throw const FormatException('下載偏好無效');
    }
    return DownloadPreferences(quality: quality, includeVip: includeVip);
  }

  Map<String, dynamic> toJson() => {
    'quality': quality,
    'includeVip': includeVip,
  };
}
