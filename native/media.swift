// CLI boundary for import inspection, oriented posters and clipboard export.
// Live playback belongs to the in-process adapter in playback.swift.
import Foundation
import AppKit
import ImageIO
import AVFoundation
import UniformTypeIdentifiers

struct MediaInfo: Codable {
    let type: String
    let format: String
    let width: Int
    let height: Int
}

enum MediaError: Error { case unreadable, unsupported, invalidDimensions, previewWrite }

func writePNG(_ image: CGImage, to url: URL) throws {
    guard let dest = CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 1, nil) else {
        throw MediaError.previewWrite
    }
    CGImageDestinationAddImage(dest, image, nil)
    guard CGImageDestinationFinalize(dest) else { throw MediaError.previewWrite }
}

func preview(_ input: URL, _ output: URL) async throws -> MediaInfo {
    if let source = CGImageSourceCreateWithURL(input as CFURL, [kCGImageSourceShouldCache: false] as CFDictionary),
       let uti = CGImageSourceGetType(source) as String? {
        let formats = ["public.jpeg": "jpg", "public.png": "png", "org.webmproject.webp": "webp", "com.compuserve.gif": "gif"]
        guard let format = formats[uti] else { throw MediaError.unsupported }
        guard let props = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
              var w = (props[kCGImagePropertyPixelWidth] as? NSNumber)?.intValue,
              var h = (props[kCGImagePropertyPixelHeight] as? NSNumber)?.intValue,
              w > 0, h > 0 else { throw MediaError.invalidDimensions }
        let orientation = (props[kCGImagePropertyOrientation] as? NSNumber)?.intValue ?? 1
        if (5...8).contains(orientation) { swap(&w, &h) }
        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: 1024,
            kCGImageSourceShouldCacheImmediately: true,
        ]
        guard let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary),
              CGImageSourceGetStatusAtIndex(source, 0) == .statusComplete else { throw MediaError.unreadable }
        try writePNG(image, to: output)
        return MediaInfo(type: "image", format: format, width: w, height: h)
    }
    // AVFoundation can read formats outside this stage. Inspect the container
    // signature as well as requiring a decodable video track; ignore suffixes.
    let handle = try FileHandle(forReadingFrom: input)
    defer { try? handle.close() }
    let head = try handle.read(upToCount: 32) ?? Data()
    guard head.count >= 12 else { throw MediaError.unreadable }
    let box = String(data: head[4..<8], encoding: .ascii) ?? ""
    guard ["ftyp", "moov", "mdat", "wide"].contains(box) else { throw MediaError.unsupported }
    let format = box != "ftyp" || String(data: head[8..<12], encoding: .ascii) == "qt  " ? "mov" : "mp4"
    // AVFoundation needs a container suffix even when the bytes identify the
    // format. Use a temporary alias derived from the signature, never the
    // user's suffix (the import staging file deliberately has none).
    let alias = output.deletingLastPathComponent().appendingPathComponent(".video-\(UUID().uuidString).\(format)")
    try FileManager.default.linkItem(at: input, to: alias)
    defer { try? FileManager.default.removeItem(at: alias) }
    let asset = AVURLAsset(url: alias)
    guard let track = try await asset.loadTracks(withMediaType: .video).first else { throw MediaError.unreadable }
    let size = try await track.load(.naturalSize)
    let transform = try await track.load(.preferredTransform)
    let rect = CGRect(origin: .zero, size: size).applying(transform)
    let w = Int(abs(rect.width).rounded()), h = Int(abs(rect.height).rounded())
    guard w > 0, h > 0 else { throw MediaError.invalidDimensions }
    let generator = AVAssetImageGenerator(asset: asset)
    generator.appliesPreferredTrackTransform = true
    generator.maximumSize = CGSize(width: 1024, height: 1024)
    let result = try await generator.image(at: .zero)
    try writePNG(result.image, to: output)
    return MediaInfo(type: "video", format: format, width: w, height: h)
}

// Full-resolution first-frame raster for clipboard consumers, derived only
// from the stored original. ImageIO applies orientation and retains alpha.
func raster(_ input: URL, _ output: URL) throws {
    guard let source = CGImageSourceCreateWithURL(input as CFURL, [kCGImageSourceShouldCache: false] as CFDictionary),
          let props = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
          let w = props[kCGImagePropertyPixelWidth] as? Int,
          let h = props[kCGImagePropertyPixelHeight] as? Int, w > 0, h > 0 else { throw MediaError.unreadable }
    let options: [CFString: Any] = [
        kCGImageSourceCreateThumbnailFromImageAlways: true,
        kCGImageSourceCreateThumbnailWithTransform: true,
        kCGImageSourceThumbnailMaxPixelSize: max(w, h),
        kCGImageSourceShouldCacheImmediately: true,
    ]
    guard let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary),
          CGImageSourceGetStatusAtIndex(source, 0) == .statusComplete else { throw MediaError.unreadable }
    try writePNG(image, to: output)
}


// Untrusted web images use a separate, bounded ImageIO path, including ICO.
// No URL is passed to AppKit: every input is an already downloaded local file.
func linkImage(_ input: URL, _ output: URL, _ edge: Int) throws -> MediaInfo {
    guard (16...1024).contains(edge),
          let source = CGImageSourceCreateWithURL(input as CFURL, [kCGImageSourceShouldCache: false] as CFDictionary),
          let props = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
          var w = (props[kCGImagePropertyPixelWidth] as? NSNumber)?.intValue,
          var h = (props[kCGImagePropertyPixelHeight] as? NSNumber)?.intValue,
          w > 0, h > 0, w <= 16384, h <= 16384, w * h <= 40_000_000 else { throw MediaError.invalidDimensions }
    let orientation = (props[kCGImagePropertyOrientation] as? NSNumber)?.intValue ?? 1
    if (5...8).contains(orientation) { swap(&w, &h) }
    let options: [CFString: Any] = [
        kCGImageSourceCreateThumbnailFromImageAlways: true,
        kCGImageSourceCreateThumbnailWithTransform: true,
        kCGImageSourceThumbnailMaxPixelSize: edge,
        kCGImageSourceShouldCacheImmediately: true,
    ]
    guard let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary),
          CGImageSourceGetStatusAtIndex(source, 0) == .statusComplete else { throw MediaError.unreadable }
    try writePNG(image, to: output)
    return MediaInfo(type: "image", format: "png", width: w, height: h)
}

func drawLinkPNG(_ width: Int, _ height: Int, _ output: URL, _ draw: (CGContext) throws -> Void) throws {
    guard width > 0, height > 0, width * height <= 16_000_000,
          let context = CGContext(data: nil, width: width, height: height, bitsPerComponent: 8,
                                  bytesPerRow: width * 4, space: CGColorSpaceCreateDeviceRGB(),
                                  bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { throw MediaError.invalidDimensions }
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(cgContext: context, flipped: false)
    defer { NSGraphicsContext.restoreGraphicsState() }
    try draw(context)
    guard let image = context.makeImage() else { throw MediaError.previewWrite }
    try writePNG(image, to: output)
}

func linkPlaceholder(_ output: URL) throws {
    try drawLinkPNG(768, 512, output) { context in
        context.setFillColor(CGColor(gray: 0.91, alpha: 1))
        context.fill(CGRect(x: 0, y: 0, width: 768, height: 512))
        let text = "Link" as NSString
        let attributes: [NSAttributedString.Key: Any] = [.font: NSFont.systemFont(ofSize: 24, weight: .medium), .foregroundColor: NSColor(white: 0.48, alpha: 1)]
        let size = text.size(withAttributes: attributes)
        text.draw(at: NSPoint(x: (768-size.width)/2, y: (512-size.height)/2), withAttributes: attributes)
    }
}

struct LinkComposition: Decodable { let image: String; let favicon: String; let title: String; let url: String }
func linkCompose(_ spec: URL, _ output: URL) throws {
    let link = try JSONDecoder().decode(LinkComposition.self, from: Data(contentsOf: spec))
    guard let image = NSImage(contentsOfFile: link.image), image.size.width > 0, image.size.height > 0,
          let favicon = NSImage(contentsOfFile: link.favicon) else { throw MediaError.unreadable }
    let width = 768.0, inset = 24.0, textWidth = width - 2 * inset
    let paragraph = NSMutableParagraphStyle(); paragraph.lineBreakMode = .byCharWrapping
    let titleAttrs: [NSAttributedString.Key: Any] = [.font: NSFont.systemFont(ofSize: 18, weight: .medium), .foregroundColor: NSColor(white: 0.12, alpha: 1), .paragraphStyle: paragraph]
    let urlAttrs: [NSAttributedString.Key: Any] = [.font: NSFont.systemFont(ofSize: 13), .foregroundColor: NSColor(white: 0.38, alpha: 1), .paragraphStyle: paragraph]
    let titleHeight = ceil((link.title as NSString).boundingRect(with: CGSize(width: textWidth-30, height: 100000), options: [.usesLineFragmentOrigin, .usesFontLeading], attributes: titleAttrs).height) + 4
    let urlHeight = ceil((link.url as NSString).boundingRect(with: CGSize(width: textWidth, height: 100000), options: [.usesLineFragmentOrigin, .usesFontLeading], attributes: urlAttrs).height) + 4
    let imageHeight = min(1800, width * image.size.height / image.size.width)
    let height = Int(ceil(imageHeight + titleHeight + urlHeight + 3 * inset + 8))
    try drawLinkPNG(Int(width), height, output) { context in
        context.setFillColor(CGColor(gray: 0.98, alpha: 1)); context.fill(CGRect(x: 0, y: 0, width: width, height: Double(height)))
        let scale = min(width / image.size.width, imageHeight / image.size.height)
        let imageWidth = image.size.width * scale, actualHeight = image.size.height * scale
        image.draw(in: NSRect(x: (width-imageWidth)/2, y: Double(height)-imageHeight+(imageHeight-actualHeight)/2, width: imageWidth, height: actualHeight))
        let titleTop = Double(height) - imageHeight - inset
        favicon.draw(in: NSRect(x: inset, y: titleTop-20, width: 20, height: 20))
        (link.title as NSString).draw(with: NSRect(x: inset+30, y: titleTop-titleHeight, width: textWidth-30, height: titleHeight), options: [.usesLineFragmentOrigin, .usesFontLeading], attributes: titleAttrs)
        (link.url as NSString).draw(with: NSRect(x: inset, y: inset, width: textWidth, height: urlHeight), options: [.usesLineFragmentOrigin, .usesFontLeading], attributes: urlAttrs)
    }
}

@main struct KeepsMedia {
    static func main() async {
        do {
            if CommandLine.arguments.count == 2 && CommandLine.arguments[1] == "--documents" {
                guard let url = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first else { throw MediaError.unreadable }
                print(url.path)
                return
            }
            if CommandLine.arguments.count == 4 && CommandLine.arguments[1] == "--raster" {
                try raster(URL(fileURLWithPath: CommandLine.arguments[2]), URL(fileURLWithPath: CommandLine.arguments[3]))
                return
            }
            if CommandLine.arguments.count == 5 && CommandLine.arguments[1] == "--link-image" {
                let info = try linkImage(URL(fileURLWithPath: CommandLine.arguments[2]), URL(fileURLWithPath: CommandLine.arguments[3]), Int(CommandLine.arguments[4]) ?? 0)
                FileHandle.standardOutput.write(try JSONEncoder().encode(info)); return
            }
            if CommandLine.arguments.count == 3 && CommandLine.arguments[1] == "--link-placeholder" {
                try linkPlaceholder(URL(fileURLWithPath: CommandLine.arguments[2])); return
            }
            if CommandLine.arguments.count == 4 && CommandLine.arguments[1] == "--link-compose" {
                try linkCompose(URL(fileURLWithPath: CommandLine.arguments[2]), URL(fileURLWithPath: CommandLine.arguments[3])); return
            }
            guard CommandLine.arguments.count == 3 else { throw MediaError.unsupported }
            let info = try await preview(URL(fileURLWithPath: CommandLine.arguments[1]), URL(fileURLWithPath: CommandLine.arguments[2]))
            FileHandle.standardOutput.write(try JSONEncoder().encode(info))
        } catch {
            FileHandle.standardError.write(Data("Media could not be read: \(error)\n".utf8))
            exit(1)
        }
    }
}
