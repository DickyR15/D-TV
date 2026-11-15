import AVFoundation
import AVKit
import CoreImage
import CoreMedia
import CoreVideo
import UIKit

@available(iOS 15.0, *)
final class DTVSampleBufferPiPRenderer: NSObject,
    AVPictureInPictureControllerDelegate,
    AVPictureInPictureSampleBufferPlaybackDelegate {
  typealias EventHandler = (String, Any?) -> Void

  private let eventHandler: EventHandler
  private let ciContext = CIContext(options: [.cacheIntermediates: false])
  private let stateLock = NSLock()
  private let frameQueue = DispatchQueue(label: "com.dickyr15.dtv.sample-buffer-pip.frames", qos: .userInitiated)

  private var enabled = false
  private var paused = false
  private var pendingFrames = 0
  private var lastMediaTime: Double?
  private var durationSeconds: Double = 0
  private var pool: CVPixelBufferPool?
  private var poolSize = CGSize.zero
  private var formatDescription: CMVideoFormatDescription?
  private var formatSize = CGSize.zero

  private var hostView: UIView?
  private var displayLayer: AVSampleBufferDisplayLayer?
  private var timebase: CMTimebase?
  private var contentSource: AVPictureInPictureController.ContentSource?
  private var controller: AVPictureInPictureController?
  private var possibleObservation: NSKeyValueObservation?
  private var startTimeout: DispatchWorkItem?
  private var pendingStart: ((Error?) -> Void)?
  private var pendingSkipCompletion: (@Sendable () -> Void)?
  // AVKit may silently drop startPictureInPicture() in the same main-queue
  // turn in which its controller was created, even when isPictureInPicturePossible
  // already reports true. Plezy's MPV PiP implementation guards this explicitly.
  private var controllerSettled = false
  // A manual start is issued once only, after a real sample has reached the layer.
  private var startIssued = false
  private var hasEnqueuedFirstFrame = false
  private var startReadinessAttempts = 0
  private var startReadinessRetry: DispatchWorkItem?
  private let maximumStartReadinessAttempts = 40
  private let startReadinessRetryInterval = DispatchTimeInterval.milliseconds(50)
  private var appendedFrameCount = 0
  private var enqueuedFrameCount = 0

  private func diagnose(_ message: String) {
    DispatchQueue.main.async { [weak self] in
      self?.eventHandler("pictureInPictureDiagnostic", message)
    }
  }

  func reportDiagnostic(_ message: String) {
    diagnose(message)
  }

  init(eventHandler: @escaping EventHandler) {
    self.eventHandler = eventHandler
    super.init()
  }

  func start(
    width: Int,
    height: Int,
    positionSeconds: Double,
    durationSeconds: Double,
    playing: Bool,
    completion: @escaping (Error?) -> Void
  ) {
    DispatchQueue.main.async {
      guard AVPictureInPictureController.isPictureInPictureSupported() else {
        completion(self.error("此 iOS 裝置不支援系統子母畫面"))
        return
      }

      self.diagnose(
        "start request width=\(width) height=\(height) playing=\(playing) " +
        "position=\(positionSeconds) controllerExists=\(self.controller != nil)"
      )
      self.durationSeconds = durationSeconds.isFinite ? max(0, durationSeconds) : 0
      self.startIssued = false
      self.hasEnqueuedFirstFrame = false
      self.startReadinessAttempts = 0
      self.startReadinessRetry?.cancel()
      self.startReadinessRetry = nil
      self.stateLock.lock()
      self.paused = !playing
      self.enabled = true
      self.lastMediaTime = nil
      self.appendedFrameCount = 0
      self.enqueuedFrameCount = 0
      self.stateLock.unlock()

      do {
        try AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
        try AVAudioSession.sharedInstance().setActive(true)
      } catch {
        completion(error)
        return
      }

      guard self.ensureDisplayLayer(width: width, height: height, positionSeconds: positionSeconds) else {
        self.diagnose("display layer creation failed; no foreground UIWindowScene")
        completion(self.error("找不到目前的 iOS 視窗，無法建立子母畫面"))
        return
      }
      self.diagnose(
        "display layer ready status=\(String(describing: self.displayLayer?.status)) " +
        "error=\(self.displayLayer?.error?.localizedDescription ?? "none")"
      )

      if self.controller == nil {
        guard let layer = self.displayLayer else {
          completion(self.error("無法建立 iOS 影格顯示層"))
          return
        }
        let source = AVPictureInPictureController.ContentSource(
          sampleBufferDisplayLayer: layer,
          playbackDelegate: self
        )
        let controller = AVPictureInPictureController(contentSource: source)
        controller.delegate = self
        controller.canStartPictureInPictureAutomaticallyFromInline = false
        self.contentSource = source
        self.controller = controller
        self.controllerSettled = false
        self.possibleObservation = controller.observe(
          \.isPictureInPicturePossible,
          options: [.initial, .new]
        ) { [weak self] observed, _ in
          self?.diagnose(
            "isPictureInPicturePossible=\(observed.isPictureInPicturePossible) " +
            "layerStatus=\(String(describing: self?.displayLayer?.status))"
          )
          DispatchQueue.main.async {
            self?.startWhenPossible()
          }
        }

        // Do not start in the same main-queue turn as controller creation.
        // Plezy documents that AVKit can silently discard that request.
        DispatchQueue.main.async { [weak self, weak controller] in
          guard let self, let controller, self.controller === controller else { return }
          self.controllerSettled = true
          self.diagnose("controller settled after main-queue turn")
          self.startWhenPossible()
        }
      }

      guard self.pendingStart == nil else {
        completion(self.error("子母畫面正在啟動，請稍後再試"))
        return
      }
      self.pendingStart = completion
      self.startReadinessAttempts = 0
      // startWhenPossible waits for all four conditions: controller settled,
      // AVKit says possible, a timebase exists, and the first frame is display-ready.
      self.startWhenPossible()
    }
  }

  private func ensureDisplayLayer(
    width: Int,
    height: Int,
    positionSeconds: Double
  ) -> Bool {
    if let hostView, let displayLayer {
      updateHostFrame(hostView, layer: displayLayer, width: width, height: height)
      displayLayer.flush()
      if let timebase {
        CMTimebaseSetTime(
          timebase,
          time: CMTime(seconds: max(0, positionSeconds), preferredTimescale: 600)
        )
        CMTimebaseSetRate(timebase, rate: paused ? 0 : 1)
      }
      return true
    }

    guard let windowScene = UIApplication.shared.connectedScenes
      .compactMap({ $0 as? UIWindowScene })
      .first(where: { $0.activationState == .foregroundActive }),
      let window = windowScene.windows.first(where: { $0.isKeyWindow }) ?? windowScene.windows.first else {
      return false
    }

    let view = UIView(frame: hostFrame(width: width, height: height))
    view.backgroundColor = .clear
    view.alpha = 0.01
    view.isUserInteractionEnabled = false
    window.addSubview(view)

    let layer = AVSampleBufferDisplayLayer()
    layer.videoGravity = .resizeAspect
    layer.frame = view.bounds
    view.layer.addSublayer(layer)

    var newTimebase: CMTimebase?
    let timebaseStatus = CMTimebaseCreateWithSourceClock(
      allocator: kCFAllocatorDefault,
      sourceClock: CMClockGetHostTimeClock(),
      timebaseOut: &newTimebase
    )
    guard timebaseStatus == noErr, let newTimebase else {
      view.removeFromSuperview()
      return false
    }
    CMTimebaseSetTime(
      newTimebase,
      time: CMTime(seconds: max(0, positionSeconds), preferredTimescale: 600)
    )
    CMTimebaseSetRate(newTimebase, rate: paused ? 0 : 1)
    layer.controlTimebase = newTimebase

    hostView = view
    displayLayer = layer
    timebase = newTimebase
    return true
  }

  private func hostFrame(width: Int, height: Int) -> CGRect {
    let safeWidth = max(1, width)
    let safeHeight = max(1, height)
    let ratio = CGFloat(safeWidth) / CGFloat(safeHeight)
    let maxDimension: CGFloat = 320
    let size = ratio >= 1
      ? CGSize(width: maxDimension, height: maxDimension / ratio)
      : CGSize(width: maxDimension * ratio, height: maxDimension)
    return CGRect(origin: CGPoint(x: 1, y: 1), size: size)
  }

  private func updateHostFrame(
    _ view: UIView,
    layer: AVSampleBufferDisplayLayer,
    width: Int,
    height: Int
  ) {
    let frame = hostFrame(width: width, height: height)
    view.frame = frame
    layer.frame = view.bounds
  }

  func appendFrame(from source: CVPixelBuffer, positionSeconds: Double) {
    stateLock.lock()
    let shouldAppend = enabled && pendingFrames < 2
    if shouldAppend {
      pendingFrames += 1
    }
    let previousTime = lastMediaTime
    if positionSeconds.isFinite {
      lastMediaTime = positionSeconds
    }
    stateLock.unlock()

    guard shouldAppend else { return }
    defer { finishPendingFrame() }

    // Copy synchronously before TextureHW returns this CVPixelBuffer to
    // Flutter's reusable three-buffer texture ring. The PiP sample owns a
    // separate pooled buffer after this copy completes.
    guard let sampleBuffer = frameQueue.sync(execute: {
      makeSampleBuffer(from: source, positionSeconds: positionSeconds)
    }) else {
      stateLock.lock()
      let firstFailure = appendedFrameCount == 0
      stateLock.unlock()
      if firstFailure {
        diagnose(
          "makeSampleBuffer failed source=\(CVPixelBufferGetWidth(source))x\(CVPixelBufferGetHeight(source)) " +
          "format=\(CVPixelBufferGetPixelFormatType(source))"
        )
      }
      return
    }
    stateLock.lock()
    appendedFrameCount += 1
    let reportFirstFrame = appendedFrameCount == 1
    stateLock.unlock()
    if reportFirstFrame {
      diagnose(
        "first MPV frame copied size=\(CVPixelBufferGetWidth(source))x\(CVPixelBufferGetHeight(source)) " +
        "format=\(CVPixelBufferGetPixelFormatType(source)) pts=\(positionSeconds)"
      )
    }

    let needsTimelineReset = previousTime != nil &&
      positionSeconds.isFinite &&
      abs(positionSeconds - previousTime!) > 0.75

    DispatchQueue.main.async { [weak self] in
      guard let self else { return }
      guard self.enabled, let layer = self.displayLayer else { return }

      if needsTimelineReset {
        layer.flush()
        if let timebase = self.timebase, positionSeconds.isFinite {
          CMTimebaseSetTime(
            timebase,
            time: CMTime(seconds: max(0, positionSeconds), preferredTimescale: 600)
          )
          CMTimebaseSetRate(timebase, rate: self.paused ? 0 : 1)
        }
      }

      if layer.isReadyForMoreMediaData {
        layer.enqueue(sampleBuffer)
        self.stateLock.lock()
        self.enqueuedFrameCount += 1
        let count = self.enqueuedFrameCount
        self.stateLock.unlock()
        if !self.hasEnqueuedFirstFrame {
          self.hasEnqueuedFirstFrame = true
          let displayReady: Bool
          if #available(iOS 17.4, *) {
            displayReady = layer.isReadyForDisplay
          } else {
            displayReady = true
          }
          self.diagnose(
            "first sample enqueued count=\(count) displayReady=\(displayReady) " +
            "layerStatus=\(String(describing: layer.status)) " +
            "layerError=\(layer.error?.localizedDescription ?? "none")"
          )
          self.startWhenPossible()
        }
      } else {
        self.stateLock.lock()
        let count = self.enqueuedFrameCount
        self.stateLock.unlock()
        if count == 0 {
          self.diagnose(
            "sample not enqueued; layer not ready status=\(String(describing: layer.status)) " +
            "error=\(layer.error?.localizedDescription ?? "none")"
          )
        }
      }
    }
  }

  private func makeSampleBuffer(
    from source: CVPixelBuffer,
    positionSeconds: Double
  ) -> CMSampleBuffer? {
    let sourceWidth = CVPixelBufferGetWidth(source)
    let sourceHeight = CVPixelBufferGetHeight(source)
    guard sourceWidth > 0, sourceHeight > 0 else { return nil }

    let scale = min(1.0, 720.0 / Double(max(sourceWidth, sourceHeight)))
    let outputWidth = max(2, Int((Double(sourceWidth) * scale).rounded()) & ~1)
    let outputHeight = max(2, Int((Double(sourceHeight) * scale).rounded()) & ~1)
    let outputSize = CGSize(width: outputWidth, height: outputHeight)

    if pool == nil || poolSize != outputSize {
      var newPool: CVPixelBufferPool?
      let poolAttributes = [
        kCVPixelBufferPoolMinimumBufferCountKey as String: 4
      ] as CFDictionary
      let pixelAttributes = [
        kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA,
        kCVPixelBufferWidthKey as String: outputWidth,
        kCVPixelBufferHeightKey as String: outputHeight,
        kCVPixelBufferMetalCompatibilityKey as String: true,
        kCVPixelBufferIOSurfacePropertiesKey as String: [:]
      ] as CFDictionary
      guard CVPixelBufferPoolCreate(
        kCFAllocatorDefault,
        poolAttributes,
        pixelAttributes,
        &newPool
      ) == kCVReturnSuccess, let newPool else {
        return nil
      }
      pool = newPool
      poolSize = outputSize
      formatDescription = nil
      formatSize = .zero
    }

    guard let pool else { return nil }
    var destination: CVPixelBuffer?
    let threshold = [
      kCVPixelBufferPoolAllocationThresholdKey as String: 8
    ] as CFDictionary
    guard CVPixelBufferPoolCreatePixelBufferWithAuxAttributes(
      kCFAllocatorDefault,
      pool,
      threshold,
      &destination
    ) == kCVReturnSuccess, let destination else {
      return nil
    }

    let image = CIImage(cvPixelBuffer: source).transformed(
      by: CGAffineTransform(scaleX: scale, y: scale)
    )
    ciContext.render(
      image,
      to: destination,
      bounds: CGRect(origin: .zero, size: outputSize),
      colorSpace: CGColorSpaceCreateDeviceRGB()
    )

    if formatDescription == nil || formatSize != outputSize {
      var newFormat: CMVideoFormatDescription?
      guard CMVideoFormatDescriptionCreateForImageBuffer(
        allocator: kCFAllocatorDefault,
        imageBuffer: destination,
        formatDescriptionOut: &newFormat
      ) == noErr, let newFormat else {
        return nil
      }
      formatDescription = newFormat
      formatSize = outputSize
    }

    guard let formatDescription else { return nil }
    let presentationTime = CMTime(
      seconds: positionSeconds.isFinite ? max(0, positionSeconds) : 0,
      preferredTimescale: 600
    )
    var timing = CMSampleTimingInfo(
      duration: CMTime(value: 1, timescale: 30),
      presentationTimeStamp: presentationTime,
      decodeTimeStamp: .invalid
    )
    var sampleBuffer: CMSampleBuffer?
    let status = CMSampleBufferCreateForImageBuffer(
      allocator: kCFAllocatorDefault,
      imageBuffer: destination,
      dataReady: true,
      makeDataReadyCallback: nil,
      refcon: nil,
      formatDescription: formatDescription,
      sampleTiming: &timing,
      sampleBufferOut: &sampleBuffer
    )
    guard status == noErr, let sampleBuffer else { return nil }

    // MPV already paces its frames. Mark samples for immediate display rather
    // than asking AVSampleBufferDisplayLayer to schedule them a second time.
    if let attachments = CMSampleBufferGetSampleAttachmentsArray(
      sampleBuffer,
      createIfNecessary: true
    ), CFArrayGetCount(attachments) > 0 {
      let dictionary = unsafeBitCast(
        CFArrayGetValueAtIndex(attachments, 0),
        to: CFMutableDictionary.self
      )
      CFDictionarySetValue(
        dictionary,
        Unmanaged.passUnretained(kCMSampleAttachmentKey_DisplayImmediately).toOpaque(),
        Unmanaged.passUnretained(kCFBooleanTrue).toOpaque()
      )
    }
    return sampleBuffer
  }

  private func finishPendingFrame() {
    stateLock.lock()
    pendingFrames = max(0, pendingFrames - 1)
    stateLock.unlock()
  }

  private func scheduleStartTimeout() {
    startTimeout?.cancel()
    let timeout = DispatchWorkItem { [weak self] in
      guard let self, let completion = self.pendingStart else { return }
      let possible = self.controller?.isPictureInPicturePossible ?? false
      let active = self.controller?.isPictureInPictureActive ?? false
      let displayReady: Bool
      if #available(iOS 17.4, *) {
        displayReady = self.displayLayer?.isReadyForDisplay ?? false
      } else {
        displayReady = self.hasEnqueuedFirstFrame
      }
      let layerStatus = self.displayLayer.map { String(describing: $0.status) } ?? "nil"
      let layerError = self.displayLayer?.error?.localizedDescription ?? "none"
      self.stateLock.lock()
      let appended = self.appendedFrameCount
      let enqueued = self.enqueuedFrameCount
      self.enabled = false
      self.stateLock.unlock()
      self.diagnose(
        "start timeout controllerSettled=\(self.controllerSettled) startIssued=\(self.startIssued) " +
        "possible=\(possible) active=\(active) hasFrame=\(self.hasEnqueuedFirstFrame) " +
        "displayReady=\(displayReady) timebase=\(self.displayLayer?.controlTimebase != nil) " +
        "layerStatus=\(layerStatus) layerError=\(layerError) " +
        "appendedFrames=\(appended) enqueuedFrames=\(enqueued)"
      )
      self.pendingStart = nil
      self.startIssued = false
      self.startTimeout = nil
      self.startReadinessRetry?.cancel()
      self.startReadinessRetry = nil
      completion(self.error("iOS 子母畫面啟動逾時，已保留原播放器"))
    }
    startTimeout = timeout
    DispatchQueue.main.asyncAfter(deadline: .now() + 10, execute: timeout)
  }

  private func failStartReadiness() {
    guard let completion = pendingStart else { return }
    let possible = controller?.isPictureInPicturePossible ?? false
    let displayReady: Bool
    if #available(iOS 17.4, *) {
      displayReady = displayLayer?.isReadyForDisplay ?? false
    } else {
      displayReady = hasEnqueuedFirstFrame
    }
    diagnose(
      "start readiness failed controllerSettled=\(controllerSettled) possible=\(possible) " +
      "hasFrame=\(hasEnqueuedFirstFrame) displayReady=\(displayReady) " +
      "timebase=\(displayLayer?.controlTimebase != nil) attempts=\(startReadinessAttempts)"
    )
    pendingStart = nil
    startIssued = false
    startReadinessRetry?.cancel()
    startReadinessRetry = nil
    completion(error("iOS 子母畫面等待首張可顯示影格逾時"))
  }

  private func startWhenPossible() {
    guard Thread.isMainThread else {
      DispatchQueue.main.async { [weak self] in self?.startWhenPossible() }
      return
    }
    guard let controller, pendingStart != nil, !startIssued else { return }
    guard controllerSettled else { return }

    let possible = controller.isPictureInPicturePossible
    let timebaseReady = displayLayer?.controlTimebase != nil
    let displayReady: Bool
    if #available(iOS 17.4, *) {
      displayReady = displayLayer?.isReadyForDisplay ?? false
    } else {
      displayReady = hasEnqueuedFirstFrame
    }

    guard possible, timebaseReady, hasEnqueuedFirstFrame, displayReady else {
      guard startReadinessRetry == nil else { return }
      guard startReadinessAttempts < maximumStartReadinessAttempts else {
        failStartReadiness()
        return
      }
      let retry = DispatchWorkItem { [weak self] in
        guard let self else { return }
        self.startReadinessRetry = nil
        self.startReadinessAttempts += 1
        self.startWhenPossible()
      }
      startReadinessRetry = retry
      DispatchQueue.main.asyncAfter(
        deadline: .now() + startReadinessRetryInterval,
        execute: retry
      )
      return
    }

    startReadinessRetry?.cancel()
    startReadinessRetry = nil
    startIssued = true
    controller.invalidatePlaybackState()
    diagnose(
      "AVKit start gate passed controllerSettled=true possible=true timebase=true " +
      "hasFrame=true displayReady=true attempts=\(startReadinessAttempts)"
    )
    // Keep pendingStart until didStart / failedToStart; this call is issued once.
    controller.startPictureInPicture()
    scheduleStartTimeout()
  }

  func updatePlaybackState(playing: Bool, positionSeconds: Double) {
    stateLock.lock()
    paused = !playing
    if positionSeconds.isFinite {
      lastMediaTime = positionSeconds
    }
    stateLock.unlock()

    DispatchQueue.main.async { [weak self] in
      guard let self else { return }
      if let timebase = self.timebase, positionSeconds.isFinite {
        CMTimebaseSetTime(
          timebase,
          time: CMTime(seconds: max(0, positionSeconds), preferredTimescale: 600)
        )
        CMTimebaseSetRate(timebase, rate: playing ? 1 : 0)
      }
      self.controller?.invalidatePlaybackState()
    }
  }

  func completePendingSkip() {
    DispatchQueue.main.async { [weak self] in
      guard let self else { return }
      let completion = self.pendingSkipCompletion
      self.pendingSkipCompletion = nil
      completion?()
    }
  }

  func stop() {
    DispatchQueue.main.async {
      self.diagnose("stop requested active=\(self.controller?.isPictureInPictureActive ?? false)")
      self.startReadinessRetry?.cancel()
      self.startReadinessRetry = nil
      self.startIssued = false
      self.controllerSettled = false
      self.hasEnqueuedFirstFrame = false
      self.stateLock.lock()
      self.enabled = false
      self.stateLock.unlock()
      self.pendingStart?(self.error("子母畫面已取消"))
      self.pendingStart = nil
      self.startTimeout?.cancel()
      self.startTimeout = nil
      self.possibleObservation?.invalidate()
      self.possibleObservation = nil
      if self.controller?.isPictureInPictureActive == true {
        self.controller?.stopPictureInPicture()
      }
      self.displayLayer?.flushAndRemoveImage()
      self.hostView?.removeFromSuperview()
      self.hostView = nil
      self.displayLayer = nil
      self.timebase = nil
      self.contentSource = nil
      self.controller?.delegate = nil
      self.controller = nil
      self.pool = nil
      self.formatDescription = nil
      self.poolSize = .zero
    }
  }

  private func error(_ message: String) -> NSError {
    NSError(
      domain: "DTVSampleBufferPiP",
      code: 1,
      userInfo: [NSLocalizedDescriptionKey: message]
    )
  }

  func pictureInPictureController(
    _ pictureInPictureController: AVPictureInPictureController,
    setPlaying playing: Bool
  ) {
    stateLock.lock()
    paused = !playing
    stateLock.unlock()
    if let timebase {
      CMTimebaseSetRate(timebase, rate: playing ? 1 : 0)
    }
    eventHandler("pictureInPictureAction", [
      "action": "setPlaying",
      "playing": playing
    ])
  }

  func pictureInPictureControllerTimeRangeForPlayback(
    _ pictureInPictureController: AVPictureInPictureController
  ) -> CMTimeRange {
    guard durationSeconds.isFinite, durationSeconds > 0 else {
      return CMTimeRange(start: .zero, duration: .positiveInfinity)
    }
    return CMTimeRange(
      start: .zero,
      duration: CMTime(seconds: durationSeconds, preferredTimescale: 600)
    )
  }

  func pictureInPictureControllerIsPlaybackPaused(
    _ pictureInPictureController: AVPictureInPictureController
  ) -> Bool {
    stateLock.lock()
    defer { stateLock.unlock() }
    return paused
  }

  func pictureInPictureController(
    _ pictureInPictureController: AVPictureInPictureController,
    didTransitionToRenderSize newRenderSize: CMVideoDimensions
  ) {
    // AVKit owns the PiP window geometry. The input frames keep their source aspect ratio.
  }

  func pictureInPictureController(
    _ pictureInPictureController: AVPictureInPictureController,
    skipByInterval skipInterval: CMTime,
    completion completionHandler: @escaping @Sendable () -> Void
  ) {
    DispatchQueue.main.async { [weak self] in
      guard let self else {
        completionHandler()
        return
      }
      // AVKit can issue another seek before the previous one is acknowledged.
      // Finish the previous request rather than leaving the system controls stuck.
      self.pendingSkipCompletion?()
      self.pendingSkipCompletion = completionHandler
      self.eventHandler("pictureInPictureAction", [
        "action": "seekBy",
        "seconds": skipInterval.seconds
      ])
    }
  }

  func pictureInPictureControllerShouldProhibitBackgroundAudioPlayback(
    _ pictureInPictureController: AVPictureInPictureController
  ) -> Bool {
    false
  }

  func pictureInPictureControllerWillStartPictureInPicture(
    _ pictureInPictureController: AVPictureInPictureController
  ) {
    diagnose("AVKit delegate willStart")
  }

  func pictureInPictureControllerDidStartPictureInPicture(
    _ pictureInPictureController: AVPictureInPictureController
  ) {
    diagnose("AVKit delegate didStart")
    let completion = pendingStart
    pendingStart = nil
    startIssued = false
    startTimeout?.cancel()
    startTimeout = nil
    startReadinessRetry?.cancel()
    startReadinessRetry = nil
    eventHandler("pictureInPictureChanged", ["active": true])
    completion?(nil)
  }

  func pictureInPictureControllerDidStopPictureInPicture(
    _ pictureInPictureController: AVPictureInPictureController
  ) {
    stateLock.lock()
    enabled = false
    stateLock.unlock()
    eventHandler("pictureInPictureChanged", ["active": false])
  }

  func pictureInPictureController(
    _ pictureInPictureController: AVPictureInPictureController,
    failedToStartPictureInPictureWithError error: Error
  ) {
    diagnose("AVKit delegate failedToStart error=\(error.localizedDescription)")
    let completion = pendingStart
    pendingStart = nil
    startIssued = false
    startTimeout?.cancel()
    startTimeout = nil
    startReadinessRetry?.cancel()
    startReadinessRetry = nil
    stateLock.lock()
    enabled = false
    stateLock.unlock()
    if let completion {
      completion(error)
    } else {
      eventHandler("mpvPictureInPictureError", error.localizedDescription)
    }
  }

  func pictureInPictureController(
    _ pictureInPictureController: AVPictureInPictureController,
    restoreUserInterfaceForPictureInPictureStopWithCompletionHandler completionHandler: @escaping (Bool) -> Void
  ) {
    completionHandler(true)
  }
}