import AppKit

// These values match the Go adapter. Swift owns AppKit views and Go owns
// application state. Text is copied by Go during the callback.
enum LibraryControlAction: Int32 {
    case selectTag = 1, createTag, createSpace, appearance, translucency
    case settingsSpace, revealSpace, checkUpdates, openRelease
    case renameTag, colorTag, deleteTag, submitTagEditor, cancelTagEditor, editSpace, toggleSpace
}

typealias LibraryControlHandler = (LibraryControlAction, String) -> Void

struct ControlIcons {
    let directory: URL

    func image(_ name: String) -> NSImage? {
        templateImage(directory.appendingPathComponent(name + ".png"))
    }

    func spaceImage(_ name: String) -> NSImage? {
        let resources = directory.deletingLastPathComponent()
        return templateImage(resources.appendingPathComponent("collections/reicon").appendingPathComponent(name + ".svg"))
    }

    private func templateImage(_ url: URL) -> NSImage? {
        let image = NSImage(contentsOf: url)
        image?.size = NSSize(width: 16, height: 16)
        image?.isTemplate = true
        return image
    }
}

private enum LibraryControlPanel: Int32 {
    case tags = 1, settings, newSpace, tagEditor
}

private final class LibraryControls {
    weak var parent: NSWindow?
    let icons: ControlIcons
    let send: LibraryControlHandler
    var tags: TagsPopover?
    var settings: KeepsSettings?
    var newSpace: NewSpacePanel?
    var tagEditor: TagEditor?

    init(parent: NSWindow, resources: String, send: @escaping LibraryControlHandler) {
        self.parent = parent
        self.icons = ControlIcons(directory: URL(fileURLWithPath: resources).appendingPathComponent("reicon-controls"))
        self.send = send
    }

    func show(_ panel: LibraryControlPanel, data: Data, anchor: NSRect) {
        guard let parent else { return }
        switch panel {
        case .tags:
            if tags == nil { tags = TagsPopover(icons: icons, send: send) }
            tags?.update(data)
            if let view = parent.contentView { tags?.show(in: view, anchor: anchor) }
        case .settings:
            if settings == nil { settings = KeepsSettings(icons: icons, send: send) }
            settings?.update(data)
            settings?.showWindow(nil)
            settings?.window?.makeKeyAndOrderFront(nil)
        case .newSpace:
            tags?.close()
            if newSpace == nil { newSpace = NewSpacePanel(icons: icons, send: send) }
            newSpace?.show(data)
        case .tagEditor:
            tags?.close()
            if tagEditor == nil { tagEditor = TagEditor(send: send) }
            tagEditor?.show(parent: parent, data: data)
        }
    }

    func update(_ panel: LibraryControlPanel, data: Data) {
        switch panel {
        case .tags: tags?.update(data)
        case .settings: settings?.update(data)
        case .newSpace: break
        case .tagEditor: tagEditor?.update(data)
        }
    }

    func close(_ panel: LibraryControlPanel) {
        switch panel {
        case .tags: tags?.close()
        case .settings: settings?.close()
        case .newSpace: newSpace?.close()
        case .tagEditor: tagEditor?.close()
        }
    }
}

private final class NewSpacePanel: NSWindowController {
    private struct State: Decodable {
        let icons: [String]
        let icon: String
    }

    private let name = NSTextField(string: "")
    private let path = NSTextField(labelWithString: "Default location")
    private let symbol: SpaceSymbolPicker
    private let createButton: NSButton
    private var folder: URL?
    private var chooser: NSOpenPanel?
    private let send: LibraryControlHandler

    init(icons: ControlIcons, send: @escaping LibraryControlHandler) {
        self.send = send
        symbol = SpaceSymbolPicker(icons: icons)
        createButton = NSButton(title: "Create", target: nil, action: nil)
        let panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 380, height: 278),
                            styleMask: [.titled, .closable], backing: .buffered, defer: false)
        panel.title = "New Space"
        panel.isReleasedWhenClosed = false
        panel.autorecalculatesKeyViewLoop = true
        super.init(window: panel)

        let form = panel.contentView!
        let nameLabel = NSTextField(labelWithString: "Name")
        nameLabel.frame = NSRect(x: 20, y: 241, width: 340, height: 18)
        name.frame = NSRect(x: 20, y: 212, width: 340, height: 24)
        name.placeholderString = "Space name"
        name.target = self
        name.action = #selector(createSpace)
        let iconLabel = NSTextField(labelWithString: "Space icon")
        iconLabel.frame = NSRect(x: 20, y: 175, width: 112, height: 18)
        let storageLabel = NSTextField(labelWithString: "Storage folder")
        storageLabel.frame = NSRect(x: 20, y: 86, width: 340, height: 18)
        path.frame = NSRect(x: 20, y: 57, width: 216, height: 22)
        path.textColor = .secondaryLabelColor
        path.lineBreakMode = .byTruncatingMiddle
        let choose = NSButton(title: "Choose…", target: self, action: #selector(chooseFolder))
        choose.image = icons.image("collections")
        choose.imagePosition = .imageLeading
        choose.frame = NSRect(x: 250, y: 54, width: 110, height: 27)
        let cancel = NSButton(title: "Cancel", target: self, action: #selector(cancelSpace))
        cancel.keyEquivalent = "\u{1b}"
        cancel.frame = NSRect(x: 186, y: 14, width: 84, height: 28)
        createButton.target = self
        createButton.action = #selector(createSpace)
        createButton.keyEquivalent = "\r"
        createButton.frame = NSRect(x: 274, y: 14, width: 86, height: 28)
        [nameLabel, name, iconLabel, storageLabel, path, choose, cancel, createButton].forEach { form.addSubview($0) }
        symbol.translatesAutoresizingMaskIntoConstraints = false
        form.addSubview(symbol)
        NSLayoutConstraint.activate([
            symbol.leadingAnchor.constraint(greaterThanOrEqualTo: iconLabel.trailingAnchor),
            symbol.trailingAnchor.constraint(equalTo: form.trailingAnchor, constant: -20),
            symbol.topAnchor.constraint(equalTo: form.topAnchor, constant: 82),
        ])
    }

    required init?(coder: NSCoder) { fatalError("New Space is created programmatically") }

    func show(_ data: Data) {
        guard let window, let state = try? JSONDecoder().decode(State.self, from: data) else { return }
        if !window.isVisible {
            name.stringValue = ""
            folder = nil
            path.stringValue = "Default location"
            path.toolTip = nil
            symbol.setIcons(state.icons)
            symbol.setChoice(icon: state.icon, emoji: "")
            window.center()
        }
        window.makeKeyAndOrderFront(nil)
        window.makeFirstResponder(name)
    }

    @objc private func chooseFolder() {
        guard let window, chooser == nil else { return }
        let open = NSOpenPanel()
        open.message = "Choose an empty folder for this Space."
        open.canChooseFiles = false
        open.canChooseDirectories = true
        open.canCreateDirectories = true
        open.allowsMultipleSelection = false
        chooser = open
        open.beginSheetModal(for: window) { [weak self, weak open] response in
            guard let self else { return }
            self.chooser = nil
            guard response == .OK, let folder = open?.url else { return }
            self.folder = folder
            self.path.stringValue = folder.path
            self.path.toolTip = folder.path
        }
    }

    @objc private func createSpace() {
        let title = name.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !title.isEmpty else {
            NSSound.beep()
            window?.makeFirstResponder(name)
            return
        }
        guard let choice = symbol.choice() else { return }
        struct Request: Encodable { let name: String; let root: String; let icon: String; let emoji: String }
        let data = try! JSONEncoder().encode(Request(name: title, root: folder?.path ?? "", icon: choice.icon, emoji: choice.emoji))
        close()
        send(.createSpace, String(decoding: data, as: UTF8.self))
    }

    @objc private func cancelSpace() { close() }

    override func close() {
        chooser?.cancel(nil)
        super.close()
    }
}

private func libraryControls(_ pointer: UnsafeRawPointer) -> LibraryControls {
    precondition(Thread.isMainThread)
    return Unmanaged<LibraryControls>.fromOpaque(pointer).takeUnretainedValue()
}

@_cdecl("KeepsControlsCreate")
public func controlsCreate(_ window: UnsafeRawPointer, _ resources: UnsafePointer<CChar>,
                           _ callback: @escaping @convention(c) (Int32, UnsafePointer<CChar>, Int32) -> Void) -> UnsafeMutableRawPointer {
    precondition(Thread.isMainThread)
    let parent = Unmanaged<NSWindow>.fromOpaque(window).takeUnretainedValue()
    let controls = LibraryControls(parent: parent, resources: String(cString: resources)) { action, text in
        text.utf8CString.withUnsafeBufferPointer { value in
            callback(action.rawValue, value.baseAddress!, Int32(value.count - 1))
        }
    }
    return Unmanaged.passRetained(controls).toOpaque()
}

@_cdecl("KeepsControlsShow")
public func controlsShow(_ pointer: UnsafeRawPointer, _ panel: Int32, _ json: UnsafePointer<CChar>,
                         _ x: Double, _ y: Double, _ width: Double, _ height: Double) {
    guard let panel = LibraryControlPanel(rawValue: panel) else { return }
    libraryControls(pointer).show(panel, data: Data(String(cString: json).utf8),
                                  anchor: NSRect(x: x, y: y, width: width, height: height))
}

@_cdecl("KeepsControlsRefresh")
public func controlsRefresh(_ pointer: UnsafeRawPointer, _ panel: Int32, _ json: UnsafePointer<CChar>) {
    guard let panel = LibraryControlPanel(rawValue: panel) else { return }
    libraryControls(pointer).update(panel, data: Data(String(cString: json).utf8))
}

@_cdecl("KeepsControlsClose")
public func controlsClose(_ pointer: UnsafeRawPointer, _ panel: Int32) {
    guard let panel = LibraryControlPanel(rawValue: panel) else { return }
    libraryControls(pointer).close(panel)
}

@_cdecl("KeepsControlsDestroy")
public func controlsDestroy(_ pointer: UnsafeRawPointer) {
    let controls = libraryControls(pointer)
    controls.close(.tags)
    controls.close(.tagEditor)
    controls.close(.newSpace)
    controls.close(.settings)
    Unmanaged<LibraryControls>.fromOpaque(pointer).release()
}
