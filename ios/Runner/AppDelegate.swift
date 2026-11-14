import Flutter
import UIKit
import DuanjuCore
import CFNetwork
import AVKit
import AVFoundation

@main
@objc class AppDelegate: FlutterAppDelegate, FlutterImplicitEngineDelegate, AVPictureInPictureControllerDelegate {
  private var deviceChannel: FlutterMethodChannel?
  private var pipPlayer: AVPlayer?
  private var pipPlayerLayer: AVPlayerLayer?
  private var pipController: AVPictureInPictureController?
  private var pipHostView: UIView?
  private var pipPossibleObservation: NSKeyValueObservation?
  private var pipItemStatusObservation: NSKeyValueObservation?
  private var pipPrepareTimeout: DispatchWorkItem?
  private var pendingPipPrepareResult: FlutterResult?
  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    ZgjEnsureCoreLinked()
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  func didInitializeImplicitFlutterEngine(_ engineBridge: FlutterImplicitEngineBridge) {
    GeneratedPluginRegistrant.register(with: engineBridge.pluginRegistry)
    if let registrar = engineBridge.pluginRegistry.registrar(forPlugin: "DeviceSettings") {
      let channel = FlutterMethodChannel(name: "duanju/device", binaryMessenger: registrar.messenger())
      self.deviceChannel = channel
      channel.setMethodCallHandler { [weak self] call, result in
        guard let self else { result(FlutterMethodNotImplemented); return }
        switch call.method {
        case "pictureInPictureStatus":
          result([
            "supported": AVPictureInPictureController.isPictureInPictureSupported(),
            "active": self.pipController?.isPictureInPictureActive ?? false
          ])
        case "preparePictureInPicture":
          self.preparePictureInPicture(arguments: call.arguments, result: result)
        case "enterPictureInPicture":
          guard let controller = self.pipController else {
            result(FlutterError(code: "PIP_NOT_PREPARED", message: "尚未準備 iOS 子母畫面", details: nil))
            return
          }
          guard controller.isPictureInPicturePossible else {
            result(FlutterError(code: "PIP_NOT_POSSIBLE", message: "目前影片尚未具備子母畫面條件", details: nil))
            return
          }
          controller.startPictureInPicture()
          result(["supported": true, "requested": true, "active": controller.isPictureInPictureActive])
        case "finishPictureInPicture":
          let positionMs = self.pipCurrentPositionMilliseconds()
          self.cleanupPictureInPicture()
          result(["positionMs": positionMs])
        case "cancelPictureInPicture":
          self.cleanupPictureInPicture()
          result(nil)
        case "systemAccentColor":
          func argb(_ style: UIUserInterfaceStyle) -> Int64? {
            let traits = UITraitCollection(userInterfaceStyle: style)
            let color = UIColor.systemBlue.resolvedColor(with: traits)
            var red: CGFloat = 0
            var green: CGFloat = 0
            var blue: CGFloat = 0
            var alpha: CGFloat = 0
            guard color.getRed(&red, green: &green, blue: &blue, alpha: &alpha) else {
              return nil
            }
            let a = UInt32((alpha * 255).rounded())
            let r = UInt32((red * 255).rounded())
            let g = UInt32((green * 255).rounded())
            let b = UInt32((blue * 255).rounded())
            return Int64((a << 24) | (r << 16) | (g << 8) | b)
          }
          guard let light = argb(.light), let dark = argb(.dark) else {
            result(nil)
            return
          }
          result(["light": light, "dark": dark])
        case "systemProxy":
          let settings = CFNetworkCopySystemProxySettings()?.takeRetainedValue() as? [String: Any] ?? [:]
          func address(_ prefix: String) -> String {
            guard (settings["\(prefix)Enable"] as? NSNumber)?.boolValue == true,
                  let host = settings["\(prefix)Proxy"] as? String,
                  let port = settings["\(prefix)Port"] as? NSNumber,
                  !host.isEmpty, port.intValue > 0 else { return "" }
            let name = host.contains(":") ? "[\(host)]" : host
            return "http://\(name):\(port.intValue)"
          }
          let http = address("HTTP")
          let https = address("HTTPS")
          result(["http": http, "https": https.isEmpty ? http : https,
                  "bypass": settings["ExceptionsList"] as? [String] ?? [],
                  "pac": (settings["ProxyAutoConfigEnable"] as? NSNumber)?.boolValue == true])
        case "openExternalUrl":
          guard
            let arguments = call.arguments as? [String: Any],
            let rawURL = arguments["url"] as? String,
            let url = URL(string: rawURL),
            let scheme = url.scheme?.lowercased(),
            (scheme == "https" || scheme == "http"),
            url.host != nil
          else {
            result(FlutterError(
              code: "invalid_url",
              message: "更新網址無效",
              details: nil
            ))
            return
          }
          DispatchQueue.main.async {
            UIApplication.shared.open(url, options: [:]) { opened in
              if opened {
                result(nil)
              } else {
                result(FlutterError(
                  code: "open_url_failed",
                  message: "無法開啟外部瀏覽器",
                  details: rawURL
                ))
              }
            }
          }
        default:
          result(FlutterMethodNotImplemented)
        }
      }
    }
  }
  private func pipCurrentPositionMilliseconds() -> Int64 {
    guard let player = pipPlayer else { return 0 }
    let seconds = player.currentTime().seconds
    guard seconds.isFinite, seconds >= 0 else { return 0 }
    return Int64((seconds * 1000).rounded())
  }

  private func completePipPreparation(error: FlutterError? = nil) {
    guard let callback = pendingPipPrepareResult else { return }
    pendingPipPrepareResult = nil
    pipPrepareTimeout?.cancel()
    pipPrepareTimeout = nil
    if let error {
      callback(error)
    } else {
      callback(["possible": true])
    }
  }

  private func preparePictureInPicture(arguments: Any?, result: @escaping FlutterResult) {
    guard AVPictureInPictureController.isPictureInPictureSupported() else {
      result(FlutterError(code: "PIP_UNSUPPORTED", message: "此 iOS 裝置不支援系統子母畫面", details: nil))
      return
    }
    guard let args = arguments as? [String: Any],
          let rawURL = args["url"] as? String,
          let url = URL(string: rawURL),
          let scheme = url.scheme?.lowercased(),
          ["http", "https", "file"].contains(scheme) else {
      result(FlutterError(code: "PIP_INVALID_URL", message: "目前片源網址無法交給 iOS 原生播放器", details: nil))
      return
    }

    cleanupPictureInPicture()
    do {
      try AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
      try AVAudioSession.sharedInstance().setActive(true)
    } catch {
      result(FlutterError(code: "PIP_AUDIO_SESSION", message: error.localizedDescription, details: nil))
      return
    }

    let headers = args["headers"] as? [String: String] ?? [:]
    let assetOptions: [String: Any] = headers.isEmpty
      ? [:]
      : ["AVURLAssetHTTPHeaderFieldsKey": headers]
    let asset = AVURLAsset(url: url, options: assetOptions)
    let item = AVPlayerItem(asset: asset)
    let player = AVPlayer(playerItem: item)
    player.automaticallyWaitsToMinimizeStalling = true

    guard let windowScene = UIApplication.shared.connectedScenes
      .compactMap({ $0 as? UIWindowScene })
      .first(where: { $0.activationState == .foregroundActive }),
      let window = windowScene.windows.first(where: { $0.isKeyWindow }) ?? windowScene.windows.first else {
      result(FlutterError(code: "PIP_NO_WINDOW", message: "找不到目前的 iOS 視窗", details: nil))
      return
    }

    let hostView = UIView(frame: CGRect(x: 1, y: 1, width: 2, height: 2))
    hostView.backgroundColor = .clear
    hostView.alpha = 0.01
    hostView.isUserInteractionEnabled = false
    window.addSubview(hostView)

    let layer = AVPlayerLayer(player: player)
    layer.frame = hostView.bounds
    layer.videoGravity = .resizeAspect
    hostView.layer.addSublayer(layer)

    guard let controller = AVPictureInPictureController(playerLayer: layer) else {
      hostView.removeFromSuperview()
      result(FlutterError(code: "PIP_CONTROLLER", message: "無法建立 iOS 子母畫面控制器", details: nil))
      return
    }
    controller.delegate = self
    if #available(iOS 14.2, *) {
      controller.canStartPictureInPictureAutomaticallyFromInline = false
    }

    pipPlayer = player
    pipPlayerLayer = layer
    pipController = controller
    pipHostView = hostView
    pendingPipPrepareResult = result

    pipPossibleObservation = controller.observe(\.isPictureInPicturePossible, options: [.initial, .new]) { [weak self] observed, _ in
      guard let self, observed.isPictureInPicturePossible else { return }
      DispatchQueue.main.async { self.completePipPreparation() }
    }
    pipItemStatusObservation = item.observe(\.status, options: [.initial, .new]) { [weak self] observed, _ in
      guard let self, observed.status == .failed else { return }
      DispatchQueue.main.async {
        self.completePipPreparation(error: FlutterError(
          code: "PIP_STREAM_UNSUPPORTED",
          message: observed.error?.localizedDescription ?? "iOS 原生播放器無法讀取此片源",
          details: nil
        ))
      }
    }

    let positionMs = max(0, args["positionMs"] as? Int64 ?? 0)
    let startTime = CMTime(value: positionMs, timescale: 1000)
    if positionMs > 0 {
      player.seek(to: startTime) { [weak player] _ in player?.play() }
    } else {
      player.play()
    }

    let timeout = DispatchWorkItem { [weak self] in
      guard let self, self.pendingPipPrepareResult != nil else { return }
      self.completePipPreparation(error: FlutterError(
        code: "PIP_PREPARE_TIMEOUT",
        message: "iOS 原生影片未能在期限內準備好子母畫面；此片源可能不相容",
        details: nil
      ))
    }
    pipPrepareTimeout = timeout
    DispatchQueue.main.asyncAfter(deadline: .now() + 10, execute: timeout)
  }

  private func cleanupPictureInPicture() {
    pendingPipPrepareResult = nil
    pipPrepareTimeout?.cancel()
    pipPrepareTimeout = nil
    pipPossibleObservation?.invalidate()
    pipPossibleObservation = nil
    pipItemStatusObservation?.invalidate()
    pipItemStatusObservation = nil
    if pipController?.isPictureInPictureActive == true {
      pipController?.stopPictureInPicture()
    }
    pipController?.delegate = nil
    pipController = nil
    pipPlayer?.pause()
    pipPlayer = nil
    pipPlayerLayer?.removeFromSuperlayer()
    pipPlayerLayer = nil
    pipHostView?.removeFromSuperview()
    pipHostView = nil
  }

  func pictureInPictureControllerDidStartPictureInPicture(_ pictureInPictureController: AVPictureInPictureController) {
    deviceChannel?.invokeMethod("pictureInPictureChanged", arguments: ["active": true])
  }

  func pictureInPictureControllerDidStopPictureInPicture(_ pictureInPictureController: AVPictureInPictureController) {
    let positionMs = pipCurrentPositionMilliseconds()
    pipPlayer?.pause()
    deviceChannel?.invokeMethod("pictureInPictureRestored", arguments: ["positionMs": positionMs])
  }

  func pictureInPictureController(
    _ pictureInPictureController: AVPictureInPictureController,
    restoreUserInterfaceForPictureInPictureStopWithCompletionHandler completionHandler: @escaping (Bool) -> Void
  ) {
    completionHandler(true)
  }

  func pictureInPictureController(
    _ pictureInPictureController: AVPictureInPictureController,
    failedToStartPictureInPictureWithError error: Error
  ) {
    deviceChannel?.invokeMethod("pictureInPictureError", arguments: error.localizedDescription)
  }

}
