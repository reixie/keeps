// In-process macOS adapter. Every exported call and all AppKit/AVPlayer state
// belong to the main thread. AVFoundation decodes asynchronously; no Go pointer
// is retained. A frame is copied synchronously into caller-owned RGBA storage.
import AppKit
import AVFoundation
import CoreImage
import QuickLookUI
import ImageIO

// ImageIO composites GIF disposal/transparency. The main thread owns the clock
// and published frame; a bounded queue decodes one pending frame per entry.
private final class InlineGIF: @unchecked Sendable {
    private static let decoding: OperationQueue = {
        let queue = OperationQueue()
        queue.maxConcurrentOperationCount = 4
        queue.qualityOfService = .userInitiated
        return queue
    }()
    private var source: CGImageSource?
    private var deadlines: [TimeInterval] = []
    private var started: CFTimeInterval?
    private var work: BlockOperation?
    var image: CGImage?
    var stopped = false
    var still = false
    var failed = false
    var frames = 0
    var delivered = 0
    var index = 0

    init(path: String) {
        let operation = BlockOperation()
        work = operation
        operation.addExecutionBlock { [weak self, weak operation] in
            guard operation?.isCancelled == false else { return }
            let source = CGImageSourceCreateWithURL(URL(fileURLWithPath: path) as CFURL, [kCGImageSourceShouldCache: false] as CFDictionary)
            var deadlines: [TimeInterval] = []
            var total: TimeInterval = 0
            if let source, CGImageSourceGetCount(source) > 1 {
                for frame in 0..<CGImageSourceGetCount(source) {
                    guard operation?.isCancelled == false else { return }
                    let properties = CGImageSourceCopyPropertiesAtIndex(source, frame, nil) as? [CFString: Any]
                    let gif = properties?[kCGImagePropertyGIFDictionary] as? [CFString: Any]
                    let delay = (gif?[kCGImagePropertyGIFUnclampedDelayTime] ?? gif?[kCGImagePropertyGIFDelayTime]) as? Double ?? 0.1
                    total += delay.isFinite && delay > 0 ? delay : 0.1
                    deadlines.append(total)
                }
            }
            DispatchQueue.main.async { [weak self] in
                guard let self, !self.stopped else { return }
                self.work = nil
                guard let source, total.isFinite else { self.failed = true; return }
                guard !deadlines.isEmpty else { self.still = true; return }
                self.source = source
                self.deadlines = deadlines
                self.update()
            }
        }
        Self.decoding.addOperation(operation)
    }

    private func target(at time: CFTimeInterval) -> Int {
        guard let started, let duration = deadlines.last else { return 0 }
        let elapsed = max(0, time - started)
        let position = elapsed.truncatingRemainder(dividingBy: duration)
        return deadlines.firstIndex(where: { position < $0 }) ?? 0
    }

    func update() {
        guard !stopped, work == nil, let source else { return }
        let wanted = target(at: CACurrentMediaTime())
        guard image == nil || index != wanted else { return }
        let operation = BlockOperation()
        work = operation
        operation.addExecutionBlock { [weak self, weak operation] in
            guard operation?.isCancelled == false else { return }
            let image = CGImageSourceCreateImageAtIndex(source, wanted, [kCGImageSourceShouldCacheImmediately: true] as CFDictionary)
            CGImageSourceRemoveCacheAtIndex(source, wanted)
            DispatchQueue.main.async { [weak self] in
                guard let self, !self.stopped else { return }
                self.work = nil
                guard let image else { self.failed = true; return }
                let now = CACurrentMediaTime()
                // A slow decode must not display an expired frame or restart the
                // clock. Cancellation also prevents stale IDs from reappearing.
                if self.started != nil && self.target(at: now) != wanted { return }
                self.index = wanted
                self.image = image
                self.frames += 1
            }
        }
        Self.decoding.addOperation(operation)
    }

    func presented() {
        if started == nil { started = CACurrentMediaTime() }
        delivered = frames
    }

    func stop() {
        stopped = true
        work?.cancel()
        work = nil
        source = nil
        image = nil
    }
}

private final class InlinePlayer: @unchecked Sendable {
    let player = AVPlayer()
    let output = AVPlayerItemVideoOutput(pixelBufferAttributes: [
        kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA,
    ])
    var task: Task<Void, Never>?
    var endObserver: NSObjectProtocol?
    var transform = CGAffineTransform.identity
    var stopped = false
    var failed = false

    init(path: String) {
        // Set these before an item or a playback request exists. The composition
        // also contains ONLY video, so future preview audio cannot leak here.
        player.isMuted = true
        player.volume = 0
        player.allowsExternalPlayback = false
        player.preventsDisplaySleepDuringVideoPlayback = false
        task = Task { @MainActor [weak self] in
            do {
                let asset = AVURLAsset(url: URL(fileURLWithPath: path))
                guard let track = try await asset.loadTracks(withMediaType: .video).first else { throw PlaybackError.noVideo }
                let range = try await track.load(.timeRange)
                let transform = try await track.load(.preferredTransform)
                try Task.checkCancellation()
                guard let self, !self.stopped else { return }
                let composition = AVMutableComposition()
                guard let video = composition.addMutableTrack(withMediaType: .video, preferredTrackID: kCMPersistentTrackID_Invalid) else { throw PlaybackError.noVideo }
                try video.insertTimeRange(range, of: track, at: .zero)
                // Output buffers are in track coordinates. Core Image applies
                // this transform once, including translation and mirroring.
                self.transform = transform
                let item = AVPlayerItem(asset: composition)
                item.add(self.output)
                self.player.replaceCurrentItem(with: item)
                self.endObserver = NotificationCenter.default.addObserver(forName: .AVPlayerItemDidPlayToEndTime, object: item, queue: .main) { [weak self, weak item] _ in
                    guard let self, let item, !self.stopped, self.player.currentItem === item else { return }
                    self.player.seek(to: .zero, toleranceBefore: .zero, toleranceAfter: .zero) { [weak self, weak item] finished in
                        // Seek completion can arrive after eviction or suspension.
                        DispatchQueue.main.async {
                            guard let self, let item, finished, !self.stopped, self.player.currentItem === item else { return }
                            self.playMuted()
                        }
                    }
                }
                self.playMuted()
            } catch is CancellationError {
                // Evicted ID: never publish the completed asset into a new slot.
            } catch {
                guard let self, !self.stopped else { return }
                self.failed = true
            }
        }
    }

    func playMuted() {
        player.isMuted = true
        player.volume = 0
        player.playImmediately(atRate: 1)
    }

    func stop() {
        stopped = true
        task?.cancel()
        task = nil
        player.pause()
        if let observer = endObserver { NotificationCenter.default.removeObserver(observer) }
        endObserver = nil
        if let item = player.currentItem { item.remove(output) }
        player.replaceCurrentItem(with: nil)
    }
}

private enum PlaybackError: Error { case noVideo }

private struct PreviewRecord: Decodable, Equatable {
    let id: String
    let path: String
    let title: String
}

private final class PreviewItem: NSObject, QLPreviewItem {
    let record: PreviewRecord
    init(_ record: PreviewRecord) { self.record = record }
    var previewItemURL: URL? { URL(fileURLWithPath: record.path) }
    var previewItemTitle: String? { record.title }
}

private final class PlaybackSession: NSWindowController, QLPreviewPanelDataSource, QLPreviewPanelDelegate {
    weak var savedResponder: NSResponder?
    weak var previousWindowController: NSWindowController?
    var players: [String: InlinePlayer] = [:]
    var gifs: [String: InlineGIF] = [:]
    let context = CIContext(options: [.cacheIntermediates: false])
    let colorSpace = CGColorSpace(name: CGColorSpace.sRGB)!
    var observers: [NSObjectProtocol] = []
    var workspaceObserver: NSObjectProtocol?
    var sequence: [PreviewItem] = []
    var previewWanted = false
    var previewSeen = false
    var closed = false
    var pendingPreviewTarget: String?
    var previewGeneration: UInt64 = 0

    init(window: NSWindow) {
        previousWindowController = window.windowController
        // Mygo owns the NSWindow and its delegate. It does not install a window
        // controller. AppKit puts this controller in the standard responder chain
        // and keeps it reachable while the Quick Look panel itself has focus.
        super.init(window: window)
        window.windowController = self
        let center = NotificationCenter.default
        for name in [NSApplication.didResignActiveNotification, NSApplication.didHideNotification,
                     NSWindow.didMiniaturizeNotification, NSWindow.didChangeOcclusionStateNotification] {
            observers.append(center.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                guard let self, !self.canPlay else { return }
                self.stopInline()
            })
        }
        observers.append(center.addObserver(forName: NSWindow.willCloseNotification, object: nil, queue: .main) { [weak self] note in
            guard let self, let panel = note.object as? QLPreviewPanel, panel.currentController as AnyObject? === self else { return }
            self.finishPreview()
        })
        observers.append(center.addObserver(forName: NSApplication.didBecomeActiveNotification, object: NSApp, queue: .main) { [weak self] _ in
            self?.presentPendingPreview()
        })
        workspaceObserver = NSWorkspace.shared.notificationCenter.addObserver(forName: NSWorkspace.accessibilityDisplayOptionsDidChangeNotification, object: nil, queue: .main) { [weak self] _ in
            if NSWorkspace.shared.accessibilityDisplayShouldReduceMotion { self?.stopInline() }
        }
    }

    required init?(coder: NSCoder) { fatalError("not serialized") }

    var canPlay: Bool {
        guard !closed, !previewWanted, let window else { return false }
        return NSApp.isActive && !NSApp.isHidden && window.isVisible && !window.isMiniaturized &&
            window.occlusionState.contains(.visible) && !NSWorkspace.shared.accessibilityDisplayShouldReduceMotion
    }

    func stopInline() {
        for entry in players.values { entry.stop() }
        players.removeAll()
        for entry in gifs.values { entry.stop() }
        gifs.removeAll()
    }

    override func acceptsPreviewPanelControl(_ panel: QLPreviewPanel!) -> Bool { !closed && previewWanted }

    override func beginPreviewPanelControl(_ panel: QLPreviewPanel!) {
        panel.dataSource = self
        panel.delegate = self
    }

    override func endPreviewPanelControl(_ panel: QLPreviewPanel!) {
        panel.dataSource = nil
        panel.delegate = nil
        // Responder-chain control can be relinquished during native focus or
        // fullscreen transitions. Visibility, rather than this callback, owns
        // the preview session's lifetime and the inline suspension barrier.
    }

    func numberOfPreviewItems(in panel: QLPreviewPanel!) -> Int { sequence.count }
    func previewPanel(_ panel: QLPreviewPanel!, previewItemAt index: Int) -> QLPreviewItem! {
        sequence.indices.contains(index) ? sequence[index] : nil
    }

    // Deliberately do not forward ANY key event to Mygo. Quick Look owns Space,
    // seeking, text commands, navigation and native fullscreen/Escape behavior.
    func previewPanel(_ panel: QLPreviewPanel!, handle event: NSEvent!) -> Bool { false }

    var currentID: String {
        guard previewWanted, let panel = QLPreviewPanel.shared(), panel.currentController as AnyObject? === self,
              sequence.indices.contains(panel.currentPreviewItemIndex) else { return "" }
        return sequence[panel.currentPreviewItemIndex].record.id
    }

    func setSequence(_ records: [PreviewRecord], target: String) {
        guard !closed else { return }
        let opening = !target.isEmpty
        guard opening || previewWanted else { return }
        let oldID = currentID
        let panel = QLPreviewPanel.shared()!
        let oldIndex = max(0, panel.currentPreviewItemIndex == NSNotFound ? 0 : panel.currentPreviewItemIndex)
        if !opening && records == sequence.map({ $0.record }) { return }
        if records.isEmpty { closePreview(); return }
        sequence = records.map(PreviewItem.init)
        if opening {
            previewGeneration &+= 1
            savedResponder = window?.firstResponder
            previewWanted = true
            previewSeen = false
            stopInline()
            pendingPreviewTarget = target
            NSApp.activate(ignoringOtherApps: true)
            // AX activation can arrive while Keeps is inactive. App activation
            // is asynchronous: present only after AppKit has a main window.
            DispatchQueue.main.async { [weak self] in self?.presentPendingPreview() }
            return
        }
        guard panel.currentController as AnyObject? === self else { return }
        let index = sequence.firstIndex(where: { $0.record.id == oldID }) ?? min(oldIndex, sequence.count - 1)
        let title = sequence[index].record.title
        panel.reloadData()
        panel.currentPreviewItemIndex = index
        panel.title = title
    }

    func presentPendingPreview() {
        guard !closed, previewWanted, NSApp.isActive, let target = pendingPreviewTarget, !sequence.isEmpty else { return }
        pendingPreviewTarget = nil
        let generation = previewGeneration
        window?.makeKeyAndOrderFront(nil)
        window?.makeMain()
        let panel = QLPreviewPanel.shared()!
        panel.makeKeyAndOrderFront(nil)
        panel.updateController()
        guard previewWanted, generation == previewGeneration, !sequence.isEmpty else { return }
        guard panel.currentController as AnyObject? === self else {
            finishPreview()
            return
        }
        let index = sequence.firstIndex(where: { $0.record.id == target }) ?? 0
        let title = sequence[index].record.title
        panel.reloadData()
        guard previewWanted, generation == previewGeneration else { return }
        panel.currentPreviewItemIndex = index
        panel.title = title
    }

    func finishPreview() {
        guard previewWanted else { return }
        previewWanted = false
        previewGeneration &+= 1
        previewSeen = false
        pendingPreviewTarget = nil
        sequence.removeAll()
        if !closed, let window, window.isVisible, !window.isMiniaturized, NSApp.isActive {
            window.makeKeyAndOrderFront(nil)
            window.makeFirstResponder(savedResponder)
        }
        savedResponder = nil
    }

    func closePreview() {
        guard previewWanted else { return }
        guard let panel = QLPreviewPanel.shared(), panel.currentController as AnyObject? === self else { finishPreview(); return }
        if panel.isInFullScreenMode { panel.exitFullScreenMode(options: nil) }
        panel.orderOut(nil)
        // Only the current controller changes panel state.
        panel.dataSource = nil
        panel.delegate = nil
        finishPreview()
    }

    func shutdown() {
        closed = true
        closePreview()
        stopInline()
        for observer in observers { NotificationCenter.default.removeObserver(observer) }
        observers.removeAll()
        if let observer = workspaceObserver { NSWorkspace.shared.notificationCenter.removeObserver(observer) }
        workspaceObserver = nil
        let mainWindow = window
        window = nil
        if mainWindow?.windowController === self { mainWindow?.windowController = previousWindowController }
    }
}

private func session(_ pointer: UnsafeRawPointer) -> PlaybackSession {
    precondition(Thread.isMainThread)
    return Unmanaged<PlaybackSession>.fromOpaque(pointer).takeUnretainedValue()
}

@_cdecl("KeepsPlaybackCreate")
public func playbackCreate(_ pointer: UnsafeRawPointer) -> UnsafeMutableRawPointer {
    precondition(Thread.isMainThread)
    let window = Unmanaged<NSWindow>.fromOpaque(pointer).takeUnretainedValue()
    return Unmanaged.passRetained(PlaybackSession(window: window)).toOpaque()
}

@_cdecl("KeepsPlaybackDestroy")
public func playbackDestroy(_ pointer: UnsafeRawPointer) {
    session(pointer).shutdown()
    Unmanaged<PlaybackSession>.fromOpaque(pointer).release()
}

@_cdecl("KeepsPlaybackState")
public func playbackState(_ pointer: UnsafeRawPointer) -> Int32 {
    let owner = session(pointer)
    // Closing Quick Look with Space can order it out without willClose.
    if owner.previewWanted, let panel = QLPreviewPanel.shared() {
        if panel.isVisible || panel.isInFullScreenMode { owner.previewSeen = true }
        else if owner.previewSeen { owner.finishPreview() }
        if panel.currentController as AnyObject? === owner, owner.sequence.indices.contains(panel.currentPreviewItemIndex) {
            panel.title = owner.sequence[panel.currentPreviewItemIndex].record.title
        }
    }
    if !owner.canPlay { owner.stopInline() }
    return (owner.canPlay ? 1 : 0) | (owner.previewWanted ? 2 : 0)
}

@_cdecl("KeepsPlaybackAdd")
public func playbackAdd(_ pointer: UnsafeRawPointer, _ id: UnsafePointer<CChar>, _ path: UnsafePointer<CChar>) {
    let owner = session(pointer), key = String(cString: id)
    guard owner.canPlay, owner.players[key] == nil, owner.gifs[key] == nil, owner.players.count + owner.gifs.count < 4 else { return }
    let file = String(cString: path)
    if URL(fileURLWithPath: file).pathExtension.lowercased() == "gif" { owner.gifs[key] = InlineGIF(path: file) }
    else { owner.players[key] = InlinePlayer(path: file) }
}

@_cdecl("KeepsPlaybackRemove")
public func playbackRemove(_ pointer: UnsafeRawPointer, _ id: UnsafePointer<CChar>) {
    let owner = session(pointer), key = String(cString: id)
    owner.players.removeValue(forKey: key)?.stop()
    owner.gifs.removeValue(forKey: key)?.stop()
}

@_cdecl("KeepsPlaybackFrame")
public func playbackFrame(_ pointer: UnsafeRawPointer, _ id: UnsafePointer<CChar>, _ pixels: UnsafeMutableRawPointer, _ width: Int32, _ height: Int32) -> Int32 {
    let owner = session(pointer)
    if let entry = owner.gifs[String(cString: id)] {
        guard owner.canPlay, !entry.stopped else { return 0 }
        if entry.still { return -2 }
        if entry.failed { return -1 }
        entry.update()
        guard entry.delivered != entry.frames, let image = entry.image else { return 0 }
        guard let context = CGContext(data: pixels, width: Int(width), height: Int(height), bitsPerComponent: 8, bytesPerRow: Int(width)*4,
                                      space: owner.colorSpace, bitmapInfo: CGBitmapInfo.byteOrder32Big.rawValue | CGImageAlphaInfo.premultipliedLast.rawValue) else { return -1 }
        context.interpolationQuality = .high
        context.setBlendMode(.copy)
        context.draw(image, in: CGRect(x: 0, y: 0, width: Int(width), height: Int(height)))
        entry.presented()
        return 1
    }
    guard owner.canPlay, let entry = owner.players[String(cString: id)], !entry.stopped else { return 0 }
    guard !entry.failed, entry.player.status != .failed, entry.player.currentItem?.status != .failed else { return -1 }
    let time = entry.output.itemTime(forHostTime: CACurrentMediaTime())
    guard entry.output.hasNewPixelBuffer(forItemTime: time), let buffer = entry.output.copyPixelBuffer(forItemTime: time, itemTimeForDisplay: nil) else { return 0 }
    return autoreleasepool {
        var image = CIImage(cvPixelBuffer: buffer)
        // Track transforms use top-left video coordinates. Conjugate through
        // vertical flips to apply them in Core Image's bottom-left coordinates.
        image = image.transformed(by: CGAffineTransform(a: 1, b: 0, c: 0, d: -1, tx: 0, ty: image.extent.height))
        image = image.transformed(by: entry.transform)
        image = image.transformed(by: CGAffineTransform(translationX: -image.extent.minX, y: -image.extent.minY))
        image = image.transformed(by: CGAffineTransform(a: 1, b: 0, c: 0, d: -1, tx: 0, ty: image.extent.height))
        image = image.transformed(by: CGAffineTransform(scaleX: CGFloat(width) / image.extent.width, y: CGFloat(height) / image.extent.height))
        // CIContext's bitmap export supplies the row order Mygo expects.
        owner.context.render(image, toBitmap: pixels, rowBytes: Int(width) * 4,
                             bounds: CGRect(x: 0, y: 0, width: Int(width), height: Int(height)), format: .RGBA8, colorSpace: owner.colorSpace)
        return 1
    }
}

@_cdecl("KeepsPreviewUpdate")
public func previewUpdate(_ pointer: UnsafeRawPointer, _ json: UnsafePointer<CChar>, _ target: UnsafePointer<CChar>) {
    let owner = session(pointer)
    guard let records = try? JSONDecoder().decode([PreviewRecord].self, from: Data(String(cString: json).utf8)) else { return }
    owner.setSequence(records, target: String(cString: target))
}

@_cdecl("KeepsPreviewCurrent")
public func previewCurrent(_ pointer: UnsafeRawPointer, _ output: UnsafeMutablePointer<UInt8>, _ capacity: Int32) -> Int32 {
    let data = Array(session(pointer).currentID.utf8)
    guard data.count < capacity else { return 0 }
    for (index, value) in data.enumerated() { output[index] = value }
    return Int32(data.count)
}
