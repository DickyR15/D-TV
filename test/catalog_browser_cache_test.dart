import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/catalog_browser.dart';
import 'package:hgtv/models.dart';

import 'fixtures.dart';

class _DelayedCacheRepository extends FixtureRepository {
  final pending = <Completer<CatalogPage>>[];

  @override
  Future<CatalogPage> cached(String source, {String category = ''}) {
    final request = Completer<CatalogPage>();
    pending.add(request);
    return request.future;
  }
}

void main() {
  test('a stale cache read cannot evict a newer read after cache clear', () async {
    final repository = _DelayedCacheRepository();
    final browser = CatalogBrowser(repository);

    final previousRead = browser.cached('hongguo');
    expect(repository.pending, hasLength(1));

    browser.clearMemoryCache();

    final currentRead = browser.cached('hongguo');
    expect(repository.pending, hasLength(2));

    repository.pending[0].complete(CatalogPage([]));
    await previousRead;

    final reusedRead = browser.cached('hongguo');
    expect(
      repository.pending,
      hasLength(2),
      reason: 'the completed old Future must not remove the replacement entry',
    );
    expect(identical(reusedRead, currentRead), isTrue);

    repository.pending[1].complete(CatalogPage([]));
    await currentRead;
  });
}
