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

  test(
    'decade, rating and film sorts reach AniList as anime filters',
    () async {
      late Map<String, dynamic> sent;
      late String query;
      final catalog = Catalog(
        client: MockClient((r) async {
          final body = jsonDecode(r.body) as Map;
          sent = body['variables'] as Map<String, dynamic>;
          query = body['query'] as String;
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
      await catalog.search('', adult: false, decade: 2010, minScore: 70);
      expect(sent['from'], 20100000);
      expect(sent['to'], 20200000);
      expect(sent['score'], 69);
      expect(query, contains('averageScore_greater:\$score'));
      // A single year wins over the decade.
      await catalog.search('', adult: false, year: 2015, decade: 2010);
      expect(sent.containsKey('from'), isFalse);
      expect(sent['year'], 2015);
      await catalog.search('', adult: false, sort: 'ANTICIPATED');
      expect(sent['status'], 'NOT_YET_RELEASED');
      expect(query, contains('POPULARITY_DESC'));
      await catalog.search('', adult: false, sort: 'WATCHED');
      expect(query, contains('TRENDING_DESC'));
    },
  );
}
