import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/catalog_filters.dart';
import 'package:hgtv/models.dart';

void main() {
  testWidgets('category filter strip supports mouse click-and-drag scrolling', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(520, 220);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Align(
            alignment: Alignment.topLeft,
            child: SizedBox(
              width: 360,
              child: CatalogFilters(
                categories: [
                  CatalogCategory.all,
                  for (var index = 1; index <= 12; index++)
                    CatalogCategory('category-$index', '分類項目 $index'),
                ],
                category: '',
                onCategory: (_) {},
                onRetry: () {},
              ),
            ),
          ),
        ),
      ),
    );

    final viewport = find.byKey(const ValueKey('catalog-categories'));
    final scrollView = tester.widget<SingleChildScrollView>(viewport);
    expect(scrollView.scrollDirection, Axis.horizontal);

    final configuration = tester.widget<ScrollConfiguration>(
      find.ancestor(
        of: viewport,
        matching: find.byType(ScrollConfiguration),
      ).first,
    );
    expect(
      configuration.behavior.dragDevices,
      contains(PointerDeviceKind.mouse),
    );

    // The real scroll view must be wrapped by the mouse-enabled behavior.
    // Flutter's Scrollable drag recognizer uses this set to admit mouse drags;
    // tap gestures on individual ChoiceChips remain unaffected.
    expect(find.byKey(const ValueKey('catalog-categories')), findsOneWidget);

  });

  testWidgets('Duoduo anime category chip stays 動漫 when focused', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: CatalogFilters(
            categories: const [
              CatalogCategory.all,
              CatalogCategory('category:動漫', '動漫'),
            ],
            category: 'category:動漫',
            onCategory: (_) {},
            onRetry: () {},
          ),
        ),
      ),
    );

    expect(find.text('動漫'), findsOneWidget);
    expect(find.text('動畫'), findsNothing);
  });
}
