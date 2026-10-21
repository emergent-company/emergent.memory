"""Search sub-client: unified search payloads and convenience wrappers."""

from __future__ import annotations

import httpx


def test_minimal_payload(make_client) -> None:
    client, recorder = make_client([httpx.Response(200, json={"results": []})])
    client.search.search("vectors")
    assert recorder.last.method == "POST"
    assert str(recorder.last.url) == "https://api.test/api/search/unified"
    assert recorder.body() == {"query": "vectors", "limit": 20}


def test_optional_fields_use_camel_case(make_client) -> None:
    client, recorder = make_client([httpx.Response(200, json={"results": []})])
    client.search.search(
        "vectors",
        limit=5,
        strategy="hybrid",
        result_types="graph",
        fusion_strategy="rrf",
        include_debug=True,
    )
    assert recorder.body() == {
        "query": "vectors",
        "limit": 5,
        "strategy": "hybrid",
        "resultTypes": "graph",
        "fusionStrategy": "rrf",
        "includeDebug": True,
    }


def test_unset_optional_fields_are_omitted(make_client) -> None:
    client, recorder = make_client([httpx.Response(200, json={"results": []})])
    client.search.search("q", strategy=None, include_debug=False)
    body = recorder.body()
    assert "strategy" not in body
    assert "includeDebug" not in body


def test_graph_search_returns_result_list(make_client) -> None:
    results = [{"score": 0.9, "key": "k"}]
    client, recorder = make_client([httpx.Response(200, json={"results": results})])
    assert client.search.graph_search("q") == results
    assert recorder.body()["resultTypes"] == "graph"


def test_text_search_returns_result_list(make_client) -> None:
    results = [{"score": 0.5, "text": "t"}]
    client, recorder = make_client([httpx.Response(200, json={"results": results})])
    assert client.search.text_search("q", limit=3) == results
    assert recorder.body()["resultTypes"] == "text"
    assert recorder.body()["limit"] == 3


def test_graph_search_tolerates_missing_results_key(make_client) -> None:
    client, _ = make_client([httpx.Response(200, json={})])
    assert client.search.graph_search("q") == []
