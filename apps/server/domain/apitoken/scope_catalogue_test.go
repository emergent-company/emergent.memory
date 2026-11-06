package apitoken

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// fieldValidateTag returns the `validate` struct tag of v's named field, or ""
// when the type has no such field.
func fieldValidateTag(v any, field string) string {
	f, ok := reflect.TypeOf(v).FieldByName(field)
	if !ok {
		return ""
	}
	return f.Tag.Get("validate")
}

// TestUserMintableScopes pins the two-catalogue split: the stored vocabulary
// keeps the retired umbrellas and the reserved markers, and the user-mintable
// subset is exactly the stored list minus those six.
func TestUserMintableScopes(t *testing.T) {
	stored := make(map[string]bool, len(ValidApiTokenScopes))
	for _, scope := range ValidApiTokenScopes {
		require.False(t, stored[scope], "ValidApiTokenScopes lists %q twice", scope)
		stored[scope] = true
	}
	for _, retired := range []string{"data:read", "data:write", "device:api", "webhook:trigger", "mcp:agent-call", "share:agent-chat"} {
		require.True(t, stored[retired], "the stored catalogue must keep %q so existing credentials keep their access", retired)
		require.True(t, nonMintableScopes[retired], "%q must be non-mintable", retired)
	}

	mintable := make(map[string]bool, len(UserMintableScopes))
	for _, scope := range UserMintableScopes {
		require.False(t, nonMintableScopes[scope], "UserMintableScopes leaked non-mintable %q", scope)
		require.True(t, stored[scope], "UserMintableScopes contains %q which is not a stored scope", scope)
		mintable[scope] = true
	}

	require.Len(t, UserMintableScopes, len(ValidApiTokenScopes)-len(nonMintableScopes),
		"the mintable catalogue must be derived from the stored one, not hand-listed")
	for _, scope := range []string{"documents:read", "documents:write", "search", "graph:read", "journal:read"} {
		require.True(t, mintable[scope], "%q must be mintable — it is the fine-grained replacement for the retired umbrellas", scope)
	}
}

// TestValidateTokenScopes pins the rejection shape: the first offending scope is
// named and the error is a 400, whichever catalogue is used.
func TestValidateTokenScopes(t *testing.T) {
	require.NoError(t, validateTokenScopes([]string{"documents:read", "search"}, UserMintableScopes))
	require.NoError(t, validateTokenScopes([]string{"schema:read", "data:read"}, ValidApiTokenScopes),
		"the stored catalogue must still accept the retired umbrella")

	for _, tc := range []struct {
		name    string
		scopes  []string
		allowed []string
		want    string
	}{
		{"data:read is not mintable", []string{"documents:read", "data:read"}, UserMintableScopes, "invalid scope: data:read"},
		{"data:write is not mintable", []string{"data:write"}, UserMintableScopes, "invalid scope: data:write"},
		{"a marker is not mintable", []string{"device:api"}, UserMintableScopes, "invalid scope: device:api"},
		{"an unknown scope is rejected against the stored catalogue too", []string{"made:up"}, ValidApiTokenScopes, "invalid scope: made:up"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTokenScopes(tc.scopes, tc.allowed)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestRequestScopeTagsExcludeRetiredUmbrellas pins the second rejection layer:
// the HTTP bind tags must not list the retired umbrellas (or any marker), so a
// request is rejected at binding time as well as in the service. The tags are
// strings, so this asserts on the DTO types the handlers bind into.
func TestRequestScopeTagsExcludeRetiredUmbrellas(t *testing.T) {
	for _, tag := range []string{
		fieldValidateTag(CreateApiTokenRequest{}, "Scopes"),
		fieldValidateTag(CreateAccountTokenRequest{}, "Scopes"),
		fieldValidateTag(UpdateApiTokenScopesRequest{}, "Scopes"),
	} {
		require.NotEmpty(t, tag, "the Scopes field must carry a validate tag")
		for _, retired := range []string{"data:read", "data:write", "device:api", "webhook:trigger", "mcp:agent-call", "share:agent-chat"} {
			require.NotContains(t, tag, retired, "the oneof tag must not offer %q", retired)
		}
		for _, mintable := range []string{"documents:read", "documents:write", "search", "journal:read"} {
			require.Contains(t, tag, mintable, "the oneof tag must offer %q", mintable)
		}
	}
}

// TestUserMintableScopesCoverRetiredUmbrellaImplications pins the #80
// invariant: every scope the retired data:read/data:write umbrellas imply must
// itself be mintable. The implied sets come from pkg/auth.ScopeImplies (the
// single canonical relation), so a future addition there fails this test until
// the catalogue catches up. Before #80 the missing scopes made a newly minted
// token unable to reach the REST routes those umbrellas used to cover.
func TestUserMintableScopesCoverRetiredUmbrellaImplications(t *testing.T) {
	mintable := make(map[string]bool, len(UserMintableScopes))
	for _, scope := range UserMintableScopes {
		mintable[scope] = true
	}

	for _, umbrella := range []string{"data:read", "data:write"} {
		implied := auth.ScopeImplies[umbrella]
		require.NotEmpty(t, implied, "ScopeImplies[%q] must not be empty", umbrella)
		for _, scope := range implied {
			require.True(t, mintable[scope],
				"%q is implied by the retired umbrella %q but is not mintable; a token minted after #69 cannot reach its route (#80)", scope, umbrella)
		}
	}
}

// routeScopeExemptions names a scope that a production route requires but that
// is deliberately absent from UserMintableScopes. Every entry needs a reason;
// the test also fails when an entry stops being route-required or becomes
// mintable, so the list cannot rot.
var routeScopeExemptions = map[string]string{
	"chat:admin":      "not mintable directly; the mintable agents:write umbrella grants it through ScopeImplies",
	"org:read":        "internal org-administration scope, deliberately outside the project token vocabulary",
	"discovery:read":  "separate discovery scope family, outside the retired data:* implication closure tracked by #80",
	"discovery:write": "separate discovery scope family, outside the retired data:* implication closure tracked by #80",
}

// TestRouteRequiredScopesAreMintable scans the production route registrations
// for RequireAPITokenScopes(...) literals and asserts each required scope is
// mintable, so a route can never be added (or kept) that no freshly minted user
// token can reach. The inventory is derived from the code, not a hand-copied
// list, and the explicit exemptions above document the only intentional gaps.
func TestRouteRequiredScopesAreMintable(t *testing.T) {
	required := routeRequiredScopes(t)
	require.NotEmpty(t, required, "no RequireAPITokenScopes literals found; the AST scan must be broken")

	mintable := make(map[string]bool, len(UserMintableScopes))
	for _, scope := range UserMintableScopes {
		mintable[scope] = true
	}

	for scope := range required {
		if mintable[scope] {
			continue
		}
		reason, exempt := routeScopeExemptions[scope]
		if !exempt {
			t.Errorf("route requires %q but it is not mintable; add it to UserMintableScopes or to routeScopeExemptions with a reason (#80)", scope)
			continue
		}
		require.NotEmpty(t, reason, "exemption %q needs a reason", scope)
	}
	for scope, reason := range routeScopeExemptions {
		require.NotEmpty(t, reason, "exemption %q needs a reason", scope)
		require.True(t, required[scope], "exemption %q no longer guards a route; drop it", scope)
		require.False(t, mintable[scope], "exemption %q is now mintable; drop it", scope)
	}
}

// routeRequiredScopes parses every non-test Go file under apps/server/domain and
// collects the literal string arguments of RequireAPITokenScopes calls.
// Non-literal arguments (helper wrappers) are skipped: they cannot be resolved
// statically, and every production gate today uses a literal.
func routeRequiredScopes(t *testing.T) map[string]bool {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must resolve the test file")
	domainDir := filepath.Dir(filepath.Dir(thisFile))

	fset := token.NewFileSet()
	out := map[string]bool{}
	err := filepath.WalkDir(domainDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "RequireAPITokenScopes" {
				return true
			}
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				scope, uerr := strconv.Unquote(lit.Value)
				if uerr == nil {
					out[scope] = true
				}
			}
			return true
		})
		return nil
	})
	require.NoError(t, err, "scanning %s", domainDir)
	return out
}
