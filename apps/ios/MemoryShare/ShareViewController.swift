import SwiftUI
import UIKit

/// The share extension's principal view controller.
///
/// It does as little as possible: read what was shared, present the compose
/// sheet, and complete or cancel the request. Everything that can fail (network,
/// queue) is handled by ``ShareComposeModel`` so the sheet can report it instead
/// of dismissing silently — a share extension that closes with no feedback is
/// indistinguishable from one that lost the item.
final class ShareViewController: UIViewController {
    private var hosting: UIHostingController<AnyView>?

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemGroupedBackground
        presentLoading()
        Task { await load() }
    }

    // MARK: - Loading

    private func load() async {
        let items = (extensionContext?.inputItems as? [NSExtensionItem]) ?? []
        guard let payload = await ShareItemLoader.load(from: items) else {
            presentFailure("Nothing in this share could be remembered.")
            return
        }
        let model = ShareComposeModel(
            payload: payload,
            connection: ShareConnection.load()
        )
        present(
            ShareComposeView(
                model: model,
                onCancel: { [weak self] in self?.cancel() },
                onFinished: { [weak self] in self?.finish() }
            )
        )
    }

    // MARK: - States

    private func presentLoading() {
        present(
            VStack(spacing: 12) {
                ProgressView()
                Text("Reading…")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color(.systemGroupedBackground))
        )
    }

    private func presentFailure(_ message: String) {
        present(
            VStack(spacing: 12) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.largeTitle)
                    .foregroundStyle(.orange)
                Text("Nothing to remember")
                    .font(.headline)
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                Button("Close") { [weak self] in self?.cancel() }
                    .buttonStyle(.borderedProminent)
            }
            .padding(24)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color(.systemGroupedBackground))
        )
    }

    private func present(_ content: some View) {
        hosting?.view.removeFromSuperview()
        hosting?.removeFromParent()

        let controller = UIHostingController(rootView: AnyView(content))
        addChild(controller)
        controller.view.frame = view.bounds
        controller.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(controller.view)
        controller.didMove(toParent: self)
        hosting = controller
    }

    // MARK: - Completing

    private func finish() {
        extensionContext?.completeRequest(returningItems: [], completionHandler: nil)
    }

    /// Cancelling is not an error the host should surface: the user changed
    /// their mind.
    private func cancel() {
        extensionContext?.completeRequest(returningItems: [], completionHandler: nil)
    }
}
