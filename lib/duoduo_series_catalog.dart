import 'models.dart';

class _SeasonIdentity {
  const _SeasonIdentity(this.baseKey, this.number, this.unit, this.groupId);
  final String baseKey;
  final int number;
  final String unit;
  final String groupId;
}

class _SeasonFamily {
  _SeasonFamily(this.baseKey, this.unit);
  final String baseKey;
  final String unit;
  final Map<int, List<int>> members = {};
  final Set<String> observedBaseKeys = {};
}

String _seriesTitleKey(String value) => value
    .replaceAll('（', '(')
    .replaceAll('）', ')')
    .replaceAll('［', '[')
    .replaceAll('］', ']')
    .replaceAll('　', ' ')
    .trim()
    .replaceAll(RegExp(r'\s+'), '')
    .toLowerCase();

int _chineseSeasonNumber(String raw) {
  final numeric = int.tryParse(raw);
  if (numeric != null) return numeric > 0 && numeric <= 999 ? numeric : 0;
  const digits = <String, int>{
    '零': 0, '〇': 0, '一': 1, '二': 2, '三': 3, '四': 4,
    '五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
  };
  var total = 0, current = 0;
  for (final rune in raw.runes) {
    final character = String.fromCharCode(rune);
    if (character == '十') {
      total += (current == 0 ? 1 : current) * 10;
      current = 0;
    } else if (character == '百') {
      if (current == 0) return 0;
      total += current * 100;
      current = 0;
    } else {
      final digit = digits[character];
      if (digit == null) return 0;
      current = digit;
    }
  }
  final result = total + current;
  return result > 0 && result <= 999 ? result : 0;
}

_SeasonIdentity? _seasonIdentity(Drama drama) {
  final title = drama.title.trim().replaceAll(RegExp(r'\s+'), ' ');
  final chinese = RegExp(
    r'^(.+?)\s*第\s*([0-9]{1,3}|[零〇一二三四五六七八九十百]+)\s*(季|部)$',
  ).firstMatch(title);
  if (chinese != null) {
    final base = chinese.group(1)!.trim();
    final number = _chineseSeasonNumber(chinese.group(2)!);
    final unit = chinese.group(3)!;
    if (base.isEmpty || number == 0) return null;
    final key = _seriesTitleKey(base);
    return _SeasonIdentity(key, number, unit,
        drama.seriesGroupId.isNotEmpty ? drama.seriesGroupId : 'duoduo-series:$key:$unit');
  }
  final english = RegExp(
    r'^(.+?)\s+(?:season\s*([0-9]{1,3})|s([0-9]{1,3}))$',
    caseSensitive: false,
  ).firstMatch(title);
  if (english != null) {
    final base = english.group(1)!.trim();
    final number = int.tryParse(english.group(2) ?? english.group(3) ?? '') ?? 0;
    if (base.isEmpty || number < 1 || number > 999) return null;
    final key = _seriesTitleKey(base);
    return _SeasonIdentity(key, number, '季',
        drama.seriesGroupId.isNotEmpty ? drama.seriesGroupId : 'duoduo-series:$key:季');
  }
  if (drama.seriesGroupId.isNotEmpty &&
      drama.seriesSeasonNumber > 0 &&
      drama.seriesSeasonUnit.isNotEmpty) {
    return _SeasonIdentity(
      _seriesTitleKey(title), drama.seriesSeasonNumber,
      drama.seriesSeasonUnit, drama.seriesGroupId,
    );
  }
  return null;
}

/// Merge exact, explicitly numbered Duoduo seasons into one catalogue card.
/// The representative is season one where available, otherwise the lowest
/// confirmed season. Ambiguous IDs or mixed season/part units are not merged.
List<Drama> collapseDuoduoSeriesCatalogItems(Iterable<Drama> sourceItems) {
  final items = sourceItems.toList();
  if (items.length < 2) return items;
  final families = <String, _SeasonFamily>{};
  final unitsByBase = <String, Set<String>>{};
  final unsuffixedByBase = <String, List<int>>{};

  for (var index = 0; index < items.length; index++) {
    final drama = items[index];
    if (drama.source != 'duoduo') continue;
    final identity = _seasonIdentity(drama);
    if (identity == null) {
      if (drama.seriesGroupId.isEmpty) {
        final key = _seriesTitleKey(drama.title);
        if (key.isNotEmpty) unsuffixedByBase.putIfAbsent(key, () => []).add(index);
      }
      continue;
    }
    unitsByBase.putIfAbsent(identity.baseKey, () => <String>{}).add(identity.unit);
    final family = families.putIfAbsent(identity.groupId,
        () => _SeasonFamily(identity.baseKey, identity.unit));
    family.observedBaseKeys.add(identity.baseKey);
    family.members.putIfAbsent(identity.number, () => []).add(index);
  }

  final membership = <int, String>{};
  final representatives = <String, Drama>{};
  for (final entry in families.entries) {
    final family = entry.value;
    if (family.observedBaseKeys.length != 1 ||
        (unitsByBase[family.baseKey]?.length ?? 0) != 1) continue;
    final indexes = <int, int>{};
    var ambiguous = false;
    for (final season in family.members.entries) {
      if (season.value.length != 1) {
        ambiguous = true;
        break;
      }
      indexes[season.key] = season.value.single;
    }
    if (ambiguous) continue;
    if (!indexes.containsKey(1)) {
      final unsuffixed = unsuffixedByBase[family.baseKey] ?? const <int>[];
      if (unsuffixed.length == 1) indexes[1] = unsuffixed.single;
    }
    if (indexes.length < 2) continue;

    final ordered = indexes.keys.toList()..sort();
    final firstNumber = ordered.first;
    final firstIndex = indexes[firstNumber]!;
    final chosen = items[firstIndex];
    final displayTitle = chosen.title
        .replaceFirst(RegExp(r'\s*第\s*([0-9]{1,3}|[零〇一二三四五六七八九十百]+)\s*(季|部)$'), '')
        .replaceFirst(RegExp(r'\s+(?:season|s)\s*[0-9]{1,3}$', caseSensitive: false), '');
    representatives[entry.key] = chosen.withSeriesIdentity(
      groupId: entry.key, seasonNumber: firstNumber,
      seasonUnit: family.unit, displayTitle: displayTitle,
    );
    for (final index in indexes.values) membership[index] = entry.key;
  }

  if (representatives.isEmpty) return items;
  final result = <Drama>[];
  final emitted = <String>{};
  for (var index = 0; index < items.length; index++) {
    final group = membership[index];
    if (group == null) {
      result.add(items[index]);
    } else if (emitted.add(group)) {
      result.add(representatives[group]!);
    }
  }
  return result;
}
