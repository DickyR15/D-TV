import Flutter
import UIKit
import DuanjuCore
import CFNetwork

@main
@objc class AppDelegate: FlutterAppDelegate, FlutterImplicitEngineDelegate {
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
      channel.setMethodCallHandler { call, result in
        switch call.method {
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
}
