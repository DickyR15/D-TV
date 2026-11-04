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
    final scrollable = find.descendant(
      of: viewport,
      matching: find.byType(Scrollable),
    ).first;
    final position = tester.state<ScrollableState>(scrollable).position;
    expect(position.maxScrollExtent, greaterThan(0));
    final before = position.pixels;

    final gesture = await tester.startGesture(
      tester.getCenter(viewport),
      kind: PointerDeviceKind.mouse,
    );
    await gesture.moveBy(const Offset(-180, 0));
    await tester.pumpAndSettle();
    await gesture.up();
    await tester.pumpAndSettle();

    expect(position.pixels, greaterThan(before));
  });
}
