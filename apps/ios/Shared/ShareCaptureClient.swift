import Foundation

/// Why a capture did not reach Memory.
///
/// Split into cases the caller acts on differently: a transport failure is
/// worth retrying later (and is what stops a drain loop from hammering a dead
/// network), a rejection is Memory's own answer and will not change on retry
/// (unsupported file, no text, empty share), and a configuration failure means
/// this device is not connected.
enum CaptureClientError: Error, Equatable, Sendable {
    /// No usable connection (never onboarded, or the group had no credentials).
    case configuration(String)
    /// The request could not be performed or got no HTTP response.
    case transport(String)
    /// Memory answered non-2xx; `message` is its own wording.
    case rejected(status: Int, message: String)

    /// Whether waiting and trying again could succeed.
    var isRetryable: Bool {
        switch self {
        case .configuration: false
        case .transport: true
        case let .rejected(status, _): status >= 500
        }
    }

    /// Whether a drain should stop instead of continuing with the next queued
    /// capture. A dead or misconfigured connection is not item-specific, so
    /// trying the rest would only burn timeouts; a rejection is about one item.
    var haltsDrain: Bool {
        switch self {
        case .configuration, .transport: true
        case let .rejected(status, _): status >= 500
        }
    }
}

extension CaptureClientError: LocalizedError {
    var errorDescription: String? {
        switch self {
        case let .configuration(message): message
        case let .transport(message): message
        case let .rejected(_, message): message
        }
    }
}

/// Posts captures to the gateway's remember routes.
///
/// Both routes are on the device credential's surface
/// (`POST /api/projects/{projectID}/remember` and `…/remember/file`), and the
/// gateway answers with Memory's own body — `{run_id, status}` on success,
/// `{error: {code, message}}` on refusal — so the refusal the user sees is
/// Memory's, not this client's paraphrase.
///
/// Deliberately dependency-free (Foundation only, no app types) so the share
/// extension and the app compile the same code.
struct ShareCaptureClient: Sendable {
    let connection: ShareConnection
    let session: URLSession

    /// Link captures answer as soon as the run exists (async remember), so a
    /// short deadline is enough.
    static let linkTimeout: TimeInterval = 20
    /// File captures do not: Memory polls document conversion for up to 120 s
    /// before it answers at all, on top of the upload and the agent run.
    static let fileTimeout: TimeInterval = 300

    init(connection: ShareConnection, session: URLSession = .shared) {
        self.connection = connection
        self.session = session
    }

    // MARK: - Link / note captures

    /// Posts a link, selection or note. Returns the run id when Memory reported
    /// one.
    @discardableResult
    func remember(_ capture: ShareCapture) async throws -> String? {
        guard connection.isUsable else {
            throw CaptureClientError.configuration("Connect a Memory project before sharing.")
        }
        var request = try URLRequest(url: rememberURL(), timeoutInterval: Self.linkTimeout)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue(connection.apiKey, forHTTPHeaderField: "X-API-Key")
        request.httpBody = try JSONEncoder().encode(Body(capture: capture))

        let (data, response) = try await perform(request)
        return try runID(from: data, response: response)
    }

    // MARK: - File captures

    /// Uploads a shared file with the user's comment.
    @discardableResult
    func rememberFile(_ capture: ShareCapture, payload: Data) async throws -> String? {
        try await rememberFile(
            payload: payload,
            fileName: capture.fileName ?? "shared-capture",
            contentType: capture.contentType ?? "application/octet-stream",
            comment: capture.comment
        )
    }

    /// Uploads `payload` as a multipart `file` part with the user's comment.
    ///
    /// The gateway's contract is `file` + `comment`; it maps the comment onto
    /// Memory's remember `guide`, which is also the fallback document text when
    /// the parser finds none (a photo rather than a document photo).
    @discardableResult
    func rememberFile(
        payload: Data,
        fileName: String,
        contentType: String,
        comment: String
    ) async throws -> String? {
        guard connection.isUsable else {
            throw CaptureClientError.configuration("Connect a Memory project before sharing.")
        }
        let boundary = "MemoryShareBoundary-\(UUID().uuidString)"
        var request = try URLRequest(
            url: rememberURL(appending: "file"),
            timeoutInterval: Self.fileTimeout
        )
        request.httpMethod = "POST"
        request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        request.setValue(connection.apiKey, forHTTPHeaderField: "X-API-Key")
        request.httpBody = Self.multipartBody(
            boundary: boundary,
            fileName: fileName,
            contentType: contentType,
            payload: payload,
            comment: comment
        )

        let (data, response) = try await perform(request)
        return try runID(from: data, response: response)
    }

    // MARK: - Request pieces

    /// The gateway's remember URL for the connection's project.
    func rememberURL(appending suffix: String? = nil) throws -> URL {
        guard var url = URL(string: connection.apiBaseURL) else {
            throw CaptureClientError.configuration("The Memory gateway URL is not valid.")
        }
        let project = connection.projectID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed)
            ?? connection.projectID
        url.append(path: "api/projects/\(project)/remember")
        if let suffix {
            url.append(path: suffix)
        }
        return url
    }

    private struct Body: Encodable {
        let url: String
        let title: String
        let text: String
        let comment: String

        init(capture: ShareCapture) {
            url = capture.url ?? ""
            title = capture.title ?? ""
            text = capture.text ?? ""
            comment = capture.comment
        }
    }

    private func perform(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        do {
            let (data, response) = try await session.data(for: request)
            guard let http = response as? HTTPURLResponse else {
                throw CaptureClientError.transport("Memory returned no HTTP response.")
            }
            return (data, http)
        } catch let error as CaptureClientError {
            throw error
        } catch {
            throw CaptureClientError.transport(error.localizedDescription)
        }
    }

    /// Validates the status and pulls out the run id, or throws Memory's own
    /// refusal.
    private func runID(from data: Data, response: HTTPURLResponse) throws -> String? {
        guard (200 ..< 300).contains(response.statusCode) else {
            throw CaptureClientError.rejected(
                status: response.statusCode,
                message: Self.errorMessage(from: data, status: response.statusCode)
            )
        }
        return (try? JSONDecoder().decode(Accepted.self, from: data))?.runID
    }

    /// Memory's error envelope (`{"error":{"code","message"}}`), the gateway's
    /// plain `{"error":"…"}`, or the status as a last resort.
    static func errorMessage(from data: Data, status: Int) -> String {
        if let envelope = try? JSONDecoder().decode(Envelope.self, from: data),
           let message = envelope.error?.message,
           !message.isEmpty
        {
            return message
        }
        if let plain = try? JSONDecoder().decode(Plain.self, from: data), !plain.error.isEmpty {
            return plain.error
        }
        return "Memory returned HTTP \(status)."
    }

    /// Builds a multipart body by hand: `file` (with the real filename so
    /// Memory's extension sniffing sees what the user shared) and `comment`.
    ///
    /// The comment part is omitted when it is blank: the gateway maps it onto
    /// Memory's remember guide, and an empty guide would be stored as the
    /// document's text — turning a text-free photo into a blank document instead
    /// of the `no_content` refusal that explains itself.
    static func multipartBody(
        boundary: String,
        fileName: String,
        contentType: String,
        payload: Data,
        comment: String
    ) -> Data {
        var body = Data()
        func line(_ text: String) {
            body.append(Data(text.utf8))
        }
        line("--\(boundary)\r\n")
        line("Content-Disposition: form-data; name=\"file\"; filename=\"\(escaped(fileName))\"\r\n")
        line("Content-Type: \(contentType)\r\n\r\n")
        body.append(payload)
        line("\r\n")
        let trimmed = comment.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty {
            line("--\(boundary)\r\n")
            line("Content-Disposition: form-data; name=\"comment\"\r\n\r\n")
            line("\(trimmed)\r\n")
        }
        line("--\(boundary)--\r\n")
        return body
    }

    /// Quotes and newlines would end the part header early.
    private static func escaped(_ fileName: String) -> String {
        fileName
            .replacingOccurrences(of: "\\", with: "_")
            .replacingOccurrences(of: "\"", with: "_")
            .replacingOccurrences(of: "\r", with: "_")
            .replacingOccurrences(of: "\n", with: "_")
    }
}

/// An accepted remember: `{"run_id": "…", "status": "queued"}`. The run id is
/// optional because acceptance, not the id, is what the caller acts on.
private struct Accepted: Decodable {
    let runID: String?

    enum CodingKeys: String, CodingKey {
        case runID = "run_id"
    }
}

/// Memory's error envelope: `{"error": {"code", "message"}}`.
private struct Envelope: Decodable {
    struct Detail: Decodable {
        let message: String?
    }

    let error: Detail?
}

/// The gateway's own errors are a plain `{"error": "…"}` string.
private struct Plain: Decodable {
    let error: String
}
