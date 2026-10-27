import 'dart:convert';

import 'package:flutter/services.dart';

class CoreFailure implements Exception {
  const CoreFailure(this.code, this.message);
  final String code;
  final String message;
  @override
  String toString() => '$code: $message';
}

class CoreClient {
  static const channel = MethodChannel('otakase/core');
  Future<bool> isTV() async =>
      await channel.invokeMethod<bool>('device') ?? false;

  Future<Map<String, dynamic>> request(
    Map<String, dynamic> input, {
    required void Function(int) onHandle,
  }) async {
    final id = await channel.invokeMethod<int>(
      'start',
      jsonEncode({'version': 1, ...input}),
    );
    if (id == null) {
      throw const CoreFailure('invalid_handle', 'No request handle.');
    }
    onHandle(id);
    final raw = await channel.invokeMethod<String>('result', id);
    final response = jsonDecode(raw!) as Map<String, dynamic>;
    if (response['error'] case final Map<String, dynamic> error) {
      throw CoreFailure(error['code'] as String, error['message'] as String);
    }
    return response['data'] as Map<String, dynamic>;
  }

  Future<void> cancel(int id) => channel.invokeMethod<void>('cancel', id);
}

class ResolvedStream {
  ResolvedStream.fromJson(Map<String, dynamic> json)
    : url = json['url'] as String,
      headers = Map<String, String>.from(json['headers'] as Map),
      referrer = json['referrer'] as String,
      subtitles = (json['subtitles'] as List).cast<Map<String, dynamic>>();
  final String url;
  final Map<String, String> headers;
  final String referrer;
  final List<Map<String, dynamic>> subtitles;
}
