#!/usr/bin/env python3
"""Apply D TV's iOS Sample Buffer PiP extension to the pinned media_kit_video package."""

from __future__ import annotations

import json
import re
import shutil
from pathlib import Path
from urllib.parse import unquote, urljoin, urlparse


ROOT = Path(__file__).resolve().parents[1]
MARKER = "DTV_SAMPLE_BUFFER_PIP_PATCH_V1"
EXPECTED_VERSION = "2.0.1"


def package_root() -> Path:
    config_path = ROOT / ".dart_tool" / "package_config.json"
    if not config_path.is_file():
        raise SystemExit("找不到 .dart_tool/package_config.json，請先執行 flutter pub get。")

    config = json.loads(config_path.read_text(encoding="utf-8"))
    for package in config.get("packages", []):
        if package.get("name") != "media_kit_video":
            continue
        root_uri = urljoin(config_path.resolve().as_uri(), package["rootUri"])
        parsed = urlparse(root_uri)
        if parsed.scheme != "file":
            raise SystemExit("media_kit_video package root 不是本機 file URI。")
        path = Path(unquote(parsed.path)).resolve()
        return path

    raise SystemExit("Flutter package graph 中找不到 media_kit_video。")


def replace_exact(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"無法安全套用 {label}：預期 1 個位置，實際找到 {count} 個。")
    return text.replace(old, new, 1)


def patch_texture_hw(package: Path, template: Path) -> None:
    # media_kit_video 2.0.1 is the pre-SwiftPM package layout:
    # native iOS plugin sources live under ios/Classes/plugin.
    path = package / "ios/Classes/plugin/TextureHW.swift"
    if not path.is_file():
        raise SystemExit(
            "media_kit_video 2.0.1 缺少預期的 iOS TextureHW.swift："
            + str(path)
        )
    text = path.read_text(encoding="utf-8")
    if MARKER in text:
        return

    text = replace_exact(
        text,
        "import OpenGLES\n",
        "import OpenGLES\nimport AVFoundation\nimport AVKit\nimport CoreImage\nimport CoreMedia\nimport CoreVideo\n",
        "TextureHW imports",
    )

    registry_code = """
  // D TV bridges the already-rendered MPV frames into AVKit's sample-buffer PiP.
  private final class WeakTextureHW {
    weak var value: TextureHW?
    init(_ value: TextureHW) { self.value = value }
  }

  private static let dtvInstanceLock = NSLock()
  private static var dtvInstances: [Int: WeakTextureHW] = [:]

  public static func startDTVPiP(
    handle: Int64,
    width: Int,
    height: Int,
    positionSeconds: Double,
    durationSeconds: Double,
    playing: Bool,
    eventHandler: @escaping (String, Any?) -> Void,
    completion: @escaping (Error?) -> Void
  ) {
    dtvInstanceLock.lock()
    let instance = dtvInstances[Int(handle)]?.value
    dtvInstanceLock.unlock()

    guard let instance else {
      completion(NSError(
        domain: "DTVSampleBufferPiP",
        code: 2,
        userInfo: [NSLocalizedDescriptionKey: "找不到目前 MPV 的 iOS 影格輸出"]
      ))
      return
    }

    guard #available(iOS 15.0, *) else {
      completion(NSError(
        domain: "DTVSampleBufferPiP",
        code: 3,
        userInfo: [NSLocalizedDescriptionKey: "Sample Buffer 子母畫面需要 iOS 15 或更新版本"]
      ))
      return
    }

    instance.startDTVPiP(
      width: width,
      height: height,
      positionSeconds: positionSeconds,
      durationSeconds: durationSeconds,
      playing: playing,
      eventHandler: eventHandler,
      completion: completion
    )
  }

  public static func stopDTVPiP(handle: Int64) {
    dtvInstanceLock.lock()
    let instance = dtvInstances[Int(handle)]?.value
    dtvInstanceLock.unlock()

    guard let instance else { return }
    if #available(iOS 15.0, *) {
      instance.stopDTVPiP()
    }
  }

  public static func updateDTVPiP(
    handle: Int64,
    playing: Bool,
    positionSeconds: Double
  ) {
    dtvInstanceLock.lock()
    let instance = dtvInstances[Int(handle)]?.value
    dtvInstanceLock.unlock()

    guard let instance else { return }
    if #available(iOS 15.0, *) {
      instance.updateDTVPiP(
        playing: playing,
        positionSeconds: positionSeconds
      )
    }
  }

  public static func completeDTVPiPSkip(handle: Int64) {
    dtvInstanceLock.lock()
    let instance = dtvInstances[Int(handle)]?.value
    dtvInstanceLock.unlock()

    guard let instance else { return }
    if #available(iOS 15.0, *) {
      instance.completeDTVPiPSkip()
    }
  }

  @available(iOS 15.0, *)
  private func completeDTVPiPSkip() {
    pipLock.lock()
    let renderer = pipRenderer
    pipLock.unlock()
    renderer?.completePendingSkip()
  }

  @available(iOS 15.0, *)
  private func updateDTVPiP(
    playing: Bool,
    positionSeconds: Double
  ) {
    pipLock.lock()
    let renderer = pipRenderer
    pipLock.unlock()
    renderer?.updatePlaybackState(
      playing: playing,
      positionSeconds: positionSeconds
    )
  }

  @available(iOS 15.0, *)
  private func startDTVPiP(
    width: Int,
    height: Int,
    positionSeconds: Double,
    durationSeconds: Double,
    playing: Bool,
    eventHandler: @escaping (String, Any?) -> Void,
    completion: @escaping (Error?) -> Void
  ) {
    pipLock.lock()
    let renderer: DTVSampleBufferPiPRenderer
    if let existing = pipRenderer {
      renderer = existing
    } else {
      let created = DTVSampleBufferPiPRenderer(eventHandler: eventHandler)
      pipRenderer = created
      renderer = created
    }
    pipLock.unlock()

    renderer.start(
      width: width,
      height: height,
      positionSeconds: positionSeconds,
      durationSeconds: durationSeconds,
      playing: playing,
      completion: completion
    )

    // If MPV is already paused, its render callback may not fire again.
    // Seed PiP once from the last completed texture frame; subsequent frames
    // arrive through TextureHW.render() as MPV continues playback.
    if !playing {
      DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(250)) { [weak self, weak renderer] in
        guard let self, let renderer,
              let current = self.textureContexts.current else { return }
        renderer.appendFrame(
          from: current.pixelBuffer,
          positionSeconds: positionSeconds
        )
      }
    }
  }

  @available(iOS 15.0, *)
  private func stopDTVPiP() {
    pipLock.lock()
    let renderer = pipRenderer
    pipRenderer = nil
    pipLock.unlock()
    renderer?.stop()
  }

"""
    text = replace_exact(
        text,
        "  public typealias UpdateCallback = () -> Void\n",
        "  public typealias UpdateCallback = () -> Void\n" + registry_code,
        "TextureHW D TV registry methods",
    )

    text = replace_exact(
        text,
        "  private var textureContexts = SwappableObjectManager<TextureGLESContext>(\n",
        "  private let pipLock = NSLock()\n  @available(iOS 15.0, *)\n  private var pipRenderer: DTVSampleBufferPiPRenderer?\n  private var textureContexts = SwappableObjectManager<TextureGLESContext>(\n",
        "TextureHW PiP state",
    )

    text = replace_exact(
        text,
        "    super.init()\n\n    self.initMPV()\n",
        """    super.init()

    Self.dtvInstanceLock.lock()
    Self.dtvInstances[Int(bitPattern: handle)] = WeakTextureHW(self)
    Self.dtvInstanceLock.unlock()

    self.initMPV()
""",
        "TextureHW instance registration",
    )

    text = replace_exact(
        text,
        "  deinit {\n    disposePixelBuffer()\n",
        """  deinit {
    if #available(iOS 15.0, *) {
      stopDTVPiP()
    }
    Self.dtvInstanceLock.lock()
    Self.dtvInstances.removeValue(forKey: Int(bitPattern: handle))
    Self.dtvInstanceLock.unlock()
    disposePixelBuffer()
""",
        "TextureHW instance cleanup",
    )

    text = replace_exact(
        text,
        "    mpv_render_context_render(renderContext, &params)\n\n    glFlush()\n",
        """    mpv_render_context_render(renderContext, &params)
    glFlush()

    // Copy the just-rendered MPV frame to a separate bounded pixel-buffer pool
    // before the source buffer re-enters Flutter's reusable texture ring.
    if #available(iOS 15.0, *) {
      pipLock.lock()
      let sampleBufferRenderer = pipRenderer
      pipLock.unlock()
      if let sampleBufferRenderer {
        var mediaTime: Double = 0
        let timeStatus = mpv_get_property(
          handle,
          "time-pos",
          MPV_FORMAT_DOUBLE,
          &mediaTime
        )
        if timeStatus >= 0 && mediaTime.isFinite {
          sampleBufferRenderer.appendFrame(
            from: textureContext!.pixelBuffer,
            positionSeconds: mediaTime
          )
        }
      }
    }
""",
        "TextureHW frame tap",
    )

    path.write_text(text + "\n// " + MARKER + "\n", encoding="utf-8")

    destination = path.parent / "DTVSampleBufferPiPRenderer.swift"
    shutil.copy2(template, destination)


def patch_plugin(package: Path) -> None:
    path = package / "ios/Classes/plugin/common/MediaKitVideoPlugin.swift"
    if not path.is_file():
        raise SystemExit(
            "media_kit_video 2.0.1 缺少預期的 iOS MediaKitVideoPlugin.swift："
            + str(path)
        )
    text = path.read_text(encoding="utf-8")
    if MARKER in text:
        return

    text = replace_exact(
        text,
        "#endif\n\npublic class MediaKitVideoPlugin",
        "#endif\n\nimport AVKit\n\npublic class MediaKitVideoPlugin",
        "MediaKitVideoPlugin AVKit import",
    )

    text = replace_exact(
        text,
        """    let channel = FlutterMethodChannel(
      name: CHANNEL_NAME,
      binaryMessenger: binaryMessenger
    )
    let instance = MediaKitVideoPlugin(
      registry: registry,
      channel: channel,
      utils: utils
    )
    registrar.addMethodCallDelegate(instance, channel: channel)
""",
        """    let channel = FlutterMethodChannel(
      name: CHANNEL_NAME,
      binaryMessenger: binaryMessenger
    )
    let pipChannel = FlutterMethodChannel(
      name: "duanju/mediakit_pip",
      binaryMessenger: binaryMessenger
    )
    let deviceChannel = FlutterMethodChannel(
      name: "duanju/device",
      binaryMessenger: binaryMessenger
    )
    let instance = MediaKitVideoPlugin(
      registry: registry,
      channel: channel,
      pipChannel: pipChannel,
      deviceChannel: deviceChannel,
      utils: utils
    )
    registrar.addMethodCallDelegate(instance, channel: channel)
    registrar.addMethodCallDelegate(instance, channel: pipChannel)
""",
        "MediaKitVideoPlugin PiP channels",
    )

    text = replace_exact(
        text,
        "  private let channel: FlutterMethodChannel\n  private let videoOutputManager: VideoOutputManager\n  private let utils: UtilsProtocol?\n",
        """  private let channel: FlutterMethodChannel
  private let pipChannel: FlutterMethodChannel
  private let deviceChannel: FlutterMethodChannel
  private let videoOutputManager: VideoOutputManager
  private let utils: UtilsProtocol?
""",
        "MediaKitVideoPlugin channel properties",
    )

    text = replace_exact(
        text,
        """  init(
    registry: FlutterTextureRegistry,
    channel: FlutterMethodChannel,
    utils: UtilsProtocol?
  ) {
    self.channel = channel
    videoOutputManager = VideoOutputManager(
""",
        """  init(
    registry: FlutterTextureRegistry,
    channel: FlutterMethodChannel,
    pipChannel: FlutterMethodChannel,
    deviceChannel: FlutterMethodChannel,
    utils: UtilsProtocol?
  ) {
    self.channel = channel
    self.pipChannel = pipChannel
    self.deviceChannel = deviceChannel
    videoOutputManager = VideoOutputManager(
""",
        "MediaKitVideoPlugin initializer",
    )

    text = replace_exact(
        text,
        """    switch call.method {
    case "VideoOutputManager.Create":
""",
        """    switch call.method {
    case "DTVPiP.Start":
      handleDTVPiPStart(call.arguments, result)
    case "DTVPiP.Stop":
      handleDTVPiPStop(call.arguments, result)
    case "DTVPiP.PlaybackStateChanged":
      handleDTVPiPPlaybackStateChanged(call.arguments, result)
    case "DTVPiP.SeekCompleted":
      handleDTVPiPSeekCompleted(call.arguments, result)
    case "DTVPiP.Status":
      result([
        "supported": AVPictureInPictureController.isPictureInPictureSupported()
      ])
    case "VideoOutputManager.Create":
""",
        "MediaKitVideoPlugin PiP method routing",
    )

    methods = """
  private func handleDTVPiPStart(
    _ arguments: Any?,
    _ result: @escaping FlutterResult
  ) {
    guard let args = arguments as? [String: Any],
          let handle = (args["handle"] as? NSNumber)?.int64Value else {
      result(FlutterError(
        code: "DTV_PIP_INVALID_HANDLE",
        message: "缺少 MPV 播放器識別碼",
        details: nil
      ))
      return
    }

    let width = max(1, (args["width"] as? NSNumber)?.intValue ?? 16)
    let height = max(1, (args["height"] as? NSNumber)?.intValue ?? 9)
    let positionMs = max(0, (args["positionMs"] as? NSNumber)?.int64Value ?? 0)
    let durationMs = max(0, (args["durationMs"] as? NSNumber)?.int64Value ?? 0)
    let playing = (args["playing"] as? NSNumber)?.boolValue ?? true

    TextureHW.startDTVPiP(
      handle: handle,
      width: width,
      height: height,
      positionSeconds: Double(positionMs) / 1000.0,
      durationSeconds: Double(durationMs) / 1000.0,
      playing: playing,
      eventHandler: { [weak self] method, payload in
        DispatchQueue.main.async {
          self?.deviceChannel.invokeMethod(method, arguments: payload)
        }
      },
      completion: { error in
        if let error {
          result(FlutterError(
            code: "DTV_PIP_START_FAILED",
            message: error.localizedDescription,
            details: nil
          ))
        } else {
          result(["started": true, "source": "mpv-sample-buffer"])
        }
      }
    )
  }

  private func handleDTVPiPStop(
    _ arguments: Any?,
    _ result: FlutterResult
  ) {
    guard let args = arguments as? [String: Any],
          let handle = (args["handle"] as? NSNumber)?.int64Value else {
      result(FlutterError(
        code: "DTV_PIP_INVALID_HANDLE",
        message: "缺少 MPV 播放器識別碼",
        details: nil
      ))
      return
    }
    TextureHW.stopDTVPiP(handle: handle)
    result(nil)
  }

  private func handleDTVPiPPlaybackStateChanged(
    _ arguments: Any?,
    _ result: FlutterResult
  ) {
    guard let args = arguments as? [String: Any],
          let handle = (args["handle"] as? NSNumber)?.int64Value else {
      result(FlutterError(
        code: "DTV_PIP_INVALID_HANDLE",
        message: "缺少 MPV 播放器識別碼",
        details: nil
      ))
      return
    }
    let playing = (args["playing"] as? NSNumber)?.boolValue ?? true
    let positionMs = max(0, (args["positionMs"] as? NSNumber)?.int64Value ?? 0)
    TextureHW.updateDTVPiP(
      handle: handle,
      playing: playing,
      positionSeconds: Double(positionMs) / 1000.0
    )
    result(nil)
  }

  private func handleDTVPiPSeekCompleted(
    _ arguments: Any?,
    _ result: FlutterResult
  ) {
    guard let args = arguments as? [String: Any],
          let handle = (args["handle"] as? NSNumber)?.int64Value else {
      result(FlutterError(
        code: "DTV_PIP_INVALID_HANDLE",
        message: "缺少 MPV 播放器識別碼",
        details: nil
      ))
      return
    }
    TextureHW.completeDTVPiPSkip(handle: handle)
    result(nil)
  }

"""
    class_suffix = """  private func handleExitNativeFullscreenMethodCall(
    _: Any?,
    _ result: FlutterResult
  ) {
    if utils == nil {
      return result(FlutterMethodNotImplemented)
    }

    utils?.exitNativeFullscreen()
    result(nil)
  }
}
"""
    text = replace_exact(
        text,
        class_suffix,
        class_suffix[:-2] + methods + "}\n",
        "MediaKitVideoPlugin PiP handlers",
    )

    path.write_text(text + "\n// " + MARKER + "\n", encoding="utf-8")


def main() -> None:
    package = package_root()
    pubspec = (package / "pubspec.yaml").read_text(encoding="utf-8")
    version_match = re.search(r"^version:\s*([^\s]+)", pubspec, re.MULTILINE)
    version = version_match.group(1) if version_match else "unknown"
    if version != EXPECTED_VERSION:
        raise SystemExit(
            f"media_kit_video 版本不符：預期 {EXPECTED_VERSION}，實際 {version}；停止套用以免破壞其他版本。"
        )

    template = ROOT / "scripts" / "ios" / "DTVSampleBufferPiPRenderer.swift"
    if not template.is_file():
        raise SystemExit("找不到 DTVSampleBufferPiPRenderer.swift 樣板。")

    podspec = package / "ios" / "media_kit_video.podspec"
    if not podspec.is_file():
        raise SystemExit("找不到 media_kit_video iOS Podspec。")
    podspec_text = podspec.read_text(encoding="utf-8")
    if "Classes/plugin/**/*.swift" not in podspec_text:
        raise SystemExit(
            "media_kit_video 2.0.1 Podspec 未包含 Classes/plugin/**/*.swift；"
            "停止套用以避免 PiP 原生檔案未被編譯。"
        )

    patch_texture_hw(package, template)
    patch_plugin(package)
    print(f"已套用 D TV Sample Buffer PiP 原生擴充：media_kit_video {version}")


if __name__ == "__main__":
    main()
