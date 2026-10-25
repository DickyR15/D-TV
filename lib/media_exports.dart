part of 'media_library.dart';

bool _validShowFolder(String folder) {
  final components = folder.split(RegExp(r'[/\\]'));
  return !path.isAbsolute(folder) &&
      components.length == 2 &&
      components.first == 'exports' &&
      components.last.isNotEmpty &&
      !{'.', '..'}.contains(components.last) &&
      !components.last.contains(':');
}

String embySpecialNfo(LocalMediaItem item, int number) =>
    '<?xml version="1.0" encoding="utf-8"?>\n<episodedetails>'
    '<title>${xmlText(item.drama.title)} · 合併第 ${item.episodes.first}–${item.episodes.last} 集</title>'
    '<showtitle>${xmlText(item.drama.title)}</showtitle><season>0</season><episode>$number</episode>'
    '<displayseason>1</displayseason><displayepisode>${item.episodes.last + 1}</displayepisode>'
    '<plot>${xmlText('合併第 ${item.episodes.first}–${item.episodes.last} 集，共 ${item.episodes.length} 集。\n${item.drama.description}')}</plot>'
    '<uniqueid type="zhenguojian" default="true">${xmlText(item.special ? item.id : 'special-${item.id}')}</uniqueid></episodedetails>\n';

extension MediaExportOperations on MediaLibrary {
  Future<String> _showDirectory(Drama drama) async {
    final existing = _showFolders[drama.id];
    if (existing != null) return existing;
    final hash = sha256
        .convert(utf8.encode(drama.id))
        .toString()
        .substring(0, 12);
    final folder = path.join(
      'exports',
      '${_safeName(drama.title)} [${drama.source}-$hash]',
    );
    if (_showFolders.values.contains(folder)) {
      throw AppFailure('匯出目錄與另一部劇衝突，請檢查本地媒體記錄');
    }
    _showFolders[drama.id] = folder;
    try {
      _check();
      await _save();
    } catch (_) {
      _showFolders.remove(drama.id);
      rethrow;
    }
    return folder;
  }

  Future<void> _writeShowMetadata(Drama drama, Directory showDirectory) async {
    // Poster file export is disabled; keep metadata compatible with any
    // poster that already exists in the show folder without fetching/copying
    // new image files as part of media export.
    final hasPoster = await File(
      path.join(showDirectory.path, 'poster.jpg'),
    ).exists();
    _check();
    await _writeText(
      File(path.join(showDirectory.path, 'tvshow.nfo')),
      embyShowNfo(drama, localPoster: hasPoster),
    );
  }

  Future<void> _copyOutput(File source, File target, int bytes) async {
    final output = await target.open(mode: FileMode.write);
    var copied = 0;
    try {
      await for (final block in source.openRead()) {
        _check();
        await output.writeFrom(block);
        copied += block.length;
        progress = bytes > 0 ? .4 * copied / bytes : 0;
        notifyListeners();
      }
      await output.flush();
    } finally {
      await output.close();
    }
    _check();
    if (copied != bytes) throw AppFailure('復製成品時檔案發生變化，請重試；原成品保留');
  }

  Future<LocalMediaItem> exportMerged(LocalMediaItem selected) => _task(
    '準備匯出特別篇',
    (temporary) async {
      final source = _items
          .where((item) => item.id == selected.id && item.merged)
          .firstOrNull;
      if (source == null || source.episodes.isEmpty) {
        throw AppFailure('合併成品不存在，請重新整理後重試');
      }
      final file = File(fileFor(source));
      if (!await file.exists() || await file.length() != source.bytes) {
        throw AppFailure('合併成品缺失或不完整，請重新合併');
      }
      final drama = repository.catalogUpdates.current(source.drama);
      final folder = await _showDirectory(drama);
      final key = '${source.drama.id}\u0000${source.id}';
      var number = _specialNumbers[key];
      if (number == null) {
        final prefix = '${source.drama.id}\u0000';
        var highest = 0;
        for (final entry in _specialNumbers.entries.where(
          (entry) => entry.key.startsWith(prefix),
        )) {
          if (entry.value > highest) highest = entry.value;
        }
        number = highest + 1;
        if (number > 100000) throw AppFailure('特別篇數量已達上限');
        _specialNumbers[key] = number;
        try {
          await _save();
        } catch (_) {
          _specialNumbers.remove(key);
          rethrow;
        }
      }
      final relative = path.join(
        folder,
        'Season 00',
        'S00E${number.toString().padLeft(3, '0')}.mkv',
      );
      final target = File(path.join(root!, relative));
      final staged = File(path.join(temporary.path, 'special.mkv'));
      await _copyOutput(file, staged, source.bytes);
      final checked = await executor.probe(staged.path);
      verifyMediaDuration(checked, source.duration);
      await _decodeOutput(staged.path, checked.duration, base: .4, weight: .55);
      await target.parent.create(recursive: true);
      _check();
      await staged.rename(target.path);
      final item = LocalMediaItem(
        id: 'special-${source.id}',
        drama: drama,
        file: relative,
        kind: 'special',
        episodes: source.episodes,
        duration: checked.duration,
        bytes: source.bytes,
        created: DateTime.now(),
        sourceVersion: '${source.id}-${source.bytes}-${source.sourceVersion}',
        decodeVerified: true,
        specialNumber: number,
      );
      await _writeShowMetadata(drama, target.parent.parent);
      await _writeText(
        File(path.setExtension(target.path, '.nfo')),
        embySpecialNfo(item, number),
      );
      final previous = List<LocalMediaItem>.of(_items);
      _items.removeWhere((entry) => entry.id == item.id);
      _items.add(item);
      try {
        _check();
        await _save();
      } catch (_) {
        _items = previous;
        rethrow;
      }
      return item;
    },
    sources: {selected.drama.source},
  );
}
