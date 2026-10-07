import SwiftUI

// MARK: - Status chrome

extension MemoryQRScannerView {
    func statusNote() -> some View {
        HStack(spacing: 2 * .grid) {
            Image(systemName: statusIcon)
                .foregroundStyle(statusColor)
            Text(statusText)
                .font(MemoryFont.meta)
                .foregroundStyle(.fg3)
        }
        .padding(3 * .grid)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
    }

    var statusIcon: String {
        if configured {
            return "checkmark.circle.fill"
        }
        if hasDecodeError {
            return "exclamationmark.triangle.fill"
        }
        return "qrcode.viewfinder"
    }

    var statusColor: Color {
        if configured {
            return .fgSuccess
        }
        if hasDecodeError {
            return .fgSerious
        }
        return .fg3
    }

    var statusText: String {
        if configured {
            return NSLocalizedString("qr.configured", comment: "")
        }
        if hasDecodeError {
            return setupErrorMessage ?? NSLocalizedString("qr.decode.failure", comment: "")
        }
        return NSLocalizedString("qr.scan.hint", comment: "")
    }
}
