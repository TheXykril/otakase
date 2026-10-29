import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/catalog.dart';

void main() {
  test('unset search filters are left out of the AniList request', () async {
    late Map<String, dynamic> sent;
    final catalog = Catalog(
      client: MockClient((r) async {
        sent = (jsonDecode(r.body) as Map)['variables'] as Map<String, dynamic>;
        return http.Response(
          jsonEncode({
            'data': {
              'Page': {
                'pageInfo': {'hasNextPage': false},
                'media': [],
              },
            },
          }),
          200,
        );
      }),
    );
    await catalog.search('frieren', adult: true);
    expect(sent['q'], 'frieren');
    expect(sent.containsKey('format'), isFalse);
    expect(sent.containsKey('adult'), isFalse);
    expect(sent.values, isNot(contains(null)));
  });
}
