import AppKit

final class SpaceSymbolPicker: NSStackView, NSTextFieldDelegate {
    struct Choice: Encodable {
        let icon: String
        let emoji: String
    }

    private let icons: ControlIcons
    private let mode = NSSegmentedControl(labels: ["Reicon", "Emoji"], trackingMode: .selectOne, target: nil, action: nil)
    private let reicon = NSPopUpButton()
    private let emoji = NSTextField(string: "")
    private let choose = NSButton(title: "Choose…", target: nil, action: nil)
    private let preview = NSImageView()
    private let error = NSTextField(labelWithString: "Enter one emoji.")
    private let emojiRow = NSStackView()
    private var keys: [String] = []

    init(icons: ControlIcons) {
        self.icons = icons
        super.init(frame: .zero)
        orientation = .vertical
        alignment = .trailing
        spacing = 5

        mode.setAccessibilityLabel("Space icon type")
        mode.target = self
        mode.action = #selector(changeSymbolType)
        reicon.setAccessibilityLabel("Reicon icon")
        reicon.target = self
        reicon.action = #selector(refreshPreview)
        emoji.placeholderString = "Emoji"
        emoji.setAccessibilityLabel("Space emoji")
        emoji.widthAnchor.constraint(equalToConstant: 55).isActive = true
        emoji.alignment = .center
        emoji.font = .systemFont(ofSize: 16)
        emoji.delegate = self
        choose.setAccessibilityLabel("Open the macOS character picker")
        choose.target = self
        choose.action = #selector(chooseEmoji)
        preview.widthAnchor.constraint(equalToConstant: 20).isActive = true
        preview.heightAnchor.constraint(equalToConstant: 20).isActive = true
        preview.imageScaling = .scaleProportionallyDown
        preview.setAccessibilityLabel("Space icon preview")
        error.font = .systemFont(ofSize: 11)
        error.textColor = .systemRed

        let options = NSStackView(views: [mode, preview])
        options.alignment = .centerY
        options.spacing = 8
        emojiRow.addArrangedSubview(emoji)
        emojiRow.addArrangedSubview(choose)
        emojiRow.alignment = .centerY
        emojiRow.spacing = 6
        [options, reicon, emojiRow, error].forEach { addArrangedSubview($0) }
        mode.selectedSegment = 0
        changeSymbolType()
    }

    required init?(coder: NSCoder) { fatalError("Space symbol picker is created programmatically") }

    func setIcons(_ values: [String]) {
        guard values != keys else { return }
        keys = values
        reicon.removeAllItems()
        for key in values {
            reicon.addItem(withTitle: key.capitalized)
            reicon.lastItem?.representedObject = key
            reicon.lastItem?.image = icons.spaceImage(key)
        }
    }

    func setChoice(icon: String, emoji value: String) {
        reicon.select(reicon.itemArray.first { ($0.representedObject as? String) == icon } ?? reicon.item(at: 0))
        emoji.stringValue = value
        mode.selectedSegment = value.isEmpty ? 0 : 1
        changeSymbolType()
    }

    func setEnabled(_ enabled: Bool) {
        [mode, reicon, emoji, choose].forEach { $0.isEnabled = enabled }
    }

    func choice() -> Choice? {
        guard let icon = reicon.selectedItem?.representedObject as? String else { return nil }
        if mode.selectedSegment == 0 { return Choice(icon: icon, emoji: "") }
        guard let value = normalizedSpaceEmoji(emoji.stringValue) else {
            error.isHidden = false
            window?.makeFirstResponder(emoji)
            return nil
        }
        return Choice(icon: icon, emoji: value)
    }

    func controlTextDidChange(_ notification: Notification) { refreshPreview() }

    @objc private func changeSymbolType() {
        reicon.isHidden = mode.selectedSegment == 1
        emojiRow.isHidden = mode.selectedSegment == 0
        refreshPreview()
    }

    @objc private func refreshPreview() {
        if mode.selectedSegment == 1 {
            let value = normalizedSpaceEmoji(emoji.stringValue)
            error.isHidden = emoji.stringValue.isEmpty || value != nil
            preview.image = value.map(spaceEmojiImage)
        } else {
            error.isHidden = true
            let key = reicon.selectedItem?.representedObject as? String
            preview.image = key.flatMap { icons.spaceImage($0) }
        }
    }

    @objc private func chooseEmoji() {
        window?.makeFirstResponder(emoji)
        emoji.selectText(nil)
        NSApp.orderFrontCharacterPalette(nil)
    }
}

private func normalizedSpaceEmoji(_ text: String) -> String? {
    let value = text.trimmingCharacters(in: .whitespacesAndNewlines)
    guard value.count == 1, value.utf8.count <= 128, let first = value.unicodeScalars.first else { return nil }
    let scalars = value.unicodeScalars
    if scalars.contains(where: { $0.value == 0xFE0E }) { return nil }
    if (0x1F1E6...0x1F1FF).contains(first.value) {
        return scalars.count == 2 && scalars.allSatisfy { (0x1F1E6...0x1F1FF).contains($0.value) } ? value : nil
    }
    if first == "#" || first == "*" || (0x30...0x39).contains(first.value) {
        let rest = Array(scalars.dropFirst().map(\.value))
        return rest == [0x20E3] || rest == [0xFE0F, 0x20E3] ? value : nil
    }
    if first.properties.isEmojiModifier { return nil }
    if first.properties.isEmojiPresentation ||
       (first.properties.isEmoji && scalars.contains(where: { $0.value == 0xFE0F })) {
        return value
    }
    return nil
}

func spaceEmojiImage(_ emoji: String) -> NSImage {
    NSImage(size: NSSize(width: 18, height: 18), flipped: false) { bounds in
        let glyph = emoji as NSString
        let font = NSFont(name: "Apple Color Emoji", size: 15) ?? .systemFont(ofSize: 15)
        let attributes: [NSAttributedString.Key: Any] = [.font: font]
        let size = glyph.size(withAttributes: attributes)
        glyph.draw(at: NSPoint(x: bounds.midX - size.width / 2, y: bounds.midY - size.height / 2), withAttributes: attributes)
        return true
    }
}
