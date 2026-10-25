import 'dart:async';
import 'package:flutter/material.dart';

import 'package:flutter/services.dart';

import 'app_layout.dart';
import 'ui_localization.dart';

class RemoteTextInput extends StatelessWidget {
  const RemoteTextInput({super.key, required this.child});
  final Widget child;

  KeyEventResult _key(FocusNode node, KeyEvent event) {
    final focus = FocusManager.instance.primaryFocus;
    final editable = focus?.context
        ?.findAncestorStateOfType<EditableTextState>();
    if (editable == null ||
        (event is! KeyDownEvent && event is! KeyRepeatEvent)) {
      return KeyEventResult.ignored;
    }
    final key = event.logicalKey;
    if (key == LogicalKeyboardKey.arrowUp) {
      focus!.focusInDirection(TraversalDirection.up);
    } else if (key == LogicalKeyboardKey.arrowDown) {
      focus!.focusInDirection(TraversalDirection.down);
    } else if (key == LogicalKeyboardKey.escape ||
        key == LogicalKeyboardKey.goBack) {
      if (event is KeyDownEvent &&
          !focus!.focusInDirection(TraversalDirection.down) &&
          !focus.focusInDirection(TraversalDirection.up)) {
        return KeyEventResult.ignored;
      }
    } else if (event is KeyDownEvent &&
        [
          LogicalKeyboardKey.select,
          LogicalKeyboardKey.enter,
          LogicalKeyboardKey.numpadEnter,
        ].contains(key)) {
      editable.requestKeyboard();
    } else {
      return KeyEventResult.ignored;
    }
    return KeyEventResult.handled;
  }

  @override
  Widget build(BuildContext context) => AppLayout.isTelevision(context)
      ? Focus(
          canRequestFocus: false,
          skipTraversal: true,
          onKeyEvent: _key,
          child: child,
        )
      : child;
}

class RemoteTarget extends StatefulWidget {
  const RemoteTarget({
    super.key,
    required this.child,
    required this.onPressed,
    this.focusNode,
    this.onFocus,
    this.onLongPress,
    this.autofocus = false,
    this.selected = false,
    this.label,
    this.radius = 14,
    this.padding = const EdgeInsets.all(4),
    this.borderWidth = 3,
    this.outlined = false,
    this.scrollAlignment = 0,
    this.scrollAlignmentPolicy,
    this.ensureVisibleOnFocus = true,
  });
  final Widget child;
  final VoidCallback? onPressed;
  final FocusNode? focusNode;
  final VoidCallback? onFocus;
  final VoidCallback? onLongPress;
  final bool autofocus;
  final bool selected;
  final String? label;
  final double radius;
  final EdgeInsetsGeometry padding;
  final double borderWidth;
  final bool outlined;
  final double scrollAlignment;
  final ScrollPositionAlignmentPolicy? scrollAlignmentPolicy;

  /// Disable automatic visibility scrolling when the parent grid already
  /// coordinates focus scrolling for every D-pad movement.
  final bool ensureVisibleOnFocus;

  @override
  State<RemoteTarget> createState() => _RemoteTargetState();
}

class _RemoteTargetState extends State<RemoteTarget> {
  bool _focused = false;
  bool _activateHeld = false;
  bool _longPressFired = false;
  Timer? _longPressTimer;

  KeyEventResult _handleActivationKey(FocusNode node, KeyEvent event) {
    if (widget.onLongPress == null) return KeyEventResult.ignored;
    final key = event.logicalKey;
    if (![
      LogicalKeyboardKey.select,
      LogicalKeyboardKey.enter,
      LogicalKeyboardKey.numpadEnter,
      LogicalKeyboardKey.space,
    ].contains(key)) {
      return KeyEventResult.ignored;
    }
    if (event is KeyDownEvent && !_activateHeld) {
      _activateHeld = true;
      _longPressFired = false;
      _longPressTimer?.cancel();
      _longPressTimer = Timer(const Duration(milliseconds: 650), () {
        if (!mounted || !_activateHeld || widget.onLongPress == null) return;
        _longPressFired = true;
        widget.onLongPress?.call();
      });
      return KeyEventResult.handled;
    }
    if (event is KeyRepeatEvent) {
      return _activateHeld ? KeyEventResult.handled : KeyEventResult.ignored;
    }
    if (event is KeyUpEvent && _activateHeld) {
      _longPressTimer?.cancel();
      if (!_longPressFired) widget.onPressed?.call();
      _activateHeld = false;
      _longPressFired = false;
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  @override
  Widget build(BuildContext context) => Focus(
    canRequestFocus: false,
    skipTraversal: true,
    onKeyEvent: _handleActivationKey,
    child: FocusableActionDetector(
    focusNode: widget.focusNode,
    autofocus: widget.autofocus,
    enabled: widget.onPressed != null,
    actions: {
      ActivateIntent: CallbackAction<ActivateIntent>(
        onInvoke: (_) {
          widget.onPressed?.call();
          return null;
        },
      ),
    },
    onFocusChange: (focused) {
      if (!focused) {
        _longPressTimer?.cancel();
        _activateHeld = false;
        _longPressFired = false;
      }
      if (mounted) {
        setState(() => _focused = focused);
      }
      if (focused) {
        widget.onFocus?.call();
        if (widget.ensureVisibleOnFocus) {
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (mounted && _focused) {
              Scrollable.ensureVisible(
                context,
                alignment: widget.scrollAlignment,
                alignmentPolicy:
                    widget.scrollAlignmentPolicy ??
                    ScrollPositionAlignmentPolicy.keepVisibleAtEnd,
              );
            }
          });
        }
      }
    },
    child: Semantics(
      button: true,
      enabled: widget.onPressed != null,
      focused: _focused,
      label: widget.label,
      child: MouseRegion(
        cursor: widget.onPressed == null
            ? SystemMouseCursors.basic
            : SystemMouseCursors.click,
        child: GestureDetector(
          behavior: HitTestBehavior.opaque,
          onTap: widget.onPressed,
          onLongPress: widget.onLongPress,
          child: AnimatedContainer(
            duration: const Duration(milliseconds: 120),
            padding: widget.padding,
            decoration: BoxDecoration(
              // Selection uses a restrained brand-tinted fill; keyboard focus uses a vivid
              // brand-red outline. They remain distinct even when both are active.
              color: widget.selected
                  ? Theme.of(context).colorScheme.secondaryContainer
                  : _focused
                  ? Theme.of(context).colorScheme.surfaceContainerHighest
                  : Colors.transparent,
              borderRadius: BorderRadius.circular(widget.radius),
              border: Border.all(
                color: _focused
                    ? Theme.of(context).colorScheme.primary
                    : widget.selected
                    ? Theme.of(context).colorScheme.secondary
                    : widget.outlined
                    ? Theme.of(context).colorScheme.outlineVariant
                    : Colors.transparent,
                width: widget.borderWidth,
              ),
            ),
            child: DefaultTextStyle.merge(
              style: TextStyle(
                color: widget.selected
                    ? Theme.of(context).colorScheme.onSecondaryContainer
                    : Theme.of(context).colorScheme.onSurface,
              ),
              child: IconTheme.merge(
                data: IconThemeData(
                  color: widget.selected
                      ? Theme.of(context).colorScheme.onSecondaryContainer
                      : Theme.of(context).colorScheme.onSurface,
                ),
                child: ExcludeFocus(child: widget.child),
              ),
            ),
          ),
        ),
      ),
    ),
  ),
  );

  @override
  void dispose() {
    _longPressTimer?.cancel();
    super.dispose();
  }
}

class RemoteButton extends StatelessWidget {
  const RemoteButton({
    super.key,
    required this.label,
    required this.onPressed,
    this.icon,
    this.selected = false,
    this.autofocus = false,
    this.focusNode,
  });
  final String label;
  final VoidCallback? onPressed;
  final IconData? icon;
  final bool selected;
  final bool autofocus;
  final FocusNode? focusNode;

  @override
  Widget build(BuildContext context) => RemoteTarget(
    onPressed: onPressed,
    selected: selected,
    autofocus: autofocus,
    focusNode: focusNode,
    label: label,
    child: Padding(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[Icon(icon, size: 22), const SizedBox(width: 8)],
          Text(
            label,
            style: TextStyle(
              fontSize: 17,
              color: onPressed == null
                  ? Theme.of(context).disabledColor
                  : selected
                  ? Theme.of(context).colorScheme.onSecondaryContainer
                  : Theme.of(context).colorScheme.onSurface,
            ),
          ),
          if (selected) ...[
            const SizedBox(width: 8),
            const Icon(Icons.check_circle_rounded, size: 18),
          ],
        ],
      ),
    ),
  );
}

/// Explicit entry point for the first poster in a TV grid.
class RemoteGridFocusController {
  Object? _owner;
  VoidCallback? _focusFirst;
  bool get hasItems => _focusFirst != null;
  void focusFirst() => _focusFirst?.call();
  void clear() { _owner = null; _focusFirst = null; }
  void _bind(Object owner, VoidCallback? callback) { _owner = owner; _focusFirst = callback; }
  void _unbind(Object owner) { if (identical(_owner, owner)) { _owner = null; _focusFirst = null; } }
}

class RemoteGrid extends StatefulWidget {
  const RemoteGrid({
    super.key,
    required this.itemKeys,
    required this.columns,
    required this.itemExtent,
    required this.itemBuilder,
    this.controller,
    this.spacing = 14,
    this.padding = const EdgeInsets.all(18),
    this.footer,
    this.initialIndex = 0,
    this.autofocus = false,
    this.focusController,
    this.onExitLeft,
  });
  final List<String> itemKeys;
  final int columns;
  final double itemExtent;
  final double spacing;
  final EdgeInsets padding;
  final ScrollController? controller;
  final Widget? footer;
  final int initialIndex;
  final bool autofocus;
  final RemoteGridFocusController? focusController;
  final VoidCallback? onExitLeft;
  final Widget Function(BuildContext, int, FocusNode, VoidCallback) itemBuilder;

  @override
  State<RemoteGrid> createState() => _RemoteGridState();
}

class _RemoteGridState extends State<RemoteGrid> {
  final _ownScroll = ScrollController();
  final _nodes = <String, FocusNode>{};
  final _itemIndices = <String, int>{};
  final Object _focusControllerOwner = Object();
  String? _focused;
  String? _target;
  int _generation = 0;
  ScrollController get _scroll => widget.controller ?? _ownScroll;

  void _syncItemIndices() {
    _itemIndices
      ..clear();
    for (var index = 0; index < widget.itemKeys.length; index++) {
      _itemIndices[widget.itemKeys[index]] = index;
    }
  }

  void _syncFocusController() {
    widget.focusController?._bind(
      _focusControllerOwner,
      widget.itemKeys.isEmpty ? null : () {
        if (mounted && widget.itemKeys.isNotEmpty) _focusAt(0);
      },
    );
  }

  FocusNode _node(int index) => _nodes.putIfAbsent(
    widget.itemKeys[index],
    () => FocusNode(debugLabel: 'remote-${widget.itemKeys[index]}'),
  );

  @override
  void initState() {
    super.initState();
    _syncItemIndices();
    _syncFocusController();
    if (widget.autofocus) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && widget.itemKeys.isNotEmpty) {
          _focusAt(widget.initialIndex.clamp(0, widget.itemKeys.length - 1));
        }
      });
    }
  }

  void _focusAt(int index) {
    final generation = ++_generation;
    _target = widget.itemKeys[index];
    final node = _node(index);

    if (_scroll.hasClients) {
      final row = index ~/ widget.columns;
      final itemTop =
          widget.padding.top + row * (widget.itemExtent + widget.spacing);
      final itemBottom = itemTop + widget.itemExtent;
      final currentOffset = _scroll.offset;
      final viewportHeight = _scroll.position.viewportDimension;

      double? targetOffset;
      if (row == 0) {
        if (currentOffset > 0.0) {
          targetOffset = 0.0;
        }
      } else if (itemTop < currentOffset + widget.padding.top) {
        targetOffset = itemTop - widget.padding.top;
      } else if (itemBottom >
          currentOffset + viewportHeight - widget.padding.bottom) {
        targetOffset = itemBottom - viewportHeight + widget.padding.bottom;
      }

      if (targetOffset != null) {
        final clamped = targetOffset.clamp(
          0.0,
          _scroll.position.maxScrollExtent,
        );
        if ((clamped - currentOffset).abs() > 1.0) {
          _scroll.animateTo(
            clamped,
            duration: const Duration(milliseconds: 180),
            curve: Curves.easeOutCubic,
          );
        }
      }
    }

    if (node.context != null) {
      node.requestFocus();
      _target = null;
    } else {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && generation == _generation && node.context != null) {
          node.requestFocus();
          _target = null;
        }
      });
    }
  }

  KeyEventResult _key(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    if (_focused == null || !(_nodes[_focused]?.hasFocus ?? false)) {
      return KeyEventResult.ignored;
    }
    final index = _itemIndices[_target ?? _focused!] ?? -1;
    if (index < 0) return KeyEventResult.ignored;
    final key = event.logicalKey;
    final int next;
    if (key == LogicalKeyboardKey.arrowDown) {
      final lastRow = (widget.itemKeys.length - 1) ~/ widget.columns;
      if (index ~/ widget.columns == lastRow) {
        if (_scroll.hasClients &&
            _scroll.offset < _scroll.position.maxScrollExtent - 1.0) {
          _scroll.animateTo(
            _scroll.position.maxScrollExtent,
            duration: const Duration(milliseconds: 150),
            curve: Curves.easeOutCubic,
          );
          return KeyEventResult.handled;
        }
        return KeyEventResult.ignored;
      }
      next = (index + widget.columns).clamp(0, widget.itemKeys.length - 1);
    } else if (key == LogicalKeyboardKey.arrowUp) {
      if (index < widget.columns) {
        if (_scroll.hasClients && _scroll.offset > 1.0) {
          _scroll.animateTo(
            0.0,
            duration: const Duration(milliseconds: 150),
            curve: Curves.easeOutCubic,
          );
          return KeyEventResult.handled;
        }
        FocusScope.of(context).focusInDirection(TraversalDirection.up);
        return KeyEventResult.handled;
      }
      next = index - widget.columns;
    } else if (key == LogicalKeyboardKey.arrowRight) {
      if (index % widget.columns == widget.columns - 1 ||
          index == widget.itemKeys.length - 1) {
        return KeyEventResult.handled;
      }
      next = index + 1;
    } else if (key == LogicalKeyboardKey.arrowLeft) {
      if (index % widget.columns == 0) {
        final exitLeft = widget.onExitLeft;
        if (exitLeft == null) return KeyEventResult.ignored;
        exitLeft();
        return KeyEventResult.handled;
      }
      next = index - 1;
    } else {
      return KeyEventResult.ignored;
    }
    if (next < 0 || next >= widget.itemKeys.length) {
      return KeyEventResult.ignored;
    }
    _focusAt(next);
    return KeyEventResult.handled;
  }

  @override
  void didUpdateWidget(covariant RemoteGrid oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.focusController != widget.focusController) {
      oldWidget.focusController?._unbind(_focusControllerOwner);
    }
    _syncItemIndices();
    _syncFocusController();
    if (widget.itemKeys.isEmpty) widget.focusController?.clear();
    final activeKeys = widget.itemKeys.toSet();
    final removed = _nodes.keys
        .where((key) => !activeKeys.contains(key))
        .toList();
    final restore = removed.any((key) => _nodes[key]!.hasFocus);
    final index = oldWidget.itemKeys.indexOf(_focused ?? '');
    for (final key in removed) {
      _nodes.remove(key)?.dispose();
    }
    if (_focused != null && !activeKeys.contains(_focused)) {
      _focused = null;
      _target = null;
      _generation++;
    }
    if (restore && widget.itemKeys.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && widget.itemKeys.isNotEmpty) {
          _focusAt(index.clamp(0, widget.itemKeys.length - 1));
        }
      });
    }
  }

  @override
  void dispose() {
    _generation++;
    widget.focusController?._unbind(_focusControllerOwner);
    for (final node in _nodes.values) {
      node.dispose();
    }
    _ownScroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Focus(
    canRequestFocus: false,
    skipTraversal: true,
    onKeyEvent: _key,
    child: CustomScrollView(
      controller: _scroll,
      slivers: [
        SliverPadding(
          padding: widget.padding,
          sliver: SliverGrid(
            gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: widget.columns,
              mainAxisExtent: widget.itemExtent,
              crossAxisSpacing: widget.spacing,
              mainAxisSpacing: widget.spacing,
            ),
            delegate: SliverChildBuilderDelegate(
              (context, index) =>
                  widget.itemBuilder(context, index, _node(index), () {
                    _focused = widget.itemKeys[index];
                  }),
              childCount: widget.itemKeys.length,
            ),
          ),
        ),
        if (widget.footer != null) SliverToBoxAdapter(child: widget.footer),
      ],
    ),
  );
}

class RemoteEpisodeButton extends StatelessWidget {
  const RemoteEpisodeButton({
    super.key,
    required this.number,
    required this.onPressed,
    this.vip = false,
    this.current = false,
    this.compact = false,
    this.focusNode,
    this.onFocus,
  });
  final int number;
  final bool vip;
  final bool current;
  final bool compact;
  final VoidCallback onPressed;
  final FocusNode? focusNode;
  final VoidCallback? onFocus;

  @override
  Widget build(BuildContext context) => RemoteTarget(
    focusNode: focusNode,
    onFocus: onFocus,
    selected: current,
    radius: compact ? 8 : 14,
    padding: compact ? const EdgeInsets.all(2) : const EdgeInsets.all(4),
    borderWidth: compact ? 1.2 : 2,
    outlined: true,
    onPressed: onPressed,
    label: '第 $number 集${vip ? '，VIP 試看' : ''}',
    child: Center(
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Flexible(
            child: Text(
              '$number',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: compact ? 14 : 20,
                fontWeight: current ? FontWeight.w700 : FontWeight.w500,
              ),
            ),
          ),
          if (vip) ...[
            SizedBox(width: compact ? 2 : 4),
            Icon(
              Icons.workspace_premium_rounded,
              color: Theme.of(context).colorScheme.tertiary,
              size: compact ? 15 : 18,
            ),
          ],
        ],
      ),
    ),
  );
}

class TelevisionSearchDialog extends StatefulWidget {
  const TelevisionSearchDialog({
    super.key,
    required this.initialValue,
    required this.title,
    this.suggestions,
    this.recentSearches = const [],
    this.onCancel,
    this.onPhoneSearch,
  });
  final Future<List<String>> Function(String)? suggestions;
  final List<String> recentSearches;
  final VoidCallback? onCancel;
  final VoidCallback? onPhoneSearch;
  final String initialValue;
  final String title;

  @override
  State<TelevisionSearchDialog> createState() => _TelevisionSearchDialogState();
}

class _TelevisionSearchDialogState extends State<TelevisionSearchDialog> {
  late final TextEditingController _controller = TextEditingController(
    text: widget.initialValue,
  );
  final FocusNode _queryFocus = FocusNode(debugLabel: 'tv-drama-search-input');
  final FocusNode _phoneSearchFocus = FocusNode(
    debugLabel: 'tv-drama-search-phone-qr',
  );
  final FocusNode _cancelFocus = FocusNode(debugLabel: 'tv-drama-search-cancel');
  final FocusNode _submitFocus = FocusNode(debugLabel: 'tv-drama-search-submit');
  Timer? _debounce;
  int _generation = 0;
  List<String> _results = [];
  bool _searching = false;

  String get _query => _controller.text;

  @override
  void initState() {
    super.initState();
    if (_query.trim().isNotEmpty) {
      _fetchSuggestions(_query.trim());
    }
  }

  @override
  void dispose() {
    _generation++;
    _debounce?.cancel();
    _controller.dispose();
    _queryFocus.dispose();
    _phoneSearchFocus.dispose();
    _cancelFocus.dispose();
    _submitFocus.dispose();
    widget.onCancel?.call();
    super.dispose();
  }

  KeyEventResult _searchInputKey(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
      _queryFocus.unfocus();
      SystemChannels.textInput.invokeMethod<void>('TextInput.hide');
      (widget.onPhoneSearch != null ? _phoneSearchFocus : _cancelFocus)
          .requestFocus();
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  KeyEventResult _searchActionKey(
    FocusNode node,
    KeyEvent event, {
    required FocusNode left,
    required FocusNode right,
  }) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
      _queryFocus.requestFocus();
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowLeft) {
      left.requestFocus();
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowRight) {
      right.requestFocus();
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  void _queryChanged(String value) {
    _debounce?.cancel();
    final query = value.trim();
    final generation = ++_generation;
    if (query.isEmpty || widget.suggestions == null) {
      setState(() {
        _results = [];
        _searching = false;
      });
      widget.onCancel?.call();
      return;
    }
    setState(() => _searching = true);
    _debounce = Timer(const Duration(milliseconds: 250), () {
      if (generation == _generation) {
        _fetchSuggestions(query, generation: generation);
      }
    });
  }

  Future<void> _fetchSuggestions(String query, {int? generation}) async {
    final fetchGeneration = generation ?? ++_generation;
    if (widget.suggestions == null || query.trim().isEmpty) return;
    setState(() => _searching = true);
    try {
      final values = await widget.suggestions!(query.trim());
      if (!mounted || fetchGeneration != _generation) return;
      setState(() {
        _results = values
            .where((value) => value.trim().isNotEmpty)
            .toSet()
            .take(12)
            .toList();
        _searching = false;
      });
    } catch (_) {
      if (!mounted || fetchGeneration != _generation) return;
      setState(() {
        _results = [];
        _searching = false;
      });
    }
  }

  void _submit(String value) {
    final query = value.trim();
    if (query.isEmpty) return;
    // 只提交使用者輸入或選取的原始劇名，不改動來源搜尋關鍵字。
    Navigator.of(context).pop(query);
  }

  void _usePhoneSearch() {
    final callback = widget.onPhoneSearch;
    if (callback == null) return;
    Navigator.of(context).pop();
    WidgetsBinding.instance.addPostFrameCallback((_) => callback());
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final query = _query.trim();
    final keyboardInset = MediaQuery.viewInsetsOf(context).bottom;
    final keyboardVisible = keyboardInset > 0;
    final contentHeight = (MediaQuery.sizeOf(context).height -
            keyboardInset -
            190)
        .clamp(140.0, 430.0)
        .toDouble();
    final showRecent =
        query.isEmpty && widget.recentSearches.isNotEmpty && !keyboardVisible;
    return AlertDialog(
      titlePadding: const EdgeInsets.fromLTRB(24, 20, 24, 12),
      contentPadding: const EdgeInsets.fromLTRB(24, 0, 24, 12),
      title: Row(
        children: [
          Icon(Icons.search_rounded, color: theme.colorScheme.primary),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              uiTraditional(widget.title),
              style: const TextStyle(fontSize: 20),
            ),
          ),
        ],
      ),
      content: SizedBox(
        width: 660,
        height: contentHeight,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Focus(
              canRequestFocus: false,
              skipTraversal: true,
              onKeyEvent: _searchInputKey,
              child: TextField(
              key: const ValueKey('tv-drama-search-input'),
              focusNode: _queryFocus,
              controller: _controller,
              autofocus: true,
              textInputAction: TextInputAction.search,
              onChanged: _queryChanged,
              onSubmitted: _submit,
              decoration: InputDecoration(
                labelText: '劇名',
                hintText: '輸入完整劇名',
                prefixIcon: const Icon(Icons.search_rounded),
                suffixIcon: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    if (_query.isNotEmpty)
                      IconButton(
                        tooltip: '清除',
                        onPressed: () {
                          _controller.clear();
                          _queryChanged('');
                        },
                        icon: const Icon(Icons.close_rounded),
                      ),
                    IconButton(
                      tooltip: '搜尋',
                      onPressed: () => _submit(_query),
                      icon: const Icon(Icons.arrow_forward_rounded),
                    ),
                  ],
                ),
              ),
              ),
            ),
            const SizedBox(height: 18),
            Row(
              children: [
                Expanded(
                  child: Text(
                    showRecent
                        ? '最近搜尋'
                        : query.isEmpty
                        ? '輸入劇名即可搜尋'
                        : '搜尋建議',
                    style: TextStyle(
                      fontSize: 16,
                      fontWeight: FontWeight.bold,
                      color: theme.colorScheme.primary,
                    ),
                  ),
                ),
                if (_searching)
                  const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
              ],
            ),
            const SizedBox(height: 8),
            Expanded(
              child: showRecent
                  ? SingleChildScrollView(
                      child: Wrap(
                        spacing: 8,
                        runSpacing: 8,
                        children: [
                          for (final item in widget.recentSearches)
                            RemoteButton(
                              key: ValueKey('recent-search-$item'),
                              label: uiTraditional(item),
                              onPressed: () => _submit(item),
                            ),
                        ],
                      ),
                    )
                  : query.isEmpty
                  ? keyboardVisible
                        ? const SizedBox.shrink()
                        : Center(
                            child: Text(
                              '可以直接輸入劇名，或改用手機掃 QR Code 搜尋。',
                              textAlign: TextAlign.center,
                              style: TextStyle(color: theme.hintColor),
                            ),
                          )
                  : _results.isEmpty && !_searching
                  ? Center(
                      child: Text(
                        '沒有搜尋建議；仍可直接搜尋完整劇名。',
                        textAlign: TextAlign.center,
                        style: TextStyle(color: theme.hintColor),
                      ),
                    )
                  : ListView.separated(
                      itemCount: _results.length,
                      separatorBuilder: (_, _) => const SizedBox(height: 6),
                      itemBuilder: (context, index) {
                        final item = _results[index];
                        return RemoteButton(
                          key: ValueKey('tv-search-suggestion-$index'),
                          label: uiTraditional(item),
                          icon: Icons.movie_outlined,
                          onPressed: () => _submit(item),
                        );
                      },
                    ),
            ),
          ],
        ),
      ),
      actions: [
        if (widget.onPhoneSearch != null)
          Focus(
            onKeyEvent: (node, event) => _searchActionKey(
              node,
              event,
              left: _submitFocus,
              right: _cancelFocus,
            ),
            child: TextButton.icon(
              key: const ValueKey('tv-drama-search-phone-qr'),
              focusNode: _phoneSearchFocus,
              onPressed: _usePhoneSearch,
              icon: const Icon(Icons.qr_code_2_rounded),
              label: const Text('手機掃描 QR Code'),
            ),
          ),
        Focus(
          onKeyEvent: (node, event) => _searchActionKey(
            node,
            event,
            left: widget.onPhoneSearch != null
                ? _phoneSearchFocus
                : _submitFocus,
            right: _submitFocus,
          ),
          child: TextButton(
            key: const ValueKey('tv-drama-search-cancel'),
            focusNode: _cancelFocus,
            onPressed: () {
              _queryFocus.unfocus();
              SystemChannels.textInput.invokeMethod<void>('TextInput.hide');
              Navigator.of(context).pop();
            },
            child: const Text('取消'),
          ),
        ),
        Focus(
          onKeyEvent: (node, event) => _searchActionKey(
            node,
            event,
            left: _cancelFocus,
            right: widget.onPhoneSearch != null
                ? _phoneSearchFocus
                : _cancelFocus,
          ),
          child: FilledButton.icon(
            key: const ValueKey('tv-drama-search-submit'),
            focusNode: _submitFocus,
            onPressed: () {
              if (query.isEmpty) {
                _queryFocus.requestFocus();
                return;
              }
              _queryFocus.unfocus();
              SystemChannels.textInput.invokeMethod<void>('TextInput.hide');
              _submit(_query);
            },
            icon: const Icon(Icons.search_rounded),
            label: const Text('搜尋'),
          ),
        ),
      ],
    );
  }
}
