import AppKit

private enum CaptureMenuResult: Int32 {
    case escape = 0, link, media
}

private final class CaptureMenuRow: NSButton {
    let result: CaptureMenuResult
    let shortcut: String
    var onHover: (() -> Void)?
    var selected = false {
        didSet { needsDisplay = true }
    }

    init(_ title: String, symbol: String, shortcut: String, result: CaptureMenuResult) {
        self.result = result
        self.shortcut = shortcut
        super.init(frame: NSRect(x: 0, y: 0, width: 208, height: 32))
        self.title = ""
        isBordered = false
        focusRingType = .none
        setAccessibilityLabel(title)
        let keyLabel = shortcut.uppercased()
        setAccessibilityHelp("Keyboard shortcut \(keyLabel)")

        let icon = NSImageView(frame: NSRect(x: 10, y: 8, width: 16, height: 16))
        icon.image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil)
        icon.contentTintColor = .secondaryLabelColor
        icon.setAccessibilityElement(false)
        addSubview(icon)

        let label = NSTextField(labelWithString: title)
        label.font = .systemFont(ofSize: 13, weight: .medium)
        label.frame = NSRect(x: 36, y: 7, width: 130, height: 18)
        label.setAccessibilityElement(false)
        addSubview(label)

        let key = NSTextField(labelWithString: keyLabel)
        key.font = .systemFont(ofSize: 12, weight: .semibold)
        key.textColor = .secondaryLabelColor
        key.alignment = .right
        key.frame = NSRect(x: 174, y: 8, width: 24, height: 16)
        key.setAccessibilityElement(false)
        addSubview(key)

        addTrackingArea(NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .activeInKeyWindow, .inVisibleRect], owner: self))
    }

    required init?(coder: NSCoder) { fatalError("Capture menu rows are created programmatically") }

    override var acceptsFirstResponder: Bool { true }

    override func hitTest(_ point: NSPoint) -> NSView? {
        super.hitTest(point) == nil ? nil : self
    }

    override func mouseEntered(with event: NSEvent) { onHover?() }

    override func draw(_ dirtyRect: NSRect) {
        if selected {
            NSColor.labelColor.withAlphaComponent(0.10).setFill()
            NSBezierPath(roundedRect: bounds, xRadius: 9, yRadius: 9).fill()
        }
        super.draw(dirtyRect)
    }
}

private final class CaptureMenu: NSObject, NSPopoverDelegate {
    private weak var window: NSWindow?
    private let popover = NSPopover()
    private let content = NSViewController()
    private let rows = [
        CaptureMenuRow("Link", symbol: "link", shortcut: "l", result: .link),
        CaptureMenuRow("Media", symbol: "photo", shortcut: "m", result: .media),
    ]
    private var selected = 0
    private var pending: CaptureMenuResult?
    private var keyMonitor: Any?
    private let onClose: (Int32) -> Void

    init(window: NSWindow, onClose: @escaping (Int32) -> Void) {
        self.window = window
        self.onClose = onClose
        super.init()

        content.view = NSView(frame: NSRect(x: 0, y: 0, width: 220, height: 104))
        let heading = NSTextField(labelWithString: "Create capture")
        heading.font = .systemFont(ofSize: 12, weight: .semibold)
        heading.textColor = .secondaryLabelColor
        heading.frame = NSRect(x: 16, y: 75, width: 188, height: 17)
        content.view.addSubview(heading)
        for (index, row) in rows.enumerated() {
            row.setFrameOrigin(NSPoint(x: 6, y: 39 - 32 * index))
            row.target = self
            row.action = #selector(choose(_:))
            row.onHover = { [weak self] in self?.highlight(index) }
            content.view.addSubview(row)
        }

        popover.contentViewController = content
        popover.contentSize = content.view.frame.size
        popover.behavior = .transient
        popover.delegate = self
    }

    func show(anchor: NSRect) {
        guard !popover.isShown, pending == nil, !anchor.isEmpty, let view = window?.contentView else { return }
        let bounds = view.bounds
        let y = view.isFlipped ? bounds.minY + anchor.minY : bounds.maxY - anchor.maxY
        let rect = NSRect(x: bounds.minX + anchor.minX, y: y, width: anchor.width, height: anchor.height)
        highlight(0)
        popover.animates = !NSWorkspace.shared.accessibilityDisplayShouldReduceMotion
        popover.show(relativeTo: rect, of: view, preferredEdge: view.isFlipped ? .maxY : .minY)
    }

    func close() {
        pending = nil
        if popover.isShown { popover.close() }
    }

    func destroy() {
        popover.delegate = nil
        stopMonitoringKeys()
        close()
    }

    func popoverDidShow(_ notification: Notification) {
        guard let popoverWindow = content.view.window else { return }
        popoverWindow.makeKey()
        popoverWindow.makeFirstResponder(rows[selected])
        keyMonitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self, weak popoverWindow] event in
            guard let self, let popoverWindow, event.window === popoverWindow else { return event }
            return self.handleKey(event) ? nil : event
        }
    }

    func popoverDidClose(_ notification: Notification) {
        stopMonitoringKeys()
        let result = pending
        pending = nil
        if let result { onClose(result.rawValue) }
    }

    private func stopMonitoringKeys() {
        if let keyMonitor { NSEvent.removeMonitor(keyMonitor) }
        keyMonitor = nil
    }

    private func highlight(_ index: Int) {
        rows[selected].selected = false
        selected = index
        rows[selected].selected = true
    }

    @objc private func choose(_ row: CaptureMenuRow) { finish(row.result) }

    private func finish(_ result: CaptureMenuResult) {
        guard popover.isShown, pending == nil else { return }
        pending = result
        popover.performClose(nil)
    }

    private func handleKey(_ event: NSEvent) -> Bool {
        guard event.modifierFlags.intersection([.command, .option, .control]).isEmpty,
              let key = event.charactersIgnoringModifiers?.lowercased() else { return false }
        switch key {
        case "\u{f700}", "\u{19}": moveSelection(by: -1) // Up, Shift-Tab
        case "\u{f701}", "\t": moveSelection(by: 1) // Down, Tab
        case "\r", "\u{3}", " ": finish(rows[selected].result)
        case "\u{1b}": finish(.escape)
        default:
            guard let row = rows.first(where: { $0.shortcut == key }) else { return false }
            finish(row.result)
        }
        return true
    }

    private func moveSelection(by offset: Int) {
        highlight((selected + offset + rows.count) % rows.count)
        content.view.window?.makeFirstResponder(rows[selected])
    }
}

private func captureMenu(_ pointer: UnsafeRawPointer) -> CaptureMenu {
    precondition(Thread.isMainThread)
    return Unmanaged<CaptureMenu>.fromOpaque(pointer).takeUnretainedValue()
}

@_cdecl("KeepsCaptureMenuCreate")
public func captureMenuCreate(_ pointer: UnsafeRawPointer, _ onClose: @escaping @convention(c) (Int32) -> Void) -> UnsafeMutableRawPointer {
    precondition(Thread.isMainThread)
    let window = Unmanaged<NSWindow>.fromOpaque(pointer).takeUnretainedValue()
    return Unmanaged.passRetained(CaptureMenu(window: window, onClose: onClose)).toOpaque()
}

@_cdecl("KeepsCaptureMenuShow")
public func captureMenuShow(_ pointer: UnsafeRawPointer, _ x: Double, _ y: Double, _ width: Double, _ height: Double) {
    captureMenu(pointer).show(anchor: NSRect(x: x, y: y, width: width, height: height))
}

@_cdecl("KeepsCaptureMenuClose")
public func captureMenuClose(_ pointer: UnsafeRawPointer) {
    captureMenu(pointer).close()
}

@_cdecl("KeepsCaptureMenuDestroy")
public func captureMenuDestroy(_ pointer: UnsafeRawPointer) {
    captureMenu(pointer).destroy()
    Unmanaged<CaptureMenu>.fromOpaque(pointer).release()
}
