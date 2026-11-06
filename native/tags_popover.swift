import AppKit

final class TagsPopover: NSObject, NSPopoverDelegate, NSSearchFieldDelegate, NSTableViewDataSource, NSTableViewDelegate {
    private struct Tag: Decodable {
        let id: String
        let name: String
        let color: String?
    }

    private enum Row {
        case tag(Tag)
        case create(String)

        var label: String {
            switch self {
            case .tag(let tag): return tag.name
            case .create(let name): return "Create tag “\(name)”"
            }
        }

        var icon: String {
            switch self {
            case .tag: return "tags"
            case .create: return "plus"
            }
        }
    }

    private let popover = NSPopover()
    private let search = NSSearchField(frame: NSRect(x: 13, y: 111, width: 252, height: 26))
    private let table = TagTableView()
    private let scroll = NSScrollView(frame: NSRect(x: 7, y: 8, width: 266, height: 94))
    private let empty = NSTextField(labelWithString: "No tags")
    private var tags: [Tag] = []
    private var rows: [Row] = []
    private let icons: ControlIcons
    private let send: LibraryControlHandler
    private weak var anchorWindow: NSWindow?

    init(icons: ControlIcons, send: @escaping LibraryControlHandler) {
        self.icons = icons
        self.send = send
        super.init()

        let content = NSViewController()
        content.view = NSView(frame: NSRect(x: 0, y: 0, width: 280, height: 146))
        search.placeholderString = "Search tags…"
        search.setAccessibilityLabel("Search tags")
        search.delegate = self
        search.font = .systemFont(ofSize: 13)
        (search.cell as? NSSearchFieldCell)?.searchButtonCell?.image = icons.image("search")
        let separator = NSBox(frame: NSRect(x: 0, y: 105, width: 280, height: 1))
        separator.boxType = .separator
        let column = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("tag"))
        column.width = scroll.frame.width
        table.addTableColumn(column)
        table.headerView = nil
        table.backgroundColor = .clear
        table.rowHeight = 28
        table.style = .inset
        table.delegate = self
        table.dataSource = self
        table.target = self
        table.action = #selector(clickedTag)
        table.actionMenu = { [weak self] row in self?.menu(for: row) }
        table.chooseRow = { [weak self] row in self?.choose(max(0, row)) }
        table.cancel = { [weak self] in self?.close() }
        table.setAccessibilityLabel("Tags")
        table.frame = NSRect(origin: .zero, size: scroll.contentSize)
        table.autoresizingMask = [.width]
        scroll.documentView = table
        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        empty.frame = NSRect(x: 16, y: 76, width: 248, height: 18)
        empty.textColor = .secondaryLabelColor
        empty.font = .systemFont(ofSize: 13, weight: .medium)
        [search, separator, scroll, empty].forEach { content.view.addSubview($0) }
        popover.contentViewController = content
        popover.contentSize = content.view.frame.size
        popover.behavior = .transient
        popover.delegate = self
    }

    func update(_ data: Data) {
        tags = (try? JSONDecoder().decode([Tag].self, from: data)) ?? []
        reload()
    }

    func show(in view: NSView, anchor: NSRect) {
        guard !popover.isShown, !anchor.isEmpty else { return }
        anchorWindow = view.window
        search.stringValue = ""
        reload()
        let bounds = view.bounds
        let y = view.isFlipped ? bounds.minY + anchor.minY : bounds.maxY - anchor.maxY
        let rect = NSRect(x: bounds.minX + anchor.minX, y: y, width: anchor.width, height: anchor.height)
        popover.animates = !NSWorkspace.shared.accessibilityDisplayShouldReduceMotion
        popover.show(relativeTo: rect, of: view, preferredEdge: .maxX)
    }

    func close() {
        if popover.isShown { popover.close() }
    }

    func popoverDidShow(_ notification: Notification) {
        guard let window = search.window else { return }
        window.makeKey()
        window.recalculateKeyViewLoop()
        window.makeFirstResponder(search)
    }

    func popoverDidClose(_ notification: Notification) {
        if let anchorWindow, anchorWindow.isVisible, NSApp.keyWindow == nil || NSApp.keyWindow === search.window {
            anchorWindow.makeKey()
        }
    }

    func controlTextDidChange(_ notification: Notification) { reload() }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy commandSelector: Selector) -> Bool {
        switch commandSelector {
        case #selector(NSResponder.moveDown(_:)):
            move(1)
        case #selector(NSResponder.moveUp(_:)):
            move(-1)
        case #selector(NSResponder.insertNewline(_:)):
            if !rows.isEmpty { choose(max(0, table.selectedRow)) }
        case #selector(NSResponder.cancelOperation(_:)):
            close()
        default:
            return false
        }
        return true
    }

    private func reload() {
        let query = search.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        rows = tags.filter { query.isEmpty || $0.name.range(of: query, options: .caseInsensitive) != nil }.map(Row.tag)
        if !query.isEmpty && !tags.contains(where: { $0.name.compare(query, options: .caseInsensitive) == .orderedSame }) {
            rows.append(.create(query))
        }
        table.reloadData()
        table.deselectAll(nil)
        scroll.isHidden = rows.isEmpty
        empty.isHidden = !rows.isEmpty
    }

    private func move(_ delta: Int) {
        guard !rows.isEmpty else { return }
        let index = min(rows.count - 1, max(0, table.selectedRow + delta))
        table.selectRowIndexes(IndexSet(integer: index), byExtendingSelection: false)
        table.scrollRowToVisible(index)
    }

    @objc private func clickedTag() {
        if table.clickedRow >= 0 { choose(table.clickedRow) }
    }

    private func choose(_ index: Int) {
        guard rows.indices.contains(index) else { return }
        switch rows[index] {
        case .tag(let tag):
            close()
            send(.selectTag, tag.id)
        case .create(let name):
            send(.createTag, name)
        }
    }

    private func menu(for index: Int) -> NSMenu? {
        guard rows.indices.contains(index), case .tag(let tag) = rows[index] else { return nil }
        let menu = NSMenu()
        menu.autoenablesItems = false
        addAction("Rename Tag…", .renameTag, tag.id, to: menu)
        let colors = NSMenu()
        colors.autoenablesItems = false
        for (value, name, color) in tagColors {
            let item = addAction(name, .colorTag, "\(tag.id):\(value)", to: colors)
            item.state = (tag.color ?? "") == value ? .on : .off
            let dot = NSImage(size: NSSize(width: 12, height: 12), flipped: false) { rect in
                color.setFill()
                NSBezierPath(ovalIn: rect.insetBy(dx: 2, dy: 2)).fill()
                return true
            }
            item.image = dot
        }
        let palette = NSMenuItem(title: "Color", action: nil, keyEquivalent: "")
        palette.submenu = colors
        menu.addItem(palette)
        menu.addItem(.separator())
        addAction("Delete Tag…", .deleteTag, tag.id, to: menu)
        return menu
    }

    @discardableResult
    private func addAction(_ title: String, _ action: LibraryControlAction, _ value: String, to menu: NSMenu) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: #selector(menuAction(_:)), keyEquivalent: "")
        item.target = self
        item.tag = Int(action.rawValue)
        item.representedObject = value
        menu.addItem(item)
        return item
    }

    @objc private func menuAction(_ item: NSMenuItem) {
        guard let action = LibraryControlAction(rawValue: Int32(item.tag)), let value = item.representedObject as? String else { return }
        send(action, value)
    }

    @objc private func openActions(_ sender: NSButton) {
        let index = table.row(for: sender)
        menu(for: index)?.popUp(positioning: nil, at: NSPoint(x: 0, y: sender.bounds.minY), in: sender)
    }

    func numberOfRows(in tableView: NSTableView) -> Int { rows.count }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let item = rows[row]
        let width = tableColumn?.width ?? tableView.bounds.width
        let cell = NSTableCellView(frame: NSRect(x: 0, y: 0, width: width, height: table.rowHeight))
        let icon = NSImageView(frame: NSRect(x: 7, y: 6, width: 16, height: 16))
        icon.image = icons.image(item.icon)
        icon.contentTintColor = .secondaryLabelColor
        if case .tag(let tag) = item, let color = tagColors.first(where: { $0.0 == tag.color }) {
            icon.contentTintColor = color.2
        }
        let text = NSTextField(labelWithString: item.label)
        text.font = .systemFont(ofSize: 13)
        text.lineBreakMode = .byTruncatingTail
        text.frame = NSRect(x: 32, y: 4, width: max(0, width - 65), height: 20)
        text.autoresizingMask = [.width]
        cell.textField = text
        cell.imageView = icon
        cell.addSubview(icon)
        cell.addSubview(text)
        if case .tag(let tag) = item {
            let more = NSButton(title: "⋯", target: self, action: #selector(openActions(_:)))
            more.frame = NSRect(x: width - 31, y: 2, width: 23, height: 24)
            more.autoresizingMask = [.minXMargin]
            more.isBordered = false
            more.font = .systemFont(ofSize: 15, weight: .semibold)
            more.setAccessibilityLabel("Actions for \(tag.name)")
            cell.addSubview(more)
        }
        return cell
    }
}


private let tagColors: [(String, String, NSColor)] = [
    ("", "Neutral", .secondaryLabelColor), ("red", "Red", .systemRed), ("orange", "Orange", .systemOrange),
    ("yellow", "Yellow", .systemYellow), ("green", "Green", .systemGreen), ("blue", "Blue", .systemBlue),
    ("purple", "Purple", .systemPurple),
]

private final class TagTableView: NSTableView {
    var actionMenu: ((Int) -> NSMenu?)?
    var chooseRow: ((Int) -> Void)?
    var cancel: (() -> Void)?

    override func menu(for event: NSEvent) -> NSMenu? {
        actionMenu?(row(at: convert(event.locationInWindow, from: nil)))
    }

    override func keyDown(with event: NSEvent) {
        switch event.keyCode {
        case 36, 49, 76:
            chooseRow?(selectedRow)
        case 53:
            cancel?()
        case 109 where event.modifierFlags.contains(.shift):
            let index = max(0, selectedRow)
            if let menu = actionMenu?(index) {
                let rect = rect(ofRow: index)
                menu.popUp(positioning: nil, at: NSPoint(x: rect.minX + 10, y: rect.maxY), in: self)
            }
        default:
            super.keyDown(with: event)
        }
    }
}

final class TagEditor {
    private struct State: Decodable {
        let title: String
        let name: String
        let error: String
        let busy: Bool
    }

    private let panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 380, height: 170),
                               styleMask: [.titled], backing: .buffered, defer: false)
    private let name = NSTextField(string: "")
    private let error = NSTextField(wrappingLabelWithString: "")
    private let save = NSButton(title: "Save", target: nil, action: nil)
    private let cancel = NSButton(title: "Cancel", target: nil, action: nil)
    private let send: LibraryControlHandler

    init(send: @escaping LibraryControlHandler) {
        self.send = send
        let label = NSTextField(labelWithString: "Name")
        label.frame = NSRect(x: 20, y: 128, width: 340, height: 19)
        name.frame = NSRect(x: 20, y: 98, width: 340, height: 24)
        name.setAccessibilityLabel("Tag name")
        error.frame = NSRect(x: 20, y: 48, width: 340, height: 43)
        error.textColor = .systemRed
        cancel.frame = NSRect(x: 190, y: 14, width: 82, height: 28)
        cancel.target = self
        cancel.action = #selector(cancelEditing)
        cancel.keyEquivalent = "\u{1b}"
        save.frame = NSRect(x: 274, y: 14, width: 86, height: 28)
        save.target = self
        save.action = #selector(saveName)
        save.keyEquivalent = "\r"
        name.target = self
        name.action = #selector(saveName)
        panel.isReleasedWhenClosed = false
        [label, name, error, cancel, save].forEach { panel.contentView?.addSubview($0) }
        panel.autorecalculatesKeyViewLoop = true
        panel.initialFirstResponder = name
    }

    func show(parent: NSWindow, data: Data) {
        guard let state = try? JSONDecoder().decode(State.self, from: data), panel.sheetParent == nil else { return }
        name.stringValue = state.name
        panel.title = state.title
        save.title = state.title == "New Tag" ? "Create" : "Save"
        update(data)
        parent.beginSheet(panel) { [weak panel] _ in panel?.orderOut(nil) }
        panel.makeFirstResponder(name)
        name.selectText(nil)
    }

    func update(_ data: Data) {
        guard let state = try? JSONDecoder().decode(State.self, from: data) else { return }
        name.isEnabled = !state.busy
        cancel.isEnabled = !state.busy
        save.isEnabled = !state.busy
        error.stringValue = state.error
        if !state.error.isEmpty {
            panel.makeFirstResponder(name)
            NSAccessibility.post(element: error, notification: .announcementRequested,
                                 userInfo: [.announcement: state.error])
        }
    }

    func close() {
        panel.sheetParent?.endSheet(panel)
    }

    @objc private func cancelEditing() { send(.cancelTagEditor, "") }
    @objc private func saveName() { if save.isEnabled { send(.submitTagEditor, name.stringValue) } }
}
