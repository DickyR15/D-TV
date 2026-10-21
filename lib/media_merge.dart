part of 'media_library.dart';

List<DownloadJob> continuousMergeJobs(List<DownloadJob> selected) {
  final jobs = List<DownloadJob>.of(selected)
    ..sort((a, b) => a.episode.number.compareTo(b.episode.number));
  if (jobs.length < 2 ||
      jobs.length > 10000 ||
      jobs.any((job) => !job.completed) ||
      jobs.map((job) => job.drama.id).toSet().length != 1 ||
      jobs.map((job) => job.episode.number).toSet().length != jobs.length) {
    throw AppFailure('請選擇同一部劇 2 至 10000 集已下載的影片，集數不能重複');
  }
  final missing = <int>[];
  for (var i = 1; i < jobs.length; i++) {
    for (
      var number = jobs[i - 1].episode.number + 1;
      number < jobs[i].episode.number && missing.length < 12;
      number++
    ) {
      missing.add(number);
    }
  }
  if (missing.isNotEmpty) {
    throw AppFailure(
      '分集不連續，缺少第 ${missing.join('、')}${missing.length == 12 ? '…' : ''} 集，請補齊後合併',
    );
  }
  return jobs;
}

String _mergeDigest(Object value) =>
    sha256.convert(utf8.encode(jsonEncode(value))).toString();

extension MediaMergeOperations on MediaLibrary {
  Future<LocalMediaItem> merge(List<DownloadJob> selected) async {
    final jobs = continuousMergeJobs(selected);
    return _task(
      '準備合併',
      (temporary) => _mergeFiles(
        jobs,
        temporary,
        'merged-${DateTime.now().microsecondsSinceEpoch}',
      ),
      sources: {jobs.first.drama.source},
    );
  }

  Future<LocalMediaItem> _mergeQueued(MergeQueueJob task) => _task(
    '恢復合併進度',
    (temporary) async {
      final existing = _items
          .where(
            (item) =>
                item.id == task.outputId && item.merged && item.decodeVerified,
          )
          .firstOrNull;
      if (existing != null &&
          existing.drama.id == task.drama.id &&
          listEquals(existing.episodes, task.episodes) &&
          await File(fileFor(existing)).exists() &&
          await File(fileFor(existing)).length() == existing.bytes) {
        return existing;
      }
      final current = {
        for (final job in await repository.downloads()) job.id: job,
      };
      _check();
      final jobs = <DownloadJob>[];
      for (final id in task.jobIds) {
        final job = current[id];
        if (job == null ||
            !job.completed ||
            _sourceVersion(job) != task.versions[id]) {
          throw AppFailure('原分集已刪除、重新下載或未完成，請重新建立合併任務；已儲存的處理進度保留');
        }
        jobs.add(job);
      }
      return _mergeFiles(continuousMergeJobs(jobs), temporary, task.outputId);
    },
    workspace: task.id,
    sources: {task.drama.source},
  );

  Future<void> _decodeOutput(
    String file,
    double duration, {
    double base = .9,
    double weight = .09,
  }) async {
    _check();
    status = '完整解碼校驗成品';
    notifyListeners();
    await executor.run(
      [
        '-xerror',
        '-err_detect',
        'explode',
        '-i',
        file,
        '-map',
        '0:v:0',
        '-map',
        '0:a:0?',
        '-sn',
        '-dn',
        '-f',
        'null',
        '-',
      ],
      duration: duration,
      progress: (value) {
        progress = base + weight * value;
        notifyListeners();
      },
    );
    _check();
  }

  Future<LocalMediaItem> _mergeFiles(
    List<DownloadJob> jobs,
    Directory temporary,
    String id,
  ) async {
    final sources = <Object>[];
    for (final job in jobs) {
      _check();
      final plan = await repository.localPlayback(job.drama, job.episode);
      _check();
      if (plan == null || !plan.local) {
        throw AppFailure('第 ${job.episode.number} 集沒有完整的本地檔案');
      }
      final stat = await File(plan.url).stat();
      sources.add([
        job.id,
        _sourceVersion(job),
        job.episode.number,
        path.basename(plan.url),
        stat.size,
        stat.modified.microsecondsSinceEpoch,
        _mergeDigest(plan.decryptionKey),
      ]);
    }
    final fingerprint = _mergeDigest(['merge-v3', sources]);
    final checkpointFile = File(path.join(temporary.path, 'checkpoint.json'));
    var checkpoint = <String, dynamic>{
      'fingerprint': fingerprint,
      'prepared': <String, dynamic>{},
      'normalized': <String, dynamic>{},
    };
    if (await checkpointFile.exists()) {
      if (await checkpointFile.length() > 8 * 1024 * 1024) {
        throw AppFailure('合併恢復記錄過大，已保留原檔案');
      }
      checkpoint = Map<String, dynamic>.from(
        jsonDecode(await checkpointFile.readAsString()) as Map,
      );
      if (checkpoint['fingerprint'] != fingerprint) {
        throw AppFailure('原分集或合併引數已改變，請取消此任務後重新加入；原影片保留');
      }
    }
    final originals = Map<String, dynamic>.from(
      checkpoint['prepared'] as Map? ?? {},
    );
    final parts = Map<String, dynamic>.from(
      checkpoint['normalized'] as Map? ?? {},
    );
    checkpoint['prepared'] = originals;
    checkpoint['normalized'] = parts;

    Future<void> saveCheckpoint() async {
      _check();
      await _writeText(checkpointFile, jsonEncode(checkpoint));
    }

    Future<MediaProbe?> restored(
      Map<String, dynamic> records,
      String key,
      String file,
    ) async {
      final raw = records[key];
      if (raw is! Map) return null;
      final stat = await File(file).stat();
      if (stat.type != FileSystemEntityType.file ||
          stat.size != raw['bytes'] ||
          stat.modified.microsecondsSinceEpoch != raw['modified']) {
        return null;
      }
      try {
        final saved = MediaProbe(
          Map<String, dynamic>.from(raw['probe'] as Map),
        );
        final probe = await executor.probe(file);
        _check();
        verifyMediaDuration(probe, saved.duration);
        if (probe.videoSignature != saved.videoSignature ||
            probe.audioSignature != saved.audioSignature) {
          return null;
        }
        return probe;
      } catch (_) {
        _check();
        return null;
      }
    }

    Future<void> record(
      Map<String, dynamic> records,
      String key,
      String file,
      MediaProbe probe,
    ) async {
      final stat = await File(file).stat();
      records[key] = {
        'bytes': stat.size,
        'modified': stat.modified.microsecondsSinceEpoch,
        'probe': probe.raw,
      };
      await saveCheckpoint();
    }

    final prepared = <String>[], probes = <MediaProbe>[];
    for (var i = 0; i < jobs.length; i++) {
      _check();
      final target = path.join(temporary.path, 'original-$i.mkv');
      var probe = await restored(originals, jobs[i].id, target);
      if (probe == null) {
        probe = await _prepare(
          jobs[i],
          target,
          .2 * i / jobs.length,
          .2 / jobs.length,
        );
        await record(originals, jobs[i].id, target, probe);
      }
      probes.add(probe);
      prepared.add(target);
    }
    final plan = MergePlan.create(probes);
    final planKey = _mergeDigest([
      plan.video.videoSignature,
      plan.audio?.audioSignature,
      plan.videoChanges,
      plan.audioChanges,
      plan.canonicalAac,
      plan.transportStream,
    ]);
    if (checkpoint['plan'] != planKey) {
      parts.clear();
      checkpoint['plan'] = planKey;
      await saveCheckpoint();
    }
    final normalized = <String>[];
    for (var i = 0; i < jobs.length; i++) {
      _check();
      status = plan.videoChanges[i]
          ? '統一第 ${jobs[i].episode.number} 集影片格式'
          : plan.audioChanges[i]
          ? '統一第 ${jobs[i].episode.number} 集音軌'
          : '保留第 ${jobs[i].episode.number} 集碼流';
      notifyListeners();
      final target = path.join(
        temporary.path,
        'part-$i.${plan.transportStream ? 'ts' : 'mkv'}',
      );
      var probe = await restored(parts, jobs[i].id, target);
      if (probe == null) {
        await executor.run(
          plan.normalizeArguments(prepared[i], target, probes[i], i),
          duration: probes[i].duration,
          progress: (value) {
            progress = .2 + .5 * (i + value) / jobs.length;
            notifyListeners();
          },
        );
        _check();
        probe = await executor.probe(target);
        _check();
        verifyMediaDuration(probe, probes[i].duration);
        await record(parts, jobs[i].id, target, probe);
      }
      normalized.add(target);
    }
    _check();
    status = '合併影片';
    notifyListeners();
    final list = File(path.join(temporary.path, 'concat.txt'));
    await list.writeAsString(
      'ffconcat version 1.0\n${normalized.map(concatFileLine).join('\n')}\n',
      flush: true,
    );
    final output = path.join(temporary.path, 'full.mkv');
    final duration = probes.fold<double>(
      0,
      (sum, probe) => sum + probe.duration,
    );
    await executor.run(
      [
        '-f',
        'concat',
        '-safe',
        '0',
        '-i',
        list.path,
        '-map',
        '0:v:0',
        '-map',
        '0:a:0?',
        '-c',
        'copy',
        '-avoid_negative_ts',
        'make_zero',
        output,
      ],
      duration: duration,
      progress: (value) {
        progress = .7 + value * .18;
        notifyListeners();
      },
    );
    _check();
    final checked = await executor.probe(output);
    verifyMediaDuration(checked, duration);
    await _decodeOutput(output, checked.duration);
    final relative = path.join('library', id, 'full.mkv');
    final target = File(path.join(root!, relative));
    await target.parent.create(recursive: true);
    _check();
    await File(output).rename(target.path);
    final item = LocalMediaItem(
      id: id,
      drama: jobs.first.drama,
      file: relative,
      kind: 'merged',
      episodes: jobs.map((job) => job.episode.number).toList(),
      duration: checked.duration,
      bytes: await target.length(),
      created: DateTime.now(),
      videoTranscodes: plan.videoTranscodes,
      audioTranscodes: plan.audioTranscodes,
      decodeVerified: true,
      sourceVersion: fingerprint,
    );
    final previous = List<LocalMediaItem>.of(_items);
    _items.removeWhere((item) => item.id == id);
    _items.add(item);
    try {
      _check();
      await _save();
    } catch (_) {
      _items = previous;
      rethrow;
    }
    return item;
  }
}
