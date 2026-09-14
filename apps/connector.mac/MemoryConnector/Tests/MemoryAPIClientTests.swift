import XCTest
@testable import MemoryConnector

final class MemoryAPIClientTests: XCTestCase {

    private let baseURL = "https://api.example.test"
    private let token = "emt_test_token"

    private let authMeJSON = """
    {"user_id":null,"email":"dev@example.test","scopes":["project.read"],
     "type":"project","project_id":"proj-1","project_name":"Memory Dev",
     "org_id":"org-1","token_id":"tok-1","token_name":"dev"}
    """

    private let profileJSON = """
    {"id":"user-1","subjectId":"sub-1","zitadelUserId":"zit-1","firstName":"Jane",
     "lastName":"Doe","displayName":"Jane Doe","phoneE164":null,
     "avatarObjectKey":null,"email":"jane@example.test"}
    """

    private let projectJSON = """
    {"project":{"id":"proj-1","name":"Memory Dev","orgId":"org-1"}}
    """

    private let orgsJSON = """
    [{"id":"org-1","name":"Emergent Company"},{"id":"org-2","name":"Other Org"}]
    """

    override func setUp() {
        super.setUp()
        StubURLProtocol.registry.reset()
    }

    override func tearDown() {
        StubURLProtocol.registry.reset()
        super.tearDown()
    }

    private func makeSession() -> URLSession {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [StubURLProtocol.self]
        return URLSession(configuration: config)
    }

    private func makeClient(serverURL: String? = nil, token: String? = nil) -> MemoryAPIClient {
        MemoryAPIClient(serverURL: serverURL ?? baseURL,
                        token: token ?? self.token,
                        session: makeSession())
    }

    private func assertRequest(path: String, url: String? = nil, token: String? = nil) {
        guard let request = StubURLProtocol.registry.capturedRequest else {
            XCTFail("no request captured")
            return
        }
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.url?.absoluteString, (url ?? baseURL) + path)
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer \(token ?? self.token)")
    }

    // MARK: - Success decoding

    func testAuthMeDecodesAndSendsAuthHeader() async throws {
        let json = authMeJSON
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let me = try await makeClient().authMe()

        assertRequest(path: "/api/auth/me")
        XCTAssertNil(me.userID)
        XCTAssertEqual(me.email, "dev@example.test")
        XCTAssertEqual(me.scopes, ["project.read"])
        XCTAssertEqual(me.type, "project")
        XCTAssertEqual(me.projectID, "proj-1")
        XCTAssertEqual(me.projectName, "Memory Dev")
        XCTAssertEqual(me.orgID, "org-1")
        XCTAssertEqual(me.tokenID, "tok-1")
        XCTAssertEqual(me.tokenName, "dev")
    }

    func testUserProfileDecodes() async throws {
        let json = profileJSON
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let profile = try await makeClient().userProfile()

        assertRequest(path: "/api/user/profile")
        XCTAssertEqual(profile.id, "user-1")
        XCTAssertEqual(profile.subjectID, "sub-1")
        XCTAssertEqual(profile.zitadelUserID, "zit-1")
        XCTAssertEqual(profile.firstName, "Jane")
        XCTAssertEqual(profile.lastName, "Doe")
        XCTAssertEqual(profile.displayName, "Jane Doe")
        XCTAssertEqual(profile.email, "jane@example.test")
        XCTAssertNil(profile.phoneE164)
        XCTAssertNil(profile.avatarObjectKey)
    }

    func testCurrentProjectDecodes() async throws {
        let json = projectJSON
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let project = try await makeClient().currentProject()

        assertRequest(path: "/api/projects/current")
        XCTAssertEqual(project?.id, "proj-1")
        XCTAssertEqual(project?.name, "Memory Dev")
        XCTAssertEqual(project?.orgID, "org-1")
    }

    func testCurrentProjectNullIsNil() async throws {
        let json = #"{"project":null,"message":"account-level token"}"#
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let project = try await makeClient().currentProject()
        XCTAssertNil(project)
    }

    func testTrailingSlashInBaseURLIsTrimmed() async throws {
        let json = authMeJSON
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        _ = try await makeClient(serverURL: baseURL + "/").authMe()
        assertRequest(path: "/api/auth/me", url: baseURL)
    }

    func testIdentitySnapshotCombinesSources() async throws {
        let me = authMeJSON
        let profile = profileJSON
        let project = projectJSON
        let orgs = orgsJSON
        StubURLProtocol.registry.setHandler { request in
            switch request.url?.path {
            case "/api/auth/me": return .ok(me)
            case "/api/user/profile": return .ok(profile)
            case "/api/projects/current": return .ok(project)
            case "/api/orgs": return .ok(orgs)
            default: return .status(404)
            }
        }

        let snapshot = try await makeClient().identitySnapshot()
        XCTAssertEqual(snapshot.displayName, "Jane Doe")
        XCTAssertEqual(snapshot.email, "jane@example.test")
        XCTAssertEqual(snapshot.initials, "JD")
        XCTAssertEqual(snapshot.project?.id, "proj-1")
        XCTAssertEqual(snapshot.projectName, "Memory Dev")
        XCTAssertEqual(snapshot.organizationName, "Emergent Company")
        XCTAssertEqual(snapshot.organizationID, "org-1")
        XCTAssertTrue(snapshot.hasIdentity)
    }

    func testOrgsDecodesAndRequestsPath() async throws {
        let json = orgsJSON
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let orgs = try await makeClient().orgs()
        assertRequest(path: "/api/orgs")
        XCTAssertEqual(orgs.count, 2)
        XCTAssertEqual(orgs[0].id, "org-1")
        XCTAssertEqual(orgs[0].name, "Emergent Company")
        XCTAssertEqual(orgs[1].name, "Other Org")
    }

    func testSnapshotOrgNotInListLeavesOrganizationNil() async throws {
        let me = authMeJSON
        let profile = profileJSON
        let project = projectJSON
        StubURLProtocol.registry.setHandler { request in
            switch request.url?.path {
            case "/api/auth/me": return .ok(me)
            case "/api/user/profile": return .ok(profile)
            case "/api/projects/current": return .ok(project)
            case "/api/orgs": return .ok(#"[{"id":"org-999","name":"Elsewhere"}]"#)
            default: return .status(404)
            }
        }

        let snapshot = try await makeClient().identitySnapshot()
        XCTAssertNil(snapshot.organization)
        XCTAssertNil(snapshot.organizationName)
        XCTAssertEqual(snapshot.organizationID, "org-1", "id still resolves from project/org fields")
        XCTAssertEqual(snapshot.projectName, "Memory Dev")
        XCTAssertTrue(snapshot.hasIdentity)
    }

    func testSnapshotToleratesOrgsFailure() async throws {
        let me = authMeJSON
        let profile = profileJSON
        let project = projectJSON
        StubURLProtocol.registry.setHandler { request in
            switch request.url?.path {
            case "/api/auth/me": return .ok(me)
            case "/api/user/profile": return .ok(profile)
            case "/api/projects/current": return .ok(project)
            case "/api/orgs": return .status(500)
            default: return .status(404)
            }
        }

        let snapshot = try await makeClient().identitySnapshot()
        XCTAssertNil(snapshot.organization)
        XCTAssertEqual(snapshot.projectName, "Memory Dev")
        XCTAssertEqual(snapshot.displayName, "Jane Doe")
    }

    // MARK: - Projects / project headers / token minting

    func testProjectsDecodes() async throws {
        let json = #"[{"id":"p1","name":"One","orgId":"o1"},{"id":"p2","name":"Two","orgId":"o2"}]"#
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let projects = try await makeClient().projects()
        assertRequest(path: "/api/projects")
        XCTAssertEqual(projects.map(\.id), ["p1", "p2"])
        XCTAssertEqual(projects[0].name, "One")
        XCTAssertEqual(projects[0].orgID, "o1")
    }

    func testProjectAndOrgHeadersSentWhenSet() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(self.authMeJSON) }
        let client = MemoryAPIClient(serverURL: baseURL, token: token,
                                     projectID: "proj-1", orgID: "org-1",
                                     session: StubURLProtocol.makeSession())
        _ = try await client.authMe()

        let request = try XCTUnwrap(StubURLProtocol.registry.capturedRequest)
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Project-ID"), "proj-1")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Org-ID"), "org-1")
    }

    func testProjectHeadersAbsentWhenNil() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(self.authMeJSON) }
        _ = try await makeClient().authMe()

        let request = try XCTUnwrap(StubURLProtocol.registry.capturedRequest)
        XCTAssertNil(request.value(forHTTPHeaderField: "X-Project-ID"))
        XCTAssertNil(request.value(forHTTPHeaderField: "X-Org-ID"))
    }

    // MARK: - Avatar

    func testAvatarDataReturnsImageBytesAndHeaders() async throws {
        let bytes = Data([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A])
        StubURLProtocol.registry.setHandler { _ in
            StubResult(statusCode: 200, data: bytes, error: nil)
        }

        let data = try await makeClient().avatarData(projectID: "proj-1")

        XCTAssertEqual(data, bytes)
        let request = try XCTUnwrap(StubURLProtocol.registry.capturedRequest)
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.url?.absoluteString, baseURL + "/api/user/avatar")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer emt_test_token")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Project-ID"), "proj-1")
    }

    func testAvatarDataNilOn404() async throws {
        StubURLProtocol.registry.setHandler { _ in .status(404) }
        let data = try await makeClient().avatarData(projectID: "proj-1")
        XCTAssertNil(data)
    }

    func testAvatarDataUnauthorized() async {
        StubURLProtocol.registry.setHandler { _ in .status(401) }
        await assertClientError(.authFailed) { _ = try await self.makeClient().avatarData(projectID: "proj-1") }
    }

    func testAvatarDataServerError() async {
        StubURLProtocol.registry.setHandler { _ in .status(500) }
        await assertClientError(.httpStatus(500)) { _ = try await self.makeClient().avatarData(projectID: "proj-1") }
    }

    func testAvatarDataTransportError() async {
        StubURLProtocol.registry.setHandler { _ in .failure(URLError(.cannotConnectToHost)) }
        do {
            _ = try await makeClient().avatarData(projectID: "proj-1")
            XCTFail("expected unreachable error")
        } catch let error as MemoryAPIError {
            guard case .unreachable = error else {
                return XCTFail("expected .unreachable, got \(error)")
            }
        } catch {
            XCTFail("unexpected \(error)")
        }
    }

    // MARK: - Dashboard endpoints

    func testObjectCountDecodesAndSendsHeaders() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(#"{"count":42}"#) }

        let count = try await makeClient().objectCount(projectID: "proj-1", accessToken: "user-access")

        XCTAssertEqual(count, 42)
        let request = try XCTUnwrap(StubURLProtocol.registry.capturedRequest)
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.url?.absoluteString, baseURL + "/api/graph/objects/count")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer user-access")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Project-ID"), "proj-1")
    }

    func testRelationshipCountDecodesAndSendsHeaders() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(#"{"count":7}"#) }

        let count = try await makeClient().relationshipCount(projectID: "proj-1", accessToken: "user-access")

        XCTAssertEqual(count, 7)
        let request = try XCTUnwrap(StubURLProtocol.registry.capturedRequest)
        XCTAssertEqual(request.url?.absoluteString, baseURL + "/api/graph/relationships/count")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer user-access")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Project-ID"), "proj-1")
    }

    func testCountMissingKeyIsNil() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok("{}") }
        let count = try await makeClient().objectCount(projectID: "proj-1", accessToken: "user-access")
        XCTAssertNil(count)
    }

    func testSchemaStatsDecodesSnakeCase() async throws {
        let json = """
        {"total_types":5,"enabled_types":3,"template_types":1,"custom_types":2,
         "discovered_types":0,"total_objects":120,"types_with_objects":4}
        """
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let stats = try await makeClient().schemaStats(projectID: "proj-1", accessToken: "user-access")

        XCTAssertEqual(stats?.totalTypes, 5)
        XCTAssertEqual(stats?.enabledTypes, 3)
        XCTAssertEqual(stats?.customTypes, 2)
        XCTAssertEqual(stats?.totalObjects, 120)
        let request = try XCTUnwrap(StubURLProtocol.registry.capturedRequest)
        XCTAssertEqual(request.url?.absoluteString, baseURL + "/api/schema-registry/projects/proj-1/stats")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Project-ID"), "proj-1")
    }

    func testSchemaStatsDecodesCamelCase() async throws {
        let json = #"{"totalTypes":5,"enabledTypes":3,"customTypes":2,"totalObjects":120}"#
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let stats = try await makeClient().schemaStats(projectID: "proj-1", accessToken: "user-access")

        XCTAssertEqual(stats?.totalTypes, 5)
        XCTAssertEqual(stats?.enabledTypes, 3)
        XCTAssertEqual(stats?.customTypes, 2)
        XCTAssertEqual(stats?.totalObjects, 120)
    }

    func testAgentsDecodesEnvelope() async throws {
        let json = """
        {"success":true,"data":[
          {"id":"a1","name":"Extractor","enabled":true,"description":"pulls facts",
           "lastRunStatus":"success","unknownField":123}
        ]}
        """
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let agents = try await makeClient().agents(projectID: "proj-1", accessToken: "user-access")

        XCTAssertEqual(agents.count, 1)
        XCTAssertEqual(agents[0].id, "a1")
        XCTAssertEqual(agents[0].name, "Extractor")
        XCTAssertEqual(agents[0].description, "pulls facts")
        XCTAssertEqual(agents[0].isEnabled, true)
        XCTAssertEqual(agents[0].lastRunStatus, "success")
        let request = try XCTUnwrap(StubURLProtocol.registry.capturedRequest)
        XCTAssertEqual(request.url?.absoluteString, baseURL + "/api/projects/proj-1/agents")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer user-access")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Project-ID"), "proj-1")
    }

    func testAgentsAcceptsCamelIsEnabledAndEmptyData() async throws {
        StubURLProtocol.registry.setHandler { _ in
            .ok(#"{"success":true,"data":[{"id":"a2","name":"X","isEnabled":false}]}"#)
        }
        let agents = try await makeClient().agents(projectID: "proj-1", accessToken: "user-access")
        XCTAssertEqual(agents.count, 1)
        XCTAssertEqual(agents[0].isEnabled, false)

        StubURLProtocol.registry.reset()
        StubURLProtocol.registry.setHandler { _ in .ok(#"{"success":true}"#) }
        let empty = try await makeClient().agents(projectID: "proj-1", accessToken: "user-access")
        XCTAssertEqual(empty, [])
    }

    // MARK: - Agent definitions

    func testAgentDefinitionsDecodesSnakeCaseAndNullTools() async throws {
        let json = """
        {"success":true,"data":[
          {"id":"def-1","name":"Researcher","description":"finds things","model":"gpt-4o",
           "tools":null,"banned_tools":["shell"],"enabled":true,"visibility":"project",
           "trigger_type":"manual","is_default":true,"flow_type":"sequential",
           "unknownField":"ignored"}
        ]}
        """
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let defs = try await makeClient().agentDefinitions(projectID: "proj-1", accessToken: "user-access")

        XCTAssertEqual(defs.count, 1)
        let d = try XCTUnwrap(defs.first)
        XCTAssertEqual(d.id, "def-1")
        XCTAssertEqual(d.name, "Researcher")
        XCTAssertEqual(d.description, "finds things")
        XCTAssertEqual(d.model, "gpt-4o")
        XCTAssertNil(d.tools, "tools: null stays nil")
        XCTAssertEqual(d.bannedTools, ["shell"])
        XCTAssertEqual(d.enabled, true)
        XCTAssertEqual(d.visibility, "project")
        XCTAssertEqual(d.triggerType, "manual")
        XCTAssertEqual(d.isDefault, true)
        XCTAssertEqual(d.flowType, "sequential")
        let request = try XCTUnwrap(StubURLProtocol.registry.capturedRequest)
        XCTAssertEqual(request.url?.absoluteString, baseURL + "/api/projects/proj-1/agent-definitions")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer user-access")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Project-ID"), "proj-1")
    }

    func testAgentDefinitionsDecodesCamelCaseAndEmptyData() async throws {
        let json = """
        {"success":true,"data":[
          {"id":"def-2","name":"Writer","tools":["notes"],"bannedTools":[],
           "enabled":false,"triggerType":"auto","isDefault":false,"flowType":"parallel"}
        ]}
        """
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let defs = try await makeClient().agentDefinitions(projectID: "proj-1", accessToken: "user-access")
        XCTAssertEqual(defs.count, 1)
        XCTAssertEqual(defs[0].tools, ["notes"])
        XCTAssertEqual(defs[0].bannedTools, [])
        XCTAssertEqual(defs[0].enabled, false)
        XCTAssertEqual(defs[0].triggerType, "auto")
        XCTAssertEqual(defs[0].isDefault, false)
        XCTAssertEqual(defs[0].flowType, "parallel")
        XCTAssertNil(defs[0].description)
        XCTAssertNil(defs[0].model)

        StubURLProtocol.registry.reset()
        StubURLProtocol.registry.setHandler { _ in .ok(#"{"success":true}"#) }
        let empty = try await makeClient().agentDefinitions(projectID: "proj-1", accessToken: "user-access")
        XCTAssertEqual(empty, [])
    }

    func testAgentDefinitionsDecodesToolCountAndSnakeCase() async throws {
        let json = """
        {"success":true,"data":[
          {"id":"d1","name":"A","toolCount":5},
          {"id":"d2","name":"B","tool_count":0},
          {"id":"d3","name":"C"}
        ]}
        """
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let defs = try await makeClient().agentDefinitions(projectID: "proj-1", accessToken: "user-access")

        XCTAssertEqual(defs.count, 3)
        XCTAssertEqual(defs[0].toolCount, 5)
        XCTAssertEqual(defs[1].toolCount, 0)
        XCTAssertNil(defs[2].toolCount, "absent toolCount stays nil")
    }

    // MARK: - Agent definition detail

    func testAgentDefinitionDecodesFullNestedModelAndWorkspace() async throws {
        let json = """
        {"success":true,"data":{
          "id":"def-1","projectId":"proj-1","name":"Researcher",
          "description":"finds things","systemPrompt":"You are a researcher.",
          "model":{"name":"gemini-2.0-flash",
                   "nativeTools":["google_search","url_context"],"temperature":0.2},
          "tools":["search","notes"],"bannedTools":["shell"],
          "skills":["summarize"],"autoLoadSkills":true,
          "enabled":true,"isDefault":false,"visibility":"project",
          "triggerType":"manual","flowType":"sequential",
          "workspaceConfig":{"enabled":true,"tools":["workspace_bash","workspace_read"]},
          "unknownField":"ignored"}}
        """
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let detail = try await makeClient().agentDefinition(projectID: "proj-1",
                                                            id: "def-1",
                                                            accessToken: "user-access")

        let d = try XCTUnwrap(detail)
        XCTAssertEqual(d.id, "def-1")
        XCTAssertEqual(d.name, "Researcher")
        XCTAssertEqual(d.description, "finds things")
        XCTAssertEqual(d.systemPrompt, "You are a researcher.")
        XCTAssertEqual(d.model, "gemini-2.0-flash", "nested model.name is flattened")
        XCTAssertEqual(d.modelNativeTools, ["google_search", "url_context"])
        XCTAssertEqual(d.tools, ["search", "notes"])
        XCTAssertEqual(d.bannedTools, ["shell"])
        XCTAssertEqual(d.skills, ["summarize"])
        XCTAssertEqual(d.autoLoadSkills, true)
        XCTAssertEqual(d.enabled, true)
        XCTAssertEqual(d.isDefault, false)
        XCTAssertEqual(d.visibility, "project")
        XCTAssertEqual(d.triggerType, "manual")
        XCTAssertEqual(d.flowType, "sequential")
        XCTAssertEqual(d.workspaceTools, ["workspace_bash", "workspace_read"])

        let request = try XCTUnwrap(StubURLProtocol.registry.capturedRequest)
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.url?.absoluteString, baseURL + "/api/projects/proj-1/agent-definitions/def-1")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer user-access")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Project-ID"), "proj-1")
    }

    func testAgentDefinitionDecodesSnakeCaseKeys() async throws {
        let json = """
        {"success":true,"data":{
          "id":"def-2","name":"Snake","system_prompt":"sys",
          "model":{"name":"m","native_tools":["code_execution"]},
          "banned_tools":["x"],"skills":["s"],"auto_load_skills":true,
          "is_default":true,"trigger_type":"auto","flow_type":"parallel",
          "workspace_config":{"tools":["workspace_write"]}}}
        """
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let d = try await makeClient().agentDefinition(projectID: "proj-1",
                                                       id: "def-2",
                                                       accessToken: "user-access")

        XCTAssertEqual(d?.systemPrompt, "sys")
        XCTAssertEqual(d?.model, "m")
        XCTAssertEqual(d?.modelNativeTools, ["code_execution"])
        XCTAssertEqual(d?.bannedTools, ["x"])
        XCTAssertEqual(d?.skills, ["s"])
        XCTAssertEqual(d?.autoLoadSkills, true)
        XCTAssertEqual(d?.isDefault, true)
        XCTAssertEqual(d?.triggerType, "auto")
        XCTAssertEqual(d?.flowType, "parallel")
        XCTAssertEqual(d?.workspaceTools, ["workspace_write"])
    }

    func testAgentDefinitionNullToolsStaysNil() async throws {
        let json = #"{"success":true,"data":{"id":"d","name":"N","tools":null,"bannedTools":null}}"#
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let d = try await makeClient().agentDefinition(projectID: "proj-1",
                                                       id: "d",
                                                       accessToken: "user-access")
        XCTAssertNotNil(d)
        XCTAssertNil(d?.tools, "tools: null stays nil")
        XCTAssertNil(d?.bannedTools)
    }

    func testAgentDefinitionEmptyToolsIsEmptyArray() async throws {
        let json = #"{"success":true,"data":{"id":"d","name":"N","tools":[],"skills":[]}}"#
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let d = try await makeClient().agentDefinition(projectID: "proj-1",
                                                       id: "d",
                                                       accessToken: "user-access")
        XCTAssertEqual(d?.tools, [], "tools: [] stays an empty array")
        XCTAssertEqual(d?.skills, [])
    }

    func testAgentDefinitionModelAsPlainString() async throws {
        let json = #"{"success":true,"data":{"id":"d","name":"N","model":"gpt-4o"}}"#
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let d = try await makeClient().agentDefinition(projectID: "proj-1",
                                                       id: "d",
                                                       accessToken: "user-access")
        XCTAssertEqual(d?.model, "gpt-4o")
        XCTAssertNil(d?.modelNativeTools)
    }

    func testAgentDefinitionMissingDataIsNil() async throws {
        StubURLProtocol.registry.setHandler { _ in .ok(#"{"success":true}"#) }
        let d = try await makeClient().agentDefinition(projectID: "proj-1",
                                                       id: "nope",
                                                       accessToken: "user-access")
        XCTAssertNil(d)
    }

    func testProjectDetailDecodesServerCasing() async throws {
        let json = """
        {"id":"proj-1","name":"Memory Dev","orgId":"org-1","project_info":"notes",
         "stats":{"documentCount":1,"objectCount":2,"relationshipCount":3,
                  "totalJobs":4,"runningJobs":5,"queuedJobs":6}}
        """
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let detail = try await makeClient().projectDetail(projectID: "proj-1", accessToken: "user-access")

        XCTAssertEqual(detail?.name, "Memory Dev")
        XCTAssertEqual(detail?.orgID, "org-1")
        XCTAssertEqual(detail?.projectInfo, "notes")
        XCTAssertEqual(detail?.stats?.documentCount, 1)
        XCTAssertEqual(detail?.stats?.objectCount, 2)
        XCTAssertEqual(detail?.stats?.relationshipCount, 3)
        XCTAssertEqual(detail?.stats?.totalJobs, 4)
        XCTAssertEqual(detail?.stats?.runningJobs, 5)
        XCTAssertEqual(detail?.stats?.queuedJobs, 6)
        let request = try XCTUnwrap(StubURLProtocol.registry.capturedRequest)
        XCTAssertEqual(request.url?.absoluteString, baseURL + "/api/projects/proj-1?include_stats=true")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer user-access")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Project-ID"), "proj-1")
    }

    func testProjectDetailDecodesSnakeCaseStats() async throws {
        let json = """
        {"name":"P","org_id":"org-2",
         "stats":{"document_count":1,"object_count":2,"relationship_count":3,
                  "total_jobs":4,"running_jobs":5,"queued_jobs":6}}
        """
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let detail = try await makeClient().projectDetail(projectID: "proj-1", accessToken: "user-access")

        XCTAssertEqual(detail?.orgID, "org-2")
        XCTAssertNil(detail?.projectInfo)
        XCTAssertEqual(detail?.stats?.objectCount, 2)
        XCTAssertEqual(detail?.stats?.queuedJobs, 6)
    }

    func testProjectDetailDecodesPascalCaseStats() async throws {
        let json = """
        {"name":"P","OrgId":"org-3","ProjectInfo":"info",
         "stats":{"DocumentCount":1,"ObjectCount":2,"RelationshipCount":3,
                  "TotalJobs":4,"RunningJobs":5,"QueuedJobs":6}}
        """
        StubURLProtocol.registry.setHandler { _ in .ok(json) }

        let detail = try await makeClient().projectDetail(projectID: "proj-1", accessToken: "user-access")

        XCTAssertEqual(detail?.orgID, "org-3")
        XCTAssertEqual(detail?.projectInfo, "info")
        XCTAssertEqual(detail?.stats?.documentCount, 1)
        XCTAssertEqual(detail?.stats?.objectCount, 2)
        XCTAssertEqual(detail?.stats?.runningJobs, 5)
        XCTAssertEqual(detail?.stats?.queuedJobs, 6)
    }

    // MARK: - Errors

    func testUnauthorizedMapsToAuthFailed() async {
        StubURLProtocol.registry.setHandler { _ in .status(401) }
        await assertClientError(.authFailed) { _ = try await self.makeClient().authMe() }
    }

    func testForbiddenMapsToAuthFailed() async {
        StubURLProtocol.registry.setHandler { _ in .status(403) }
        await assertClientError(.authFailed) { _ = try await self.makeClient().userProfile() }
    }

    func testServerErrorMapsToHTTPStatus() async {
        StubURLProtocol.registry.setHandler { _ in .status(500) }
        await assertClientError(.httpStatus(500)) { _ = try await self.makeClient().authMe() }
    }

    func testMalformedJSONMapsToDecoding() async {
        StubURLProtocol.registry.setHandler { _ in .ok("this is not json") }
        do {
            _ = try await makeClient().authMe()
            XCTFail("expected decoding error")
        } catch let error as MemoryAPIError {
            guard case .decoding = error else {
                XCTFail("expected .decoding, got \(error)")
                return
            }
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    func testTransportErrorMapsToUnreachable() async {
        StubURLProtocol.registry.setHandler { _ in .failure(URLError(.notConnectedToInternet)) }
        do {
            _ = try await makeClient().authMe()
            XCTFail("expected unreachable error")
        } catch let error as MemoryAPIError {
            guard case .unreachable = error else {
                XCTFail("expected .unreachable, got \(error)")
                return
            }
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    func testMissingConfigurationFailsFast() async {
        await assertClientError(.notConfigured) {
            _ = try await MemoryAPIClient(serverURL: "", token: self.token, session: self.makeSession()).authMe()
        }
        await assertClientError(.notConfigured) {
            _ = try await MemoryAPIClient(serverURL: self.baseURL, token: "", session: self.makeSession()).userProfile()
        }
        XCTAssertNil(StubURLProtocol.registry.capturedRequest, "no network call for unconfigured client")
    }

    // MARK: - Helpers

    private func assertClientError(
        _ expected: MemoryAPIError,
        file: StaticString = #filePath,
        line: UInt = #line,
        _ operation: () async throws -> Void
    ) async {
        do {
            try await operation()
            XCTFail("expected \(expected)", file: file, line: line)
        } catch let error as MemoryAPIError {
            XCTAssertEqual(error, expected, file: file, line: line)
        } catch {
            XCTFail("unexpected error \(error)", file: file, line: line)
        }
    }
}
