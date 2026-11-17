import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// A bounded, cross-platform release-notes viewport.
///
/// On touch and desktop platforms the scroll view supports normal gestures,
/// mouse-wheel input and an always-visible scrollbar. On TV remotes, the
/// focused viewport maps D-pad Up/Down (and PageUp/PageDown/Home/End) to
/// scrolling so long release notes remain reachable without a pointer.
class UpdateNotesScrollView extends StatefulWidget {
  const UpdateNotesScrollView({
    super.key,
    required this.text,
    required this.maxHeight,
    this.autofocus = false,
  });

  final String text;
  final double maxHeight;
  final bool autofocus;

  @override
  State<UpdateNotesScrollView> createState() => _UpdateNotesScrollViewState();
}

class _UpdateNotesScrollViewState extends State<UpdateNotesScrollView> {
  final ScrollController _scrollController = ScrollController();
  final FocusNode _focusNode = FocusNode(debugLabel: 'update-release-notes');

  @override
  void dispose() {
    _scrollController.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    if (!_scrollController.hasClients) return KeyEventResult.ignored;

    final position = _scrollController.position;
    if (!position.hasContentDimensions) return KeyEventResult.ignored;

    final key = event.logicalKey;
    final current = position.pixels;
    final min = position.minScrollExtent;
    final max = position.maxScrollExtent;
    final page = (position.viewportDimension * 0.72).clamp(80.0, 420.0);
    double? target;

    if (key == LogicalKeyboardKey.arrowDown ||
        key == LogicalKeyboardKey.pageDown) {
      target = (current + page).clamp(min, max).toDouble();
    } else if (key == LogicalKeyboardKey.arrowUp ||
        key == LogicalKeyboardKey.pageUp) {
      target = (current - page).clamp(min, max).toDouble();
    } else if (key == LogicalKeyboardKey.home) {
      target = min;
    } else if (key == LogicalKeyboardKey.end) {
      target = max;
    } else {
      return KeyEventResult.ignored;
    }

    // At either edge, allow the event to bubble so the focus system can move
    // from the notes area to the dialog's action buttons.
    if ((target - current).abs() < 1) return KeyEventResult.ignored;

    unawaited(
      _scrollController.animateTo(
        target,
        duration: const Duration(milliseconds: 160),
        curve: Curves.easeOutCubic,
      ),
    );
    return KeyEventResult.handled;
  }

  @override
  Widget build(BuildContext context) {
    final boundedHeight = widget.maxHeight.isFinite
        ? widget.maxHeight.clamp(120.0, 900.0).toDouble()
        : 420.0;

    return ConstrainedBox(
      constraints: BoxConstraints(maxHeight: boundedHeight),
      child: Scrollbar(
        controller: _scrollController,
        thumbVisibility: true,
        trackVisibility: true,
        interactive: true,
        child: Focus(
          focusNode: _focusNode,
          autofocus: widget.autofocus,
          onKeyEvent: _handleKeyEvent,
          child: SingleChildScrollView(
            key: const ValueKey('update-release-notes-scroll'),
            controller: _scrollController,
            primary: false,
            padding: const EdgeInsets.only(
              top: 4,
              right: 18,
              bottom: 8,
              left: 2,
            ),
            child: Text(widget.text),
          ),
        ),
      ),
    );
  }
}
