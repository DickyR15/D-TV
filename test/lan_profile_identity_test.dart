import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/lan_models.dart';

void main() {
  test('LAN profile identity requires matching ID and user name', () {
    expect(
      lanProfileIdentityMatches(
        remoteProfileId: 'default',
        localProfileId: 'default',
        remoteUser: 'Dicky',
        localUser: 'Dicky',
      ),
      isTrue,
    );
    expect(
      lanProfileIdentityMatches(
        remoteProfileId: 'profile-123',
        localProfileId: 'profile-123',
        remoteUser: 'Family',
        localUser: ' family ',
      ),
      isTrue,
    );
    expect(
      lanProfileIdentityMatches(
        remoteProfileId: 'default',
        localProfileId: 'default',
        remoteUser: 'Guest',
        localUser: 'Dicky',
      ),
      isFalse,
    );
    expect(
      lanProfileIdentityMatches(
        remoteProfileId: 'profile-123',
        localProfileId: 'profile-456',
        remoteUser: 'Dicky',
        localUser: 'Dicky',
      ),
      isFalse,
    );
    expect(
      lanProfileIdentityMatches(
        remoteProfileId: null,
        localProfileId: 'default',
        remoteUser: 'Dicky',
        localUser: 'Dicky',
      ),
      isFalse,
    );
    expect(
      lanProfileIdentityMatches(
        remoteProfileId: 'default',
        localProfileId: 'default',
        remoteUser: '',
        localUser: 'Dicky',
      ),
      isFalse,
    );
  });
}
