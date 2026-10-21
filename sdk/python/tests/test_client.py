"""Root client: construction, auth wiring, context propagation, lifecycle."""

from __future__ import annotations

import httpx
import pytest

from emergent import Client, Config


class TestConstruction:
    def test_server_url_is_required(self) -> None:
        with pytest.raises(ValueError, match="server_url"):
            Client(Config(server_url=""))

    def test_trailing_slash_is_normalized(self, make_client) -> None:
        client, recorder = make_client(
            [httpx.Response(200, json=[])], server_url="https://api.test/"
        )
        assert client.projects.list() == []
        assert str(recorder.last.url) == "https://api.test/api/projects"

    def test_sub_clients_are_wired(self, make_client) -> None:
        client, _ = make_client()
        for name in (
            "chat",
            "agents",
            "agent_definitions",
            "mcp",
            "graph",
            "search",
            "schemas",
            "skills",
            "documents",
            "branches",
            "api_tokens",
            "tasks",
            "projects",
            "orgs",
        ):
            assert hasattr(client, name), name

    def test_repr_exposes_context(self, make_client) -> None:
        client, _ = make_client(org_id="org_1", project_id="proj_1")
        text = repr(client)
        assert "https://api.test" in text
        assert "org_1" in text
        assert "proj_1" in text


class TestAuthentication:
    def test_api_key_uses_x_api_key(self, make_client) -> None:
        client, recorder = make_client([httpx.Response(200, json=[])], api_key="plain")
        client.projects.list()
        assert recorder.last.headers["x-api-key"] == "plain"
        assert "authorization" not in recorder.last.headers

    def test_emt_token_uses_bearer(self, make_client) -> None:
        client, recorder = make_client([httpx.Response(200, json=[])], api_key="emt_123")
        client.projects.list()
        assert recorder.last.headers["authorization"] == "Bearer emt_123"
        assert "x-api-key" not in recorder.last.headers

    def test_oauth_token_uses_bearer(self, make_client) -> None:
        client, recorder = make_client(
            [httpx.Response(200, json=[])], mode="oauth", api_key="", access_token="jwt"
        )
        client.projects.list()
        assert recorder.last.headers["authorization"] == "Bearer jwt"

    def test_authenticate_request_mutates_and_returns_headers(self, make_client) -> None:
        client, _ = make_client(org_id="org_1", project_id="proj_1")
        headers = client.authenticate_request({"Accept": "json"})
        assert headers["Accept"] == "json"
        assert headers["X-API-Key"] == "test-key"
        assert headers["X-Org-ID"] == "org_1"
        assert headers["X-Project-ID"] == "proj_1"


class TestContext:
    def test_context_headers_reach_context_scoped_clients(self, make_client) -> None:
        client, recorder = make_client(
            [httpx.Response(200, json={"results": []})], org_id="org_1", project_id="proj_1"
        )
        client.search.search("q")
        headers = recorder.last.headers
        assert headers["x-org-id"] == "org_1"
        assert headers["x-project-id"] == "proj_1"

    def test_set_context_propagates_to_every_sub_client(self, make_client) -> None:
        responses = [httpx.Response(200, json={"results": []}), httpx.Response(200, json=[])]
        client, recorder = make_client(responses)
        client.set_context("org_2", "proj_2")
        client.search.search("q")
        client.projects.list()
        assert recorder.requests[0].headers["x-org-id"] == "org_2"
        assert recorder.requests[0].headers["x-project-id"] == "proj_2"
        # projects / orgs are not context-scoped
        assert "x-org-id" not in recorder.requests[1].headers
        assert "x-project-id" not in recorder.requests[1].headers

    def test_projects_never_send_context_headers(self, make_client) -> None:
        client, recorder = make_client(
            [httpx.Response(200, json=[])], org_id="org_1", project_id="proj_1"
        )
        client.projects.list()
        assert "x-org-id" not in recorder.last.headers
        assert "x-project-id" not in recorder.last.headers


class TestLifecycle:
    def test_context_manager_closes_owned_transport(self) -> None:
        client = Client.from_api_key("https://api.test", "key")
        with client:
            pass
        assert client._http.is_closed

    def test_close_leaves_supplied_transport_open(self, make_client) -> None:
        client, _ = make_client()
        http = client._http
        client.close()
        assert not http.is_closed
        http.close()

    def test_from_api_key_and_oauth_constructors(self) -> None:
        keyed = Client.from_api_key("https://api.test", "key", org_id="o", project_id="p")
        assert keyed.authenticate_request({})["X-Org-ID"] == "o"
        oauth = Client.from_oauth_token("https://api.test", "jwt")
        assert oauth.authenticate_request({})["Authorization"] == "Bearer jwt"
