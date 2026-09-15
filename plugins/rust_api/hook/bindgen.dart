import 'dart:io';

Map<String, String> bindgenEnvironment({
  required Uri? compiler,
  required String? libclangPath,
}) {
  if (libclangPath != null && libclangPath.isNotEmpty) {
    final directory = Directory(libclangPath);
    if (!_hasLibclang(directory)) {
      throw StateError('LIBCLANG_PATH contains no libclang: $libclangPath');
    }
    return {'LIBCLANG_PATH': directory.path};
  }
  if (compiler == null) return const {};

  final llvmRoot = File.fromUri(compiler).parent.parent;
  for (final name in const ['lib', 'lib64']) {
    final directory = Directory(
      '${llvmRoot.path}${Platform.pathSeparator}$name',
    );
    if (_hasLibclang(directory)) {
      return {'LIBCLANG_PATH': directory.path};
    }
  }
  throw StateError(
    'No libclang under ${llvmRoot.path}; install libclang and set LIBCLANG_PATH',
  );
}

bool _hasLibclang(Directory directory) {
  return directory.existsSync() &&
      directory.listSync().any(
        (entity) =>
            File(entity.path).existsSync() &&
            entity.path
                .split(Platform.pathSeparator)
                .last
                .startsWith('libclang.'),
      );
}
