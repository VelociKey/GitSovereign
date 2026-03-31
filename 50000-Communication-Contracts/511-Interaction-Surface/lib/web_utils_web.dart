import 'package:web/web.dart' as web;
import 'dart:js_interop';
@JS()
external void _noop();
void initWeb() { () => _noop(); }
String getQueryTier() { try { final search = web.window.location.search; final params = Uri.parse(search).queryParameters; return params['tier']?.toLowerCase() ?? ''; } catch (e) { return ''; } }
