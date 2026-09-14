import XCTest
@testable import MemoryConnector

final class MemoryIdentityTests: XCTestCase {

    // MARK: - initials

    func testInitialsVariants() {
        XCTAssertEqual(MemoryIdentity.initials(from: "Mary Anne Watson"), "MA")
        XCTAssertEqual(MemoryIdentity.initials(from: "jane doe"), "JD")
        XCTAssertEqual(MemoryIdentity.initials(from: "Cher"), "C")
        XCTAssertEqual(MemoryIdentity.initials(from: "Ø"), "Ø")
        XCTAssertEqual(MemoryIdentity.initials(from: "élodie durand"), "ÉD")
        XCTAssertEqual(MemoryIdentity.initials(from: "  jane   doe  "), "JD")
        XCTAssertEqual(MemoryIdentity.initials(from: ""), "")
        XCTAssertEqual(MemoryIdentity.initials(from: "   "), "")
    }

    func testInitialsUseFirstTwoWordsRuneSafely() {
        // Memory web rule: first rune of the first two whitespace words.
        XCTAssertEqual(MemoryIdentity.initials(from: "John Ronald Reuel Tolkien"), "JR")
        XCTAssertEqual(MemoryIdentity.initials(from: "🥑 Avocado Toast"), "🥑A")
    }

    // MARK: - display name / email fallbacks

    func testDisplayNamePrefersProfileDisplayName() {
        let snapshot = IdentitySnapshot(
            authMe: authMe(email: "dev@example.test", projectName: "Memory Dev"),
            profile: profile(displayName: "Jane Doe", firstName: "Ignored", lastName: "Name")
        )
        XCTAssertEqual(snapshot.displayName, "Jane Doe")
        XCTAssertEqual(snapshot.initials, "JD")
    }

    func testDisplayNameFallsBackToProfileFullName() {
        let snapshot = IdentitySnapshot(
            profile: profile(displayName: nil, firstName: "Mary", lastName: "Anne")
        )
        XCTAssertEqual(snapshot.displayName, "Mary Anne")
        XCTAssertEqual(snapshot.initials, "MA")
    }

    func testDisplayNameFallsBackToAuthEmailLocalPart() {
        let snapshot = IdentitySnapshot(authMe: authMe(email: "dev.user@example.test", projectName: nil))
        XCTAssertEqual(snapshot.displayName, "dev.user")
        XCTAssertEqual(snapshot.initials, "D")
    }

    func testDisplayNameFallsBackToProjectThenTokenName() {
        let projectFallback = IdentitySnapshot(authMe: authMe(email: nil, projectName: "Memory Dev", tokenName: "tok"))
        XCTAssertEqual(projectFallback.displayName, "Memory Dev")

        let tokenFallback = IdentitySnapshot(authMe: authMe(email: nil, projectName: nil, tokenName: "dev-token"))
        XCTAssertEqual(tokenFallback.displayName, "dev-token")
    }

    func testEmailPrefersProfileOverAuth() {
        let snapshot = IdentitySnapshot(
            authMe: authMe(email: "token@example.test"),
            profile: profile(email: "profile@example.test")
        )
        XCTAssertEqual(snapshot.email, "profile@example.test")

        let authOnly = IdentitySnapshot(authMe: authMe(email: "token@example.test"))
        XCTAssertEqual(authOnly.email, "token@example.test")
    }

    func testEmptySourcesYieldEmptyIdentity() {
        let snapshot = IdentitySnapshot()
        XCTAssertFalse(snapshot.hasIdentity)
        XCTAssertEqual(snapshot.displayName, "")
        XCTAssertEqual(snapshot.email, "")
        XCTAssertEqual(snapshot.initials, "")
    }

    func testSandboxTokenHasIdentityFromAuthMe() {
        // Sandbox tokens have no user_id but still yield an identity snapshot.
        let snapshot = IdentitySnapshot(authMe: authMe(userID: nil, email: "sandbox@example.test"))
        XCTAssertTrue(snapshot.hasIdentity)
        XCTAssertEqual(snapshot.displayName, "sandbox")
    }

    // MARK: - Project / organisation names + ids

    func testProjectNameAndIDPreferCurrentProject() {
        let snapshot = IdentitySnapshot(
            authMe: authMe(projectName: "Auth Name"),
            project: ProjectInfo(id: "proj-1", name: "Current Name", orgID: "org-1")
        )
        XCTAssertEqual(snapshot.projectName, "Current Name")
        XCTAssertEqual(snapshot.projectID, "proj-1")
    }

    func testProjectNameFallsBackToAuthMe() {
        let snapshot = IdentitySnapshot(authMe: authMe(projectName: "Auth Name"))
        XCTAssertEqual(snapshot.projectName, "Auth Name")
        XCTAssertEqual(snapshot.projectID, "proj-1")
    }

    func testProjectNameNilWhenBlank() {
        let snapshot = IdentitySnapshot(
            authMe: authMe(projectName: "   "),
            project: ProjectInfo(id: "proj-1", name: nil, orgID: nil)
        )
        XCTAssertNil(snapshot.projectName)
    }

    func testOrganizationNameFromResolvedOrg() {
        let snapshot = IdentitySnapshot(
            authMe: authMe(),
            project: ProjectInfo(id: "proj-1", name: "Memory Dev", orgID: "org-1"),
            organization: OrgInfo(id: "org-1", name: "Emergent Company")
        )
        XCTAssertEqual(snapshot.organizationName, "Emergent Company")
        XCTAssertEqual(snapshot.organizationID, "org-1")
    }

    func testEmptyOrganizationNameIsNil() {
        let snapshot = IdentitySnapshot(organization: OrgInfo(id: "org-1", name: "  "))
        XCTAssertNil(snapshot.organizationName)
        XCTAssertEqual(snapshot.organizationID, "org-1")
    }

    func testOrganizationIDFallsBackToProjectThenAuthMe() {
        let fromProject = IdentitySnapshot(
            authMe: authMe(),
            project: ProjectInfo(id: "proj-1", name: "Memory Dev", orgID: "org-project")
        )
        XCTAssertEqual(fromProject.organizationID, "org-project")

        let fromAuth = IdentitySnapshot(authMe: authMe())
        XCTAssertEqual(fromAuth.organizationID, "org-1")
    }

    func testSandboxIdentityWithoutOrg() {
        let snapshot = IdentitySnapshot(authMe: authMe(userID: nil, email: "sandbox@example.test"))
        XCTAssertTrue(snapshot.hasIdentity)
        XCTAssertNil(snapshot.organization)
        XCTAssertNil(snapshot.organizationName)
        XCTAssertEqual(snapshot.organizationID, "org-1", "org id may still come from authMe")
    }

    // MARK: - Fixtures

    private func authMe(userID: String? = "user-1",
                        email: String? = "dev@example.test",
                        projectName: String? = "Memory Dev",
                        tokenName: String? = "dev") -> AuthMe {
        AuthMe(userID: userID, email: email, scopes: ["project.read"], type: "project",
               projectID: "proj-1", projectName: projectName, orgID: "org-1",
               tokenID: "tok-1", tokenName: tokenName)
    }

    private func profile(displayName: String? = nil,
                         firstName: String? = nil,
                         lastName: String? = nil,
                         email: String? = nil) -> UserProfile {
        UserProfile(id: "user-1", subjectID: "sub-1", zitadelUserID: "zit-1",
                    firstName: firstName, lastName: lastName, displayName: displayName,
                    phoneE164: nil, avatarObjectKey: nil, email: email)
    }
}
