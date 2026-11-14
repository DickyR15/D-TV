import 'models.dart';

class _SeasonIdentity {
  const _SeasonIdentity(this.baseKey, this.number, this.unit, this.groupId);
  final String baseKey;
  final int number;
  final String unit;
  final String groupId;
}

class _SeasonFamily {
  _SeasonFamily(this.unit);
  final String unit;
  final Set<String> baseKeys = {};
  final Map<int, List<int>> members = {};
}

String _seriesTitleKey(String value) => value
    .replaceAll('（', '(').replaceAll('）', ')')
    .replaceAll('［', '[').replaceAll('］', ']')
    .replaceAll('　', ' ').trim()
    .replaceAll(RegExp(r'\s+'), '').toLowerCase();

const _verifiedDuoduoSeriesAliases = <String, List<String>>{
  'chicago-med': ['芝加哥急救','芝加哥醫院','芝加哥医院','芝加哥医魂','芝加哥醫魂','芝加哥醫情','芝加哥医情','Chicago Med'],
};

String _verifiedAliasKey(String base) {
  final key = _seriesTitleKey(base);
  for (final entry in _verifiedDuoduoSeriesAliases.entries) {
    if (entry.value.any((alias) => _seriesTitleKey(alias) == key)) return entry.key;
  }
  return '';
}

String _canonicalGroupId(Drama drama, String base, String unit) {
  final alias = _verifiedAliasKey(base);
  if (alias.isNotEmpty) return 'duoduo-series:alias:$alias:$unit';
  return 'duoduo-series:${_seriesTitleKey(base)}:$unit';
}

int _chineseSeasonNumber(String raw) {
  final numeric = int.tryParse(raw);
  if (numeric != null) return numeric > 0 && numeric <= 999 ? numeric : 0;
  const digits = <String,int>{'零':0,'〇':0,'一':1,'二':2,'三':3,'四':4,'五':5,'六':6,'七':7,'八':8,'九':9};
  var total=0, current=0;
  for (final rune in raw.runes) {
    final character=String.fromCharCode(rune);
    if (character=='十') { total+=(current==0?1:current)*10; current=0; }
    else if (character=='百') { if(current==0)return 0; total+=current*100; current=0; }
    else { final digit=digits[character]; if(digit==null)return 0; current=digit; }
  }
  final result=total+current;
  return result>0 && result<=999 ? result : 0;
}

_SeasonIdentity? _seasonIdentity(Drama drama) {
  if (drama.source != 'duoduo') return null;
  final title=drama.title.trim().replaceAll(RegExp(r'\s+'),' ');
  final chinese=RegExp(r'^(.+?)\s*第\s*([0-9]{1,3}|[零〇一二三四五六七八九十百]+)\s*(季|部)$').firstMatch(title);
  if(chinese!=null){
    final base=chinese.group(1)!.trim(), number=_chineseSeasonNumber(chinese.group(2)!), unit=chinese.group(3)!;
    if(base.isEmpty||number==0)return null;
    final key=_seriesTitleKey(base);
    return _SeasonIdentity(key,number,unit,drama.seriesGroupId.isNotEmpty?drama.seriesGroupId:_canonicalGroupId(drama,base,unit));
  }
  final english=RegExp(r'^(.+?)\s+(?:season\s*([0-9]{1,3})|s([0-9]{1,3}))$',caseSensitive:false).firstMatch(title);
  if(english!=null){
    final base=english.group(1)!.trim(), number=int.tryParse(english.group(2)??english.group(3)??'')??0;
    if(base.isEmpty||number<1||number>999)return null;
    final key=_seriesTitleKey(base);
    return _SeasonIdentity(key,number,'季',drama.seriesGroupId.isNotEmpty?drama.seriesGroupId:_canonicalGroupId(drama,base,'季'));
  }
  if(drama.seriesGroupId.isNotEmpty&&drama.seriesSeasonNumber>0&&drama.seriesSeasonUnit.isNotEmpty){
    return _SeasonIdentity(_seriesTitleKey(title),drama.seriesSeasonNumber,drama.seriesSeasonUnit,drama.seriesGroupId);
  }
  return null;
}

int _catalogCandidateScore(Drama drama) {
  var score=0;
  if(drama.cover.trim().isNotEmpty)score+=8;
  if(drama.episodes>0)score+=4;
  if(drama.doubanId.isNotEmpty)score++;
  if(drama.description.isNotEmpty)score++;
  return score;
}

List<Drama> collapseDuoduoSeriesCatalogItems(Iterable<Drama> sourceItems) {
  final items=sourceItems.toList();
  if(items.length<2)return items;
  final families=<String,_SeasonFamily>{};
  final unsuffixedByBase=<String,List<int>>{};
  for(var index=0;index<items.length;index++){
    final drama=items[index];
    if(drama.source!='duoduo')continue;
    final identity=_seasonIdentity(drama);
    if(identity==null){
      if(drama.seriesGroupId.isEmpty){
        final key=_seriesTitleKey(drama.title);
        if(key.isNotEmpty)unsuffixedByBase.putIfAbsent(key,()=>[]).add(index);
      }
      continue;
    }
    final family=families.putIfAbsent(identity.groupId,()=>_SeasonFamily(identity.unit));
    if(family.unit!=identity.unit)continue;
    family.baseKeys.add(identity.baseKey);
    family.members.putIfAbsent(identity.number,()=>[]).add(index);
  }
  final membership=<int,String>{};
  final representatives=<String,Drama>{};
  for(final entry in families.entries){
    final family=entry.value;
    final indexes=<int,int>{};
    for(final season in family.members.entries){
      if(season.value.isEmpty)continue;
      var selected=season.value.first;
      for(final candidate in season.value.skip(1)){
        if(_catalogCandidateScore(items[candidate])>_catalogCandidateScore(items[selected]))selected=candidate;
      }
      indexes[season.key]=selected;
    }
    if(!indexes.containsKey(1)){
      final parts=entry.key.split(':');
      final aliasKey=parts.length>2&&parts[1]=='alias'?parts[2]:'';
      final matching=<int>[];
      for(final index in unsuffixedByBase.values.expand((values)=>values)){
        final drama=items[index];
        if(family.baseKeys.contains(_seriesTitleKey(drama.title)) ||
          (aliasKey.isNotEmpty&&_verifiedAliasKey(drama.title)==aliasKey)){
          matching.add(index);
        }
      }
      if(matching.length==1)indexes[1]=matching.single;
    }
    if(indexes.length<2)continue;
    final ordered=indexes.keys.toList()..sort();
    final firstNumber=ordered.first, chosen=items[indexes[ordered.first]!];
    final displayTitle=chosen.title
      .replaceFirst(RegExp(r'\s*第\s*([0-9]{1,3}|[零〇一二三四五六七八九十百]+)\s*(季|部)$'),'')
      .replaceFirst(RegExp(r'\s+(?:season|s)\s*[0-9]{1,3}$',caseSensitive:false),'');
    representatives[entry.key]=chosen.withSeriesIdentity(groupId:entry.key,seasonNumber:firstNumber,seasonUnit:family.unit,displayTitle:displayTitle);
    for(final index in indexes.values) membership[index]=entry.key;
    for(final indices in family.members.values) {
      for(final index in indices) membership[index]=entry.key;
    }
  }
  if(representatives.isEmpty)return items;
  final result=<Drama>[], emitted=<String>{};
  for(var index=0;index<items.length;index++){
    final group=membership[index];
    if(group==null)result.add(items[index]);
    else if(emitted.add(group))result.add(representatives[group]!);
  }
  return result;
}
