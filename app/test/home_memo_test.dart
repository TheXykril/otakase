import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/ui/home_page.dart';

import 'home_discovery_test.dart' show HomeCatalog, launchHome;
import 'widget_test.dart' as fixture;

// Home works out Continue watching when the history or lists change, not on
// every rebuild.
void main() {
  testWidgets('Continue watching is not rebuilt for unrelated redraws', (
    tester,
  ) async {
    final services = await launchHome(tester, catalog: HomeCatalog());
    final start = HomePage.continuationBuilds;
    tester.view.physicalSize = tester.view.physicalSize * 1.1;
    addTearDown(tester.view.resetPhysicalSize);
    await tester.pumpAndSettle();
    expect(HomePage.continuationBuilds, start);
    await services.store.progress(fixture.anime, 5, 720, 1440);
    await tester.pumpAndSettle();
    expect(HomePage.continuationBuilds, start + 1);
  });
}
