import 'dart:io';

import 'package:test/test.dart';

import '../../plugins/rust_api/hook/bindgen.dart';

void main() {
  late Directory root;

  setUp(() {
    root = Directory.systemTemp.createTempSync('flclash-bindgen-');
  });

  tearDown(() {
    root.deleteSync(recursive: true);
  });

  String library(String folder) {
    final directory = Directory('${root.path}/$folder')
      ..createSync(recursive: true);
    File('${directory.path}/libclang.so.18').writeAsStringSync('');
    return directory.path;
  }

  test('uses host libclang when the Android NDK has none', () {
    final host = library('host');

    expect(
      bindgenEnvironment(
        compiler: File('${root.path}/ndk/bin/clang').uri,
        libclangPath: host,
      ),
      {'LIBCLANG_PATH': host},
    );
  });

  test('explicit libclang takes precedence over the NDK', () {
    final host = library('host');
    library('ndk/lib');

    expect(
      bindgenEnvironment(
        compiler: File('${root.path}/ndk/bin/clang').uri,
        libclangPath: host,
      ),
      {'LIBCLANG_PATH': host},
    );
  });

  for (final folder in ['lib', 'lib64']) {
    test('discovers NDK libclang in $folder', () {
      final ndk = library('ndk/$folder');

      expect(
        bindgenEnvironment(
          compiler: File('${root.path}/ndk/bin/clang').uri,
          libclangPath: null,
        ),
        {'LIBCLANG_PATH': ndk},
      );
    });
  }

  test('reports an invalid explicit libclang path', () {
    library('ndk/lib');

    expect(
      () => bindgenEnvironment(
        compiler: File('${root.path}/ndk/bin/clang').uri,
        libclangPath: '${root.path}/missing',
      ),
      throwsStateError,
    );
  });

  test('reports a missing NDK libclang with setup instructions', () {
    expect(
      () => bindgenEnvironment(
        compiler: File('${root.path}/ndk/bin/clang').uri,
        libclangPath: null,
      ),
      throwsA(
        isA<StateError>().having(
          (error) => error.message,
          'message',
          contains('set LIBCLANG_PATH'),
        ),
      ),
    );
  });

  test('leaves compiler-free discovery to bindgen', () {
    expect(bindgenEnvironment(compiler: null, libclangPath: null), isEmpty);
  });
}
