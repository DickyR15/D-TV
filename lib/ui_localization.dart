import 'dart:convert';
import 'dart:isolate';

import 'package:flutter/services.dart';

/// 將來源站提供的簡體中文轉為台灣常用繁體中文。
/// 僅處理畫面顯示文字；API 的分類 ID、查詢參數與原始資料不修改。
const Map<String, String> _twPhrases = {
  // 來源資料先經 OpenCC 正體化，這裡同時涵蓋正體化後的台灣用語。
  '番劇': '動畫連載',
  '番剧': '動畫連載',
  '動漫': '動畫',
  '动漫': '動畫',
  '劇場動畫': '劇場版動畫',
  '劇場版動畫': '劇場版動畫',
  '緩存': '快取',
  '缓存': '快取',
  '緩存目錄': '快取目錄',
  '缓存目录': '快取目錄',
  '緩存清理': '清除快取',
  '缓存清理': '清除快取',
  '緩存資料': '快取資料',
  '缓存数据': '快取資料',
  '文件夾': '資料夾',
  '文件夹': '資料夾',
  '文件路徑': '檔案路徑',
  '文件路径': '檔案路徑',
  '文件管理': '檔案管理',
  '本機文件': '本機檔案',
  '本地文件': '本機檔案',
  '本機文件夾': '本機資料夾',
  '本地文件夹': '本機資料夾',
  '文件大小': '檔案大小',
  '默認': '預設',
  '默认': '預設',
  '登錄': '登入',
  '登录': '登入',
  '退出登錄': '登出',
  '退出登录': '登出',
  '賬號': '帳號',
  '账号': '帳號',
  '用戶名': '使用者名稱',
  '用户名': '使用者名稱',
  '連接': '連線',
  '连接': '連線',
  '連接失敗': '連線失敗',
  '连接失败': '連線失敗',
  '安裝包': '安裝檔',
  '安装包': '安裝檔',
  '卸載': '解除安裝',
  '卸载': '解除安裝',
  '視頻': '影片',
  '视频': '影片',
  '視頻地址': '影片網址',
  '视频地址': '影片網址',
  '軟件': '軟體',
  '软件': '軟體',
  '設備': '裝置',
  '设备': '裝置',
  '鼠標': '滑鼠',
  '鼠标': '滑鼠',
  '信息': '資訊',
  '消息': '訊息',
  '訊息提示': '訊息提示',
  '消息提示': '訊息提示',
  '保存': '儲存',
  '保存成功': '儲存成功',
  '存儲': '儲存',
  '存储': '儲存',
  '首頁': '首頁',
  '首页': '首頁',
  '主頁': '主畫面',
  '主页': '主畫面',
  '點擊': '點選',
  '点击': '點選',
  '來源管理': '片源管理',
  '来源管理': '片源管理',
  '目前來源': '目前片源',
  '当前来源': '目前片源',
  '使用權限': '使用權限',
  '番劇': '動畫連載',
  '番剧': '動畫連載',
  '劇場動畫': '劇場版動畫',
  '动漫': '動畫',
  '動漫': '動畫',
  '缓存': '快取',
  '緩存': '快取',
  '缓存目录': '快取目錄',
  '缓存清理': '清除快取',
  '缓存数据': '快取資料',
  '文件夹': '資料夾',
  '文件路径': '檔案路徑',
  '文件管理': '檔案管理',
  '本地文件': '本機檔案',
  '本地存储': '本機儲存空間',
  '文件': '檔案',
  '默认': '預設',
  '密码': '密碼',
  '登录': '登入',
  '退出登录': '登出',
  '登录状态': '登入狀態',
  '搜索结果': '搜尋結果',
  '消息提示': '訊息提示',
  '消息': '訊息',
  '信息': '資訊',
  '账号': '帳號',
  '用户名': '使用者名稱',
  '连接失败': '連線失敗',
  '正在连接': '正在連線',
  '连接': '連線',
  '安装包': '安裝檔',
  '程序': '程式',
  '系统': '系統',
  '已启用': '已啟用',
  '已禁用': '已停用',
  '使用权限': '使用權限',
  '路径': '路徑',
  '调试': '除錯',
  '播放日志': '播放日誌',
  '权限': '權限',
  '許可權': '權限',
  '訪問': '存取',
  '访问': '存取',
  '全部許可權': '所有權限',
  '本機配置': '本機設定',
  '本地配置': '本機設定',
  '配置': '設定',
  '全部站源': '所有片源',
  '切換站源': '切換片源',
  '站源管理': '片源管理',
  '站源診斷': '片源診斷',
  '站源': '片源',
  '本地': '本機',
  '當前': '目前',
  '全部來源': '所有片源',
  '目前站源': '目前片源',
  '红果热播榜': '紅果熱播榜',
  '真人剧热播榜': '真人劇熱播榜',
  'AI剧热播榜': 'AI 劇熱播榜',
  '漫剧热播榜': '漫劇熱播榜',
  '红果榜单': '紅果榜單',
  '真人剧榜': '真人劇榜',
  '聚宝仙盆': '聚寶仙盆',
  '杂灵根': '雜靈根',
  '觉醒伪神': '覺醒偽神',
  '神明弃我我自为神': '神明棄我我自為神',
  '一场卖房，改写命运': '一場賣房，改寫命運',
  '一场百日宴，看清一家人': '一場百日宴，看清一家人',
  '紅果熱播榜': '紅果熱播榜',
  '真人劇熱播榜': '真人劇熱播榜',
  'AI 劇熱播榜': 'AI 劇熱播榜',
  'AI劇熱播榜': 'AI 劇熱播榜',
  'AI劇': 'AI 劇',
  'AI短劇': 'AI 短劇',
  'AI成人短劇': 'AI 成人短劇',
  'AI漫劇': 'AI 漫劇',
  'AI成人漫劇': 'AI 成人漫劇',
  'AI成人短剧': 'AI 成人短劇',
  'AI短剧': 'AI 短劇',
  'AI成人漫剧': 'AI 成人漫劇',
  'AI漫剧': 'AI 漫劇',
  'AI剧': 'AI 劇',
  '真人剧': '真人劇',
  '漫剧': '漫劇',
  '短剧': '短劇',
  '新剧': '新劇',
  '发现': '發現',
  '推荐': '推薦',
  '榜单': '榜單',
  '播放历史': '播放歷史',
  '观看历史': '觀看歷史',
  '播放记录': '播放紀錄',
  '观看记录': '觀看紀錄',
  '收藏夹': '收藏',
  '搜索': '搜尋',
  '设置': '設定',
  '用户': '使用者',
  '网络': '網路',
  '视频': '影片',
  '剧集': '劇集',
  '详情': '詳情',
  '简介': '簡介',
  '数据': '資料',
  '暂无': '暫無',
  '当前': '目前',
  '失败': '失敗',
  '正在获取': '正在取得',
  '自动重试': '自動重試',
  '加载更多': '載入更多',
  '已显示全部': '已顯示全部',
  '确定': '確定',
  '刷新': '重新整理',
  '高清': '高畫質',
  '红果': '紅果',
  '剧情': '劇情',
  '反转': '反轉',
  '古装': '古裝',
  '宫廷': '宮廷',
  '总裁': '總裁',
  '恋爱': '戀愛',
  '悬疑': '懸疑',
  '种田': '種田',
  '类别': '類別',
  '分类': '分類',
  '互动': '互動',
  '综合': '綜合',
  '热度': '熱度',
  '基于': '基於',
  '连载': '連載',
  '更新中': '更新中',
  '完结': '完結',
  '已完结': '已完結',
  '选择': '選擇',
  '删除': '刪除',
  '清空': '清空',
  '暂停': '暫停',
  '继续播放': '繼續播放',
  '线路切换': '線路切換',
  '播放失败': '播放失敗',
  '网络错误': '網路錯誤',
  '请稍后重试': '請稍後重試',
  '加载失败': '載入失敗',
  '没有更多内容': '沒有更多內容',
  '里面': '裡面',
  '这里': '這裡',
  '那里': '那裡',
  '家里': '家裡',
  '心里': '心裡',
  '头发': '頭髮',
  '白头发': '白頭髮',
  '发型': '髮型',
  '理发': '理髮',
  '染发': '染髮',
  '洗发': '洗髮',
  '长发': '長髮',
  '短发': '短髮',
  '皇后': '皇后',
  '太后': '太后',
  '王后': '王后',
  '面条': '麵條',
  '方便面': '泡麵',
  '一场': '一場',
  '过后': '過後',
  '最后': '最後',
  '然后': '然後',
  '以后': '以後',
  '后续': '後續',
  '皇宫': '皇宮',
  '里面的': '裡面的',
  '在线': '線上',
  '下载': '下載',
  '网页': '網頁',
  '两代人': '兩代人',
  '青虾': '青蝦',
  '纸书': '紙書',
  '脑洞': '腦洞',
  '热门': '熱門',
  '热播': '熱播',
  '最新剧集': '最新劇集',
  '最新短剧': '最新短劇',
  '用户协议': '使用者協議',
  '隐私政策': '隱私權政策',
  '软件': '軟體',
  '扫码': '掃描',
  '掃碼': '掃描',
  '二維碼': 'QR Code',
  '手机': '手機',
  '设备': '裝置',
  '应用': '應用程式',
  '网络连接': '網路連線',
  '切换': '切換',
  '播放进度': '播放進度',
  '剧名': '劇名',
  '剧情简介': '劇情簡介',
  '追剧': '收藏',
  '再试一次': '再試一次',
  '一只': '一隻',
  '只猫': '隻貓',
  '只橘猫': '隻橘貓',
};

/// 常見簡體字轉成繁體字。詞組先替換可避免簡繁用語差異。
const Map<String, String> _twCharacters = {
  '红': '紅',
  '剧': '劇',
  '热': '熱',
  '单': '單',
  '录': '錄',
  '览': '覽',
  '图': '圖',
  '标': '標',
  '类': '類',
  '别': '別',
  '说': '說',
  '从': '從',
  '进': '進',
  '选': '選',
  '择': '擇',
  '连': '連',
  '设': '設',
  '获': '獲',
  '资': '資',
  '显': '顯',
  '简': '簡',
  '体': '體',
  '汉': '漢',
  '这': '這',
  '个': '個',
  '国': '國',
  '广': '廣',
  '开': '開',
  '关': '關',
  '时': '時',
  '间': '間',
  '问': '問',
  '题': '題',
  '来': '來',
  '经': '經',
  '过': '過',
  '没': '沒',
  '为': '為',
  '与': '與',
  '对': '對',
  '内': '內',
  '现': '現',
  '发': '發',
  '样': '樣',
  '机': '機',
  '载': '載',
  '务': '務',
  '统': '統',
  '验': '驗',
  '证': '證',
  '线': '線',
  '档': '檔',
  '备': '備',
  '启': '啟',
  '动': '動',
  '级': '級',
  '处': '處',
  '变': '變',
  '数': '數',
  '据': '據',
  '续': '續',
  '读': '讀',
  '写': '寫',
  '页': '頁',
  '帮': '幫',
  '项': '項',
  '响': '響',
  '应': '應',
  '达': '達',
  '云': '雲',
  '传': '傳',
  '输': '輸',
  '扫': '掃',
  '码': '碼',
  '门': '門',
  '后': '後',
  '尝': '嘗',
  '试': '試',
  '将': '將',
  '执': '執',
  '仅': '僅',
  '区': '區',
  '当': '當',
  '联': '聯',
  '终': '終',
  '断': '斷',
  '带': '帶',
  '错': '錯',
  '误': '誤',
  '稳': '穩',
  '转': '轉',
  '缓': '緩',
  '冲': '衝',
  '话': '話',
  '编': '編',
  '辑': '輯',
  '历': '歷',
  '爱': '愛',
  '宝': '寶',
  '杂': '雜',
  '灵': '靈',
  '觉': '覺',
  '伪': '偽',
  '弃': '棄',
  '师': '師',
  '恶': '惡',
  '龙': '龍',
  '剑': '劍',
  '轻': '輕',
  '枪': '槍',
  '杀': '殺',
  '战': '戰',
  '旧': '舊',
  '远': '遠',
  '长': '長',
  '东': '東',
  '头': '頭',
  '脸': '臉',
  '岁': '歲',
  '爷': '爺',
  '妈': '媽',
  '钱': '錢',
  '财': '財',
  '猫': '貓',
  '养': '養',
  '万': '萬',
  '圣': '聖',
  '称': '稱',
  '实': '實',
  '贵': '貴',
  '听': '聽',
  '见': '見',
  '谁': '誰',
  '亲': '親',
  '声': '聲',
  '乐': '樂',
  '习': '習',
  '队': '隊',
  '记': '記',
  '号': '號',
  '戏': '戲',
  '满': '滿',
  '书': '書',
  '画': '畫',
  '气': '氣',
  '买': '買',
  '卖': '賣',
  '谈': '談',
  '场': '場',
  '办': '辦',
  '闪': '閃',
  '难': '難',
  '惊': '驚',
  '阳': '陽',
  '阴': '陰',
  '静': '靜',
  '净': '淨',
  '欢': '歡',
  '园': '園',
  '边': '邊',
  '离': '離',
  '际': '際',
  '换': '換',
  '并': '並',
  '状': '狀',
  '异': '異',
  '态': '態',
  '桥': '橋',
  '湾': '灣',
  '电': '電',
  '视': '視',
  '台': '台',
  '网': '網',
  '条': '條',
  '齐': '齊',
  '权': '權',
  '导': '導',
  '属': '屬',
  '团': '團',
  '专': '專',
  '观': '觀',
  '负': '負',
  '责': '責',
  '畅': '暢',
  '层': '層',
  '隐': '隱',
  '飞': '飛',
  '举': '舉',
  '员': '員',
  '圆': '圓',
  '贴': '貼',
  '险': '險',
  '药': '藥',
  '价': '價',
  '镜': '鏡',
  '农': '農',
  '艺': '藝',
  '术': '術',
  '劳': '勞',
  '种': '種',
  '归': '歸',
  '质': '質',
  '纷': '紛',
  '伤': '傷',
  '艳': '艷',
  '绝': '絕',
  '够': '夠',
  '楼': '樓',
  '紧': '緊',
  '语': '語',
  '识': '識',
  '词': '詞',
  '删': '刪',
  '闭': '閉',
  '摆': '擺',
  '摇': '搖',
  '灯': '燈',
  '户': '戶',
  '厉': '厲',
  '积': '積',
  '块': '塊',
  '阶': '階',
  '华': '華',
  '众': '眾',
  '环': '環',
  '境': '境',
  '围': '圍',
  '绕': '繞',
  '两': '兩',
  '虾': '蝦',
  '脑': '腦',
  '纸': '紙',
  '运': '運',
  '产': '產',
  '车': '車',
  '库': '庫',
  '节': '節',
  '许': '許',
  '须': '須',
  '马': '馬',
  '鸟': '鳥',
  '猪': '豬',
  '亿': '億',
  '倾': '傾',
  '钟': '鐘',
  '柜': '櫃',
  '饭': '飯',
  '认': '認',
  '讲': '講',
  '让': '讓',
  '补': '補',
  '极': '極',
  '构': '構',
  '扩': '擴',
  '规': '規',
  '尽': '盡',
  '继': '繼',
  '评': '評',
  '论': '論',
  '结': '結',
  '协': '協',
  '议': '議',
  '优': '優',
  '惠': '惠',
  '奖': '獎',
  '顾': '顧',
  '洁': '潔',
  '担': '擔',
  '彻': '徹',
  '赞': '讚',
  '频': '頻',
  '真': '真',
};

String _uiTraditionalFallback(String value) {
  var result = value;
  for (final entry in _twPhrases.entries) {
    result = result.replaceAll(entry.key, entry.value);
  }
  for (final entry in _twCharacters.entries) {
    result = result.replaceAll(entry.key, entry.value);
  }
  const restorations = {
    '皇後': '皇后',
    '太後': '太后',
    '王後': '王后',
    '頭發': '頭髮',
    '白發': '白髮',
    '長發': '長髮',
    '短發': '短髮',
    '理發': '理髮',
    '染發': '染髮',
    '洗發': '洗髮',
    '發型': '髮型',
    '頭發型': '頭髮型',
    '公裡': '公里',
    '里程': '里程',
  };
  for (final entry in restorations.entries) {
    result = result.replaceAll(entry.key, entry.value);
  }
  return result;
}


class _OpenCcNode {
  final Map<int, _OpenCcNode> children = {};
  String? value;
}

_OpenCcNode? _openCcS2t;
_OpenCcNode? _openCcTaiwan;
bool _openCcReady = false;

// Repeated widget rebuilds often convert the same ranking/poster text.
// Keep a bounded LRU-like cache so focus changes don't repeatedly scan every
// phrase substitution and trie edge on the UI isolate.
final _conversionCache = <String, String>{};
const _conversionCacheLimit = 4096;

/// Build the large tries off the UI isolate. Parsing these dictionaries here
/// used to block Android TV D-pad input for several seconds on first launch.
typedef _OpenCcTrees = ({_OpenCcNode s2t, _OpenCcNode taiwan});

_OpenCcTrees _buildOpenCcTrees(Map<String, String> sourceFiles) {
  final characterMap = _parseOpenCc(sourceFiles['STCharacters.txt']!);
  final phraseMap = _parseOpenCc(sourceFiles['STPhrases.txt']!);
  final taiwanPhraseMap = _parseOpenCc(sourceFiles['TWPhrases.txt']!);
  final variantPhraseMap =
      _parseOpenCc(sourceFiles['TWVariantsPhrases.txt']!);
  final variantMap = _parseOpenCc(sourceFiles['TWVariants.txt']!);

  final s2t = _OpenCcNode();
  _insertOpenCc(s2t, characterMap, overwrite: false);
  // OpenCC phrase entries take precedence over individual character mappings.
  _insertOpenCc(s2t, phraseMap, overwrite: true);

  final taiwan = _OpenCcNode();
  // The first matching dictionary has precedence, as in OpenCC's s2twp chain.
  _insertOpenCc(taiwan, taiwanPhraseMap, overwrite: false);
  _insertOpenCc(taiwan, variantPhraseMap, overwrite: false);
  _insertOpenCc(taiwan, variantMap, overwrite: false);
  return (s2t: s2t, taiwan: taiwan);
}

/// Load OpenCC dictionaries bundled in the APK; no network is used at runtime.
Future<void> initializeUiLocalization() async {
  final sourceFiles = <String, String>{};
  for (final name in const [
    'STCharacters.txt',
    'STPhrases.txt',
    'TWPhrases.txt',
    'TWVariantsPhrases.txt',
    'TWVariants.txt',
  ]) {
    sourceFiles[name] = await rootBundle.loadString('assets/opencc/$name');
  }

  final trees = await Isolate.run(() => _buildOpenCcTrees(sourceFiles));
  _openCcS2t = trees.s2t;
  _openCcTaiwan = trees.taiwan;
  // Drop fallback translations created while the dictionaries were loading.
  _conversionCache.clear();
  _openCcReady = true;
}

Map<String, String> _parseOpenCc(String content) {
  final result = <String, String>{};
  for (final line in const LineSplitter().convert(content)) {
    if (line.isEmpty || line.startsWith('#')) continue;
    final separator = line.indexOf('\t');
    if (separator <= 0 || separator == line.length - 1) continue;
    final key = line.substring(0, separator);
    final values = line.substring(separator + 1).trim().split(RegExp(r'\s+'));
    if (key.isEmpty || values.isEmpty || values.first.isEmpty) continue;
    result[key] = values.first;
  }
  return result;
}

void _insertOpenCc(
  _OpenCcNode root,
  Map<String, String> dictionary, {
  required bool overwrite,
}) {
  for (final entry in dictionary.entries) {
    var node = root;
    for (final rune in entry.key.runes) {
      node = node.children.putIfAbsent(rune, _OpenCcNode.new);
    }
    if (overwrite || node.value == null) node.value = entry.value;
  }
}

String _convertOpenCc(String input, _OpenCcNode root) {
  final runes = input.runes.toList(growable: false);
  final output = StringBuffer();
  var index = 0;
  while (index < runes.length) {
    var node = root;
    var cursor = index;
    String? bestValue;
    var bestEnd = index;
    while (cursor < runes.length) {
      final next = node.children[runes[cursor]];
      if (next == null) break;
      node = next;
      cursor++;
      if (node.value != null) {
        bestValue = node.value;
        bestEnd = cursor;
      }
    }
    if (bestValue != null) {
      output.write(bestValue);
      index = bestEnd;
    } else {
      output.write(String.fromCharCode(runes[index]));
      index++;
    }
  }
  return output.toString();
}

/// Convert source-site text to Taiwan Traditional Chinese for display only.
/// API identifiers, category IDs, request arguments, and raw records stay intact.
String uiTraditional(String value) {
  final cached = _conversionCache.remove(value);
  if (cached != null) {
    _conversionCache[value] = cached;
    return cached;
  }

  final s2t = _openCcS2t;
  final taiwan = _openCcTaiwan;
  late final String result;
  if (!_openCcReady || s2t == null || taiwan == null) {
    result = _uiTraditionalFallback(value);
  } else {
    var converted = _convertOpenCc(value, s2t);
    converted = _convertOpenCc(converted, taiwan);
    // D TV 專用詞彙，補足 OpenCC 未涵蓋的台灣用語。
    for (final entry in _twPhrases.entries) {
      converted = converted.replaceAll(entry.key, entry.value);
    }
    const restorations = {
      '皇後': '皇后',
      '太後': '太后',
      '王後': '王后',
      '頭發': '頭髮',
      '白發': '白髮',
      '長發': '長髮',
      '短發': '短髮',
      '理發': '理髮',
      '染發': '染髮',
      '洗發': '洗髮',
      '發型': '髮型',
      '頭發型': '頭髮型',
      '公裡': '公里',
      '里程': '里程',
    };
    for (final entry in restorations.entries) {
      converted = converted.replaceAll(entry.key, entry.value);
    }
    result = converted;
  }

  if (_conversionCache.length >= _conversionCacheLimit) {
    _conversionCache.remove(_conversionCache.keys.first);
  }
  _conversionCache[value] = result;
  return result;
}
