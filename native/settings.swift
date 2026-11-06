import AppKit

private final class SettingsGroup: NSView {
    override func viewDidChangeEffectiveAppearance() {
        super.viewDidChangeEffectiveAppearance()
        needsDisplay = true
    }

    override func draw(_ dirtyRect: NSRect) {
        let card = NSBezierPath(roundedRect: bounds.insetBy(dx: 0.5, dy: 0.5), xRadius: 13, yRadius: 13)
        NSColor.labelColor.withAlphaComponent(0.065).setFill()
        card.fill()
        NSColor.separatorColor.withAlphaComponent(0.25).setStroke()
        card.stroke()
    }
}

private final class SettingsDocument: NSView {
    override var isFlipped: Bool { true }
}

final class KeepsSettings: NSWindowController, NSTableViewDataSource, NSTableViewDelegate, NSSearchFieldDelegate {
    private static let shortcutRows = [
        ("Search captures", "⌘F"), ("Import media", "⌘O"), ("Paste files, images or links", "⌘V"),
        ("Copy selected captures", "⌘C"), ("Select all visible captures", "⌘A"),
        ("Move between captures", "Arrow keys"), ("Quick Look", "Space"), ("Open selection", "Return"),
        ("Show or hide Sidebar", "⌃⌘S"), ("Open Settings", "⌘,"),
    ]

    private enum Page: String, CaseIterable {
        case appearance = "Appearance"
        case spaces = "Spaces"
        case shortcuts = "Shortcuts"
        case updates = "Updates"

        var description: String {
            switch self {
            case .appearance: return "Choose how Keeps looks on this Mac."
            case .spaces: return "Manage the local libraries in your Sidebar."
            case .shortcuts: return "Get around the library from your keyboard."
            case .updates: return "See your installed version and check for new releases."
            }
        }

        var keywords: String {
            switch self {
            case .appearance: return "Theme System Light Dark Translucent window Transparency"
            case .spaces: return "Storage folder Library Show in Finder Name Icon Save Changes Visibility Hide from Sidebar"
            case .shortcuts: return KeepsSettings.shortcutRows.map(\.0).joined(separator: " ")
            case .updates: return "Installed version App updates Check for Updates Open Release Download"
            }
        }

        var icon: String { self == .shortcuts ? "type" : rawValue.lowercased() }
    }

    private struct State: Decodable {
        struct Space: Decodable, Equatable {
            let id: String
            let name: String
            let icon: String
            let emoji: String?
            let hidden: Bool?
        }

        let appearance: String
        let translucent: Bool
        let canEdit: Bool
        let spaces: [Space]
        let selectedSpace: String
        let activeSpace: String
        let spaceIcons: [String]
        let spaceError: String
        let canEditSpace: Bool
        let openSpaces: Bool?
        let storagePath: String
        let version: String
        let updateStatus: String
        let checking: Bool
        let releaseReady: Bool
    }

    private let send: LibraryControlHandler
    private let icons: ControlIcons
    private let search = NSSearchField()
    private let sidebar = NSTableView()
    private let detail = NSScrollView()
    private var choices = Page.allCases
    private var page: Page = .appearance
    private var reloading = false
    private var pages: [Page: NSView] = [:]
    private let noResults = NSTextField(labelWithString: "No matching settings")
    private let appearance = NSSegmentedControl(labels: ["System", "Light", "Dark"], trackingMode: .selectOne, target: nil, action: nil)
    private let translucency = NSSwitch()
    private let spaces = NSPopUpButton()
    private let name = NSTextField(string: "")
    private let spaceIcon: SpaceSymbolPicker
    private let storage = NSTextField(labelWithString: "")
    private let spaceError = NSTextField(wrappingLabelWithString: "")
    private let reveal = NSButton(title: "Show in Finder", target: nil, action: nil)
    private let save = NSButton(title: "Save Changes", target: nil, action: nil)
    private let visibility = NSButton(title: "Hide from Sidebar", target: nil, action: nil)
    private let version = NSTextField(labelWithString: "")
    private let updates = NSTextField(wrappingLabelWithString: "")
    private let check = NSButton(title: "Check for Updates…", target: nil, action: nil)
    private let release = NSButton(title: "Open Release", target: nil, action: nil)
    private let appearances = ["system", "light", "dark"]
    private var savedSpaces: [State.Space] = []
    private var selectedSpace: State.Space?
    private var activeSpace = ""

    init(icons: ControlIcons, send: @escaping LibraryControlHandler) {
        self.icons = icons
        self.send = send
        self.spaceIcon = SpaceSymbolPicker(icons: icons)
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 760, height: 510),
                              styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                              backing: .buffered, defer: false)
        window.title = "Keeps Settings"
        window.titleVisibility = .hidden
        window.titlebarAppearsTransparent = true
        window.isMovableByWindowBackground = true
        window.autorecalculatesKeyViewLoop = true
        window.contentMinSize = NSSize(width: 610, height: 385)
        window.isReleasedWhenClosed = false
        super.init(window: window)

        let root = NSView()
        window.contentView = root
        let navigation = NSVisualEffectView()
        navigation.material = .sidebar
        navigation.blendingMode = .withinWindow
        navigation.state = .followsWindowActiveState
        let list = NSScrollView()
        list.drawsBackground = false
        list.hasVerticalScroller = true
        list.documentView = sidebar
        sidebar.style = .sourceList
        sidebar.headerView = nil
        sidebar.rowHeight = 32
        sidebar.backgroundColor = .clear
        sidebar.allowsEmptySelection = true
        sidebar.autoresizingMask = [.width]
        sidebar.setAccessibilityLabel("Settings pages")
        let column = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("page"))
        column.width = 160
        column.resizingMask = .autoresizingMask
        sidebar.addTableColumn(column)
        sidebar.delegate = self
        sidebar.dataSource = self

        search.placeholderString = "Search Settings"
        search.setAccessibilityLabel("Search Settings")
        search.delegate = self
        search.sendsSearchStringImmediately = true
        (search.cell as? NSSearchFieldCell)?.searchButtonCell?.image = icons.image("search")
        noResults.textColor = .secondaryLabelColor
        noResults.isHidden = true

        detail.drawsBackground = false
        detail.hasVerticalScroller = true
        detail.autohidesScrollers = true
        [navigation, detail].forEach { add($0, to: root) }
        [search, list, noResults].forEach { add($0, to: navigation) }
        NSLayoutConstraint.activate([
            navigation.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            navigation.topAnchor.constraint(equalTo: root.topAnchor),
            navigation.bottomAnchor.constraint(equalTo: root.bottomAnchor),
            navigation.widthAnchor.constraint(equalToConstant: 190),
            detail.leadingAnchor.constraint(equalTo: navigation.trailingAnchor),
            detail.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            detail.topAnchor.constraint(equalTo: root.topAnchor, constant: 28),
            detail.bottomAnchor.constraint(equalTo: root.bottomAnchor),
            search.leadingAnchor.constraint(equalTo: navigation.leadingAnchor, constant: 12),
            search.trailingAnchor.constraint(equalTo: navigation.trailingAnchor, constant: -12),
            search.topAnchor.constraint(equalTo: navigation.topAnchor, constant: 42),
            list.leadingAnchor.constraint(equalTo: navigation.leadingAnchor, constant: 5),
            list.trailingAnchor.constraint(equalTo: navigation.trailingAnchor, constant: -5),
            list.topAnchor.constraint(equalTo: search.bottomAnchor, constant: 12),
            list.bottomAnchor.constraint(equalTo: navigation.bottomAnchor, constant: -12),
            noResults.leadingAnchor.constraint(equalTo: search.leadingAnchor),
            noResults.topAnchor.constraint(equalTo: list.topAnchor, constant: 8),
        ])

        appearance.target = self
        appearance.action = #selector(changeAppearance)
        appearance.setAccessibilityLabel("Appearance")
        translucency.target = self
        translucency.action = #selector(changeTranslucency)
        translucency.setAccessibilityLabel("Translucent window")
        pages[.appearance] = makePage(.appearance, groups: [
            ("Window", [row("Appearance", control: appearance),
                        row("Translucent window", subtitle: "Let the desktop show through the library window.", control: translucency)])
        ])

        spaces.target = self
        spaces.action = #selector(changeSpace)
        spaces.setAccessibilityLabel("Space")
        spaces.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        spaces.widthAnchor.constraint(lessThanOrEqualToConstant: 235).isActive = true
        name.setAccessibilityLabel("Space name")
        name.widthAnchor.constraint(equalToConstant: 176).isActive = true
        name.target = self
        name.action = #selector(saveSpace)
        storage.textColor = .secondaryLabelColor
        storage.lineBreakMode = .byTruncatingMiddle
        storage.setAccessibilityLabel("Storage folder path")
        spaceError.textColor = .systemRed
        reveal.target = self
        reveal.action = #selector(revealSpace)
        save.target = self
        save.action = #selector(saveSpace)
        visibility.target = self
        visibility.action = #selector(toggleSpace)
        pages[.spaces] = makePage(.spaces, groups: [
            ("Library", [row("Space", control: spaces), row("Storage folder", subtitleView: storage, control: reveal)]),
            ("Sidebar", [row("Name", control: name), row("Icon", control: spaceIcon), row("", control: save),
                         row("Visibility", subtitle: "Hidden Spaces keep their files and can be shown again here.", control: visibility)]),
        ], status: spaceError)

        pages[.shortcuts] = makePage(.shortcuts, groups: [
            ("Library and navigation", Self.shortcutRows.map { title, key in
                let badge = NSTextField(labelWithString: key)
                badge.font = .monospacedSystemFont(ofSize: 12, weight: .medium)
                badge.textColor = .secondaryLabelColor
                return row(title, control: badge)
            })
        ])

        check.target = self
        check.action = #selector(checkUpdates)
        release.target = self
        release.action = #selector(openRelease)
        updates.textColor = .secondaryLabelColor
        let updateActions = NSStackView(views: [release, check])
        updateActions.spacing = 7
        pages[.updates] = makePage(.updates, groups: [
            ("Keeps", [row("Installed version", control: version), row("App updates", control: updateActions)])
        ], status: updates)
        select(.appearance)
        window.center()
    }

    required init?(coder: NSCoder) { fatalError("Settings is created programmatically") }

    func update(_ data: Data) {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        guard let state = try? decoder.decode(State.self, from: data) else { return }
        appearance.selectedSegment = appearances.firstIndex(of: state.appearance) ?? 0
        appearance.isEnabled = state.canEdit
        translucency.state = state.translucent ? .on : .off
        translucency.isEnabled = state.canEdit
        spaceIcon.setIcons(state.spaceIcons)
        if savedSpaces != state.spaces || activeSpace != state.activeSpace {
            savedSpaces = state.spaces
            activeSpace = state.activeSpace
            spaces.removeAllItems()
            for space in savedSpaces {
                let status = space.hidden == true ? " (Hidden)" : space.id == activeSpace ? " (Active)" : ""
                spaces.addItem(withTitle: space.name + status)
                spaces.lastItem?.representedObject = space.id
                spaces.lastItem?.image = space.emoji.map(spaceEmojiImage) ?? icons.spaceImage(space.icon)
            }
        }
        spaces.select(spaces.itemArray.first { ($0.representedObject as? String) == state.selectedSpace })
        let selected = savedSpaces.first { $0.id == state.selectedSpace }
        if selected?.id != selectedSpace?.id || selected?.name != selectedSpace?.name ||
            selected?.icon != selectedSpace?.icon || selected?.emoji != selectedSpace?.emoji {
            name.stringValue = selected?.name ?? ""
            spaceIcon.setChoice(icon: selected?.icon ?? "folder", emoji: selected?.emoji ?? "")
        }
        selectedSpace = selected
        let editable = state.canEditSpace && selected != nil
        let custom = editable && selected?.id != "default"
        name.isEnabled = custom
        visibility.isEnabled = custom
        spaceIcon.setEnabled(editable)
        save.isEnabled = editable
        visibility.title = selected?.hidden == true ? "Show in Sidebar" : "Hide from Sidebar"
        storage.stringValue = state.storagePath.isEmpty ? "Folder unavailable" : state.storagePath
        storage.toolTip = state.storagePath
        reveal.isEnabled = !state.storagePath.isEmpty
        spaceError.stringValue = state.spaceError
        spaceError.isHidden = state.spaceError.isEmpty
        version.stringValue = state.version
        updates.stringValue = state.updateStatus
        updates.isHidden = state.updateStatus.isEmpty
        check.isEnabled = !state.checking
        release.isHidden = !state.releaseReady
        if state.openSpaces == true {
            search.stringValue = ""
            choices = Page.allCases
            select(.spaces)
        }
    }

    func numberOfRows(in tableView: NSTableView) -> Int { choices.count }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let choice = choices[row]
        let icon = NSImageView()
        icon.image = icons.image(choice.icon)
        icon.contentTintColor = .secondaryLabelColor
        icon.widthAnchor.constraint(equalToConstant: 16).isActive = true
        icon.heightAnchor.constraint(equalToConstant: 16).isActive = true
        let title = NSTextField(labelWithString: choice.rawValue)
        title.font = .systemFont(ofSize: 13, weight: .medium)
        let label = NSStackView(views: [icon, title])
        label.spacing = 9
        label.alignment = .centerY
        let cell = NSTableCellView()
        cell.textField = title
        add(label, to: cell)
        NSLayoutConstraint.activate([
            label.leadingAnchor.constraint(equalTo: cell.leadingAnchor, constant: 8),
            label.trailingAnchor.constraint(lessThanOrEqualTo: cell.trailingAnchor),
            label.centerYAnchor.constraint(equalTo: cell.centerYAnchor),
        ])
        return cell
    }

    func tableViewSelectionDidChange(_ notification: Notification) {
        if !reloading && choices.indices.contains(sidebar.selectedRow) {
            show(choices[sidebar.selectedRow])
        }
    }

    func controlTextDidChange(_ notification: Notification) {
        let words = search.stringValue.split(whereSeparator: \.isWhitespace)
        choices = Page.allCases.filter { option in
            let text = option.rawValue + " " + option.description + " " + option.keywords
            return words.allSatisfy { text.localizedCaseInsensitiveContains(String($0)) }
        }
        select(choices.contains(page) ? page : choices.first ?? page)
    }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy commandSelector: Selector) -> Bool {
        switch commandSelector {
        case #selector(NSResponder.moveDown(_:)), #selector(NSResponder.insertNewline(_:)):
            if !choices.isEmpty {
                window?.makeFirstResponder(sidebar)
                if sidebar.selectedRow < 0 { sidebar.selectRowIndexes(IndexSet(integer: 0), byExtendingSelection: false) }
            }
        default:
            return false
        }
        return true
    }

    private func select(_ selected: Page) {
        reloading = true
        sidebar.reloadData()
        if let index = choices.firstIndex(of: selected) {
            sidebar.selectRowIndexes(IndexSet(integer: index), byExtendingSelection: false)
        } else {
            sidebar.deselectAll(nil)
        }
        reloading = false
        noResults.isHidden = !choices.isEmpty
        show(selected)
    }

    private func show(_ selected: Page) {
        let document = SettingsDocument()
        detail.documentView = document
        document.translatesAutoresizingMaskIntoConstraints = false
        if let content = pages[selected], !choices.isEmpty {
            content.removeFromSuperview()
            add(content, to: document)
            NSLayoutConstraint.activate([
                content.leadingAnchor.constraint(equalTo: document.leadingAnchor, constant: 26),
                content.trailingAnchor.constraint(equalTo: document.trailingAnchor, constant: -26),
                content.topAnchor.constraint(equalTo: document.topAnchor, constant: 17),
                content.bottomAnchor.constraint(equalTo: document.bottomAnchor, constant: -26),
                document.widthAnchor.constraint(equalTo: detail.contentView.widthAnchor),
            ])
        } else {
            document.frame = .zero
        }
        page = selected
        detail.contentView.scroll(to: .zero)
        detail.reflectScrolledClipView(detail.contentView)
    }

    private func makePage(_ page: Page, groups: [(String, [NSView])], status: NSView? = nil) -> NSView {
        let heading = NSTextField(labelWithString: page.rawValue)
        heading.font = .systemFont(ofSize: 19, weight: .semibold)
        let subtitle = NSTextField(wrappingLabelWithString: page.description)
        subtitle.textColor = .secondaryLabelColor
        let layout = stack([stack([heading, subtitle], spacing: 4)], spacing: 22)
        for (title, rows) in groups {
            let label = NSTextField(labelWithString: title)
            label.font = .systemFont(ofSize: 12, weight: .semibold)
            label.textColor = .secondaryLabelColor
            let card = SettingsGroup()
            let content = stack([], spacing: 0)
            for (index, row) in rows.enumerated() {
                if index > 0 {
                    let rule = NSBox()
                    rule.boxType = .separator
                    rule.heightAnchor.constraint(equalToConstant: 1).isActive = true
                    append(rule, to: content)
                }
                append(row, to: content)
            }
            add(content, to: card)
            NSLayoutConstraint.activate([
                content.leadingAnchor.constraint(equalTo: card.leadingAnchor, constant: 13),
                content.trailingAnchor.constraint(equalTo: card.trailingAnchor, constant: -13),
                content.topAnchor.constraint(equalTo: card.topAnchor),
                content.bottomAnchor.constraint(equalTo: card.bottomAnchor),
            ])
            append(stack([label, card], spacing: 7), to: layout)
        }
        if let status { append(status, to: layout) }
        return layout
    }

    private func stack(_ views: [NSView], spacing: CGFloat) -> NSStackView {
        let stack = NSStackView()
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = spacing
        for view in views { append(view, to: stack) }
        return stack
    }

    private func append(_ view: NSView, to stack: NSStackView) {
        stack.addArrangedSubview(view)
        view.widthAnchor.constraint(equalTo: stack.widthAnchor).isActive = true
    }

    private func row(_ title: String, subtitle: String? = nil, subtitleView: NSTextField? = nil, control: NSView) -> NSView {
        let label = NSTextField(labelWithString: title)
        label.font = .systemFont(ofSize: 13, weight: .medium)
        var labels: [NSView] = [label]
        if let subtitle {
            let text = NSTextField(wrappingLabelWithString: subtitle)
            text.font = .systemFont(ofSize: 11)
            text.textColor = .secondaryLabelColor
            labels.append(text)
        } else if let subtitleView {
            labels.append(subtitleView)
        }
        let left = stack(labels, spacing: 3)
        left.setContentHuggingPriority(.defaultLow, for: .horizontal)
        control.setContentHuggingPriority(.defaultHigh, for: .horizontal)
        let row = NSStackView(views: [left, control])
        row.alignment = .centerY
        row.edgeInsets = NSEdgeInsets(top: 11, left: 1, bottom: 11, right: 1)
        row.spacing = 12
        return row
    }

    private func add(_ child: NSView, to parent: NSView) {
        child.translatesAutoresizingMaskIntoConstraints = false
        parent.addSubview(child)
    }

    @objc private func changeAppearance() {
        if appearances.indices.contains(appearance.selectedSegment) {
            send(.appearance, appearances[appearance.selectedSegment])
        }
    }

    @objc private func changeTranslucency() { send(.translucency, translucency.state == .on ? "true" : "false") }
    @objc private func changeSpace() {
        if let id = spaces.selectedItem?.representedObject as? String { send(.settingsSpace, id) }
    }
    @objc private func saveSpace() {
        guard save.isEnabled, let space = selectedSpace, let choice = spaceIcon.choice() else { return }
        struct Edit: Encodable { let id: String; let name: String; let icon: String; let emoji: String }
        let data = try! JSONEncoder().encode(Edit(id: space.id, name: name.stringValue, icon: choice.icon, emoji: choice.emoji))
        send(.editSpace, String(decoding: data, as: UTF8.self))
    }
    @objc private func toggleSpace() {
        if let space = selectedSpace { send(.toggleSpace, "\(space.id):\(space.hidden != true)") }
    }
    @objc private func revealSpace() { send(.revealSpace, "") }
    @objc private func checkUpdates() { send(.checkUpdates, "") }
    @objc private func openRelease() { send(.openRelease, "") }
}
