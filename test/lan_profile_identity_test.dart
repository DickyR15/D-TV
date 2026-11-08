import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/lan_models.dart';

void main() {
  test('LAN data sync requires an exact matching local profile ID', () {
    expect(lanProfileIdentityMatches('default', 'default'), isTrue);
    expect(lanProfileIdentityMatches('profile-123', 'profile-123'), isTrue);
    expect(lanProfileIdentityMatches('profile-123', 'profile-456'), isFalse);
    expect(lanProfileIdentityMatches(null, 'default'), isFalse);
    expect(lanProfileIdentityMatches('', 'default'), isFalse);
  });
}
