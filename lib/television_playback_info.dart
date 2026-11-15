String? televisionNetworkSpeedLabel(
  String rawBytesPerSecond, {
  required bool networkInput,
}) {
  if (!networkInput) return null;
  final bytesPerSecond = double.tryParse(rawBytesPerSecond.trim());
  if (bytesPerSecond == null ||
      !bytesPerSecond.isFinite ||
      bytesPerSecond < 0) {
    return null;
  }
  const kibibyte = 1024.0;
  const mebibyte = kibibyte * 1024.0;
  if (bytesPerSecond >= mebibyte) {
    return '${(bytesPerSecond / mebibyte).toStringAsFixed(1)} MB/s';
  }
  if (bytesPerSecond >= kibibyte) {
    return '${(bytesPerSecond / kibibyte).toStringAsFixed(1)} KB/s';
  }
  return '${bytesPerSecond.round()} B/s';
}

String televisionClockLabel(DateTime value) {
  final hour = value.hour.toString().padLeft(2, '0');
  final minute = value.minute.toString().padLeft(2, '0');
  return '$hour:$minute';
}
