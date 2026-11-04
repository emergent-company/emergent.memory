"""Projects sub-client: paths, verbs, and response unwrapping."""

from __future__ import annotations

import httpx
import pytest

from emergent import APIError


def test_list_returns_a_bare_array(make_client) -> None:
    client, _ = make_client([httpx.Response(200, json=[{"id": "p1"}])])
    assert client.projects.list() == [{"id": "p1"}]


def test_list_unwraps_projects_key(make_client) -> None:
    client, _ = make_client([httpx.Response(200, json={"projects": [{"id": "p1"}]})])
    assert client.projects.list() == [{"id": "p1"}]


def test_list_unwraps_items_key(make_client) -> None:
    client, _ = make_client([httpx.Response(200, json={"items": [{"id": "p1"}]})])
    assert client.projects.list() == [{"id": "p1"}]


def test_get_quotes_the_identifier(make_client) -> None:
    client, recorder = make_client([httpx.Response(200, json={"id": "a/b"})])
    assert client.projects.get("a/b") == {"id": "a/b"}
    assert str(recorder.last.url) == "https://api.test/api/projects/a%2Fb"


def test_create_posts_payload(make_client) -> None:
    client, recorder = make_client([httpx.Response(201, json={"id": "p1"})])
    assert client.projects.create({"name": "demo"}) == {"id": "p1"}
    assert recorder.last.method == "POST"
    assert recorder.body() == {"name": "demo"}


def test_update_uses_patch(make_client) -> None:
    client, recorder = make_client([httpx.Response(200, json={"id": "p1"})])
    client.projects.update("p1", {"name": "renamed"})
    assert recorder.last.method == "PATCH"
    assert recorder.body() == {"name": "renamed"}


def test_delete_sends_delete(make_client) -> None:
    client, recorder = make_client([httpx.Response(204)])
    client.projects.delete("p1")
    assert recorder.last.method == "DELETE"
    assert str(recorder.last.url) == "https://api.test/api/projects/p1"


def test_errors_propagate_as_api_error(make_client) -> None:
    client, _ = make_client(
        [httpx.Response(403, json={"error": {"message": "denied", "code": "FORBIDDEN"}})]
    )
    with pytest.raises(APIError) as excinfo:
        client.projects.get("p1")
    assert excinfo.value.is_forbidden
    assert excinfo.value.code == "FORBIDDEN"
