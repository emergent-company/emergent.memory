import Foundation
import ImageIO
import UniformTypeIdentifiers

/// Turns a shared image into something Memory can parse.
///
/// Two reasons this is not optional:
///
/// - iPhone photos are HEIC/HEIF by default, and Memory's document converter
///   has no HEIC support (its extension/MIME allowlist is JPEG, PNG, WebP, TIFF,
///   GIF, BMP, PDF, …). A HEIC upload is refused `415 unsupported_media_type`.
/// - A 12-megapixel JPEG is several megabytes for no OCR benefit: the text on a
///   page is legible at 2400 px, and the share extension has a ~120 MB memory
///   budget.
///
/// So every shared image is decoded, downsampled to ``maxPixelSize`` and
/// re-encoded as JPEG. Re-encoding also drops the source metadata (GPS, camera
/// model) that a shared photo would otherwise carry into the knowledge graph.
enum ShareImage {
    /// Longest edge of the uploaded image, in pixels.
    static let maxPixelSize = 2400
    /// JPEG quality. 0.82 is visually lossless for text and keeps a downsampled
    /// page photo under ~1 MB.
    static let quality = 0.82

    /// JPEG bytes for any image Core Graphics can decode (HEIC included), or
    /// nil when the data is not an image this device can read.
    static func jpegData(
        from data: Data,
        maxPixelSize: Int = maxPixelSize,
        quality: Double = quality
    ) -> Data? {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil),
              CGImageSourceGetCount(source) > 0
        else { return nil }

        let options: [CFString: Any] = [
            // Always rebuild from the full image: the embedded thumbnail is
            // often 320 px and would make the OCR useless.
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            // Honour EXIF orientation, so a sideways photo is not uploaded
            // sideways.
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixelSize,
        ]
        guard let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary) else {
            return nil
        }

        let output = NSMutableData()
        guard let destination = CGImageDestinationCreateWithData(
            output, UTType.jpeg.identifier as CFString, 1, nil
        ) else { return nil }
        CGImageDestinationAddImage(
            destination,
            image,
            [kCGImageDestinationLossyCompressionQuality: quality] as CFDictionary
        )
        guard CGImageDestinationFinalize(destination) else { return nil }
        return output as Data
    }

    /// Whether Memory can convert this type as an image. Kept deliberately
    /// narrow: it mirrors `XbergSupportedExtensions` on the server.
    static func isConvertibleImageType(_ contentType: String?) -> Bool {
        guard let type = contentType?.lowercased() else { return false }
        return ["image/jpeg", "image/jpg", "image/png", "image/webp", "image/tiff",
                "image/gif", "image/bmp"].contains(type)
    }
}
