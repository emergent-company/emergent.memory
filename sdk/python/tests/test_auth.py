"""Authentication providers and the ``make_provider`` factory."""

from __future__ import annotations

import pytest

from emergent import APIKeyProvider, APITokenProvider, OAuthProvider, make_provider


class TestAPIKeyProvider:
    def test_sends_x_api_key_header(self) -> None:
        assert APIKeyProvider("secret").headers() == {"X-API-Key": "secret"}

    def test_rejects_empty_key(self) -> None:
        with pytest.raises(ValueError, match="api_key"):
            APIKeyProvider("")


class TestAPITokenProvider:
    def test_sends_bearer_header(self) -> None:
        assert APITokenProvider("emt_abc").headers() == {"Authorization": "Bearer emt_abc"}

    def test_rejects_empty_token(self) -> None:
        with pytest.raises(ValueError, match="token"):
            APITokenProvider("")


class TestOAuthProvider:
    def test_sends_bearer_header(self) -> None:
        assert OAuthProvider("jwt").headers() == {"Authorization": "Bearer jwt"}

    def test_update_token_replaces_credential(self) -> None:
        provider = OAuthProvider("old")
        provider.update_token("new")
        assert provider.headers() == {"Authorization": "Bearer new"}

    def test_rejects_empty_token(self) -> None:
        with pytest.raises(ValueError, match="access_token"):
            OAuthProvider("")


class TestMakeProvider:
    def test_emt_prefix_upgrades_to_bearer(self) -> None:
        assert isinstance(make_provider("apikey", api_key="emt_123"), APITokenProvider)

    def test_plain_key_uses_x_api_key(self) -> None:
        assert isinstance(make_provider("apikey", api_key="plain"), APIKeyProvider)

    def test_explicit_apitoken_mode(self) -> None:
        provider = make_provider("apitoken", api_key="anything")
        assert provider.headers() == {"Authorization": "Bearer anything"}

    def test_oauth_mode_requires_access_token(self) -> None:
        with pytest.raises(ValueError, match="access_token"):
            make_provider("oauth")
        assert make_provider("oauth", access_token="jwt").headers() == {
            "Authorization": "Bearer jwt"
        }

    @pytest.mark.parametrize("mode", ["apikey", "apitoken"])
    def test_key_modes_require_api_key(self, mode: str) -> None:
        with pytest.raises(ValueError, match="api_key"):
            make_provider(mode)

    def test_unknown_mode_is_rejected(self) -> None:
        with pytest.raises(ValueError, match="Invalid auth mode"):
            make_provider("magic", api_key="x")
