/// Identifies the TCL 75QM8B / C755 television family from Android build
/// properties. Do not apply this profile to Android TV devices generically:
/// onn. 4K Pro and other televisions keep their original playback defaults.
bool isTcl75Qm8bTelevision({
  required bool television,
  String manufacturer = '',
  String brand = '',
  String model = '',
  String device = '',
  String product = '',
  String fingerprint = '',
}) {
  if (!television) return false;
  final vendorIdentity = '$manufacturer $brand $model $device $product $fingerprint'
      .toLowerCase();
  final isTcl = vendorIdentity.contains('tcl');
  final isTargetModel =
      vendorIdentity.contains('75qm8b') || vendorIdentity.contains('c755');
  return isTcl && isTargetModel;
}
