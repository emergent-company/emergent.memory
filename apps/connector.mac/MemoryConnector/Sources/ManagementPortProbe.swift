import Darwin
import Foundation

/// The engine's loopback management-port probe and spawn gate.
///
/// Extracted from `EngineManager`: the TCP connect probe and the "may we
/// spawn?" decision are pure and independently testable. `portInUse` is an
/// injectable closure so tests can stub the socket probe without a real TCP
/// connect (the default attempts a connect to `127.0.0.1`).
enum ManagementPortProbe {
    /// Loopback port for the engine's hosted-MCP-server management API
    /// (`relay --api-port`). Bound to 127.0.0.1 by the engine; the app's
    /// `MCPServersAPIClient` talks to it at `http://127.0.0.1:8931`.
    ///
    /// Deliberately NOT 8890: the Diane companion's local API uses 8890 on the
    /// same host, and both apps can run side by side.
    static let port = 8931

    /// Injectable probe: true when something is already listening on the
    /// loopback management port. Settable so tests can stub the socket probe;
    /// the default attempts a TCP connect to `127.0.0.1:8931`.
    nonisolated(unsafe) static var portInUse: () -> Bool = {
        tcpConnect(host: "127.0.0.1", port: UInt16(port))
    }

    /// The pure "may we spawn?" decision for a held management port: abort only
    /// when there is no live child AND the port is in use. A restart now waits
    /// for its previous child to exit, so a spawn is only attempted with no live
    /// child; a port still held then belongs to a foreign process and must not
    /// be raced.
    static func shouldAbortStartForPortConflict(hasLiveProcess: Bool,
                                                portInUse: Bool) -> Bool
    {
        !hasLiveProcess && portInUse
    }

    /// Best-effort synchronous TCP connect probe: true when the connect
    /// succeeds (something is listening on `host:port`).
    static func tcpConnect(host: String, port: UInt16) -> Bool {
        let socketFD = socket(AF_INET, SOCK_STREAM, 0)
        guard socketFD >= 0 else { return false }
        defer { Darwin.close(socketFD) }

        var addr = sockaddr_in()
        addr.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = port.bigEndian
        addr.sin_addr.s_addr = inet_addr(host)

        let result = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.connect(socketFD, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        return result == 0
    }
}
