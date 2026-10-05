package mcpregistry

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

func TestRenderArguments(t *testing.T) {
	tests := []struct {
		name string
		args []RegistryArgument
		want []string
	}{
		{
			name: "named pair",
			args: []RegistryArgument{{Name: "foo", Value: "bar"}},
			want: []string{"--foo", "bar"},
		},
		{
			name: "positional",
			args: []RegistryArgument{{Value: "bar"}},
			want: []string{"bar"},
		},
		{
			name: "empty omitted",
			args: []RegistryArgument{{}},
			want: []string{},
		},
		{
			name: "default fallback",
			args: []RegistryArgument{{Name: "x", Default: "d"}},
			want: []string{"--x", "d"},
		},
		{
			name: "value wins over default",
			args: []RegistryArgument{{Name: "x", Value: "v", Default: "d"}},
			want: []string{"--x", "v"},
		},
		{
			name: "bare flag",
			args: []RegistryArgument{{Name: "verbose"}},
			want: []string{"--verbose"},
		},
		{
			name: "multiple in order",
			args: []RegistryArgument{
				{Name: "log-level", Value: "debug"},
				{Value: "serve"},
				{Name: "port", Default: "8080"},
			},
			want: []string{"--log-level", "debug", "serve", "--port", "8080"},
		},
		{
			name: "nil input",
			args: nil,
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderArguments(tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("renderArguments = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("renderArguments = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestRegistryServerToDTOArgsAndHeaders(t *testing.T) {
	s := RegistryServer{
		Name:    "io.github.x/y",
		Version: "1.0.0",
		Packages: []RegistryPackage{
			{
				RegistryType:     "npm",
				Name:             "y",
				Identifier:       "y",
				Transport:        RegistryTransport{Type: "stdio"},
				RuntimeArguments: []RegistryArgument{{Name: "log-level", Value: "debug"}},
				PackageArguments: []RegistryArgument{{Value: "serve"}},
			},
		},
		Remotes: []RegistryRemote{
			{
				Type: "streamable-http",
				URL:  "https://mcp.example.com/mcp",
				Headers: []RegistryHeader{
					{Name: "Authorization", Description: "Bearer", IsRequired: true, IsSecret: true, Value: "Bearer {api_key}"},
				},
			},
		},
	}

	dto := registryServerToDTO(s)

	if len(dto.Packages) != 1 {
		t.Fatalf("packages = %d, want 1", len(dto.Packages))
	}
	if got := dto.Packages[0].RuntimeArguments; len(got) != 2 || got[0] != "--log-level" || got[1] != "debug" {
		t.Errorf("RuntimeArguments = %v", got)
	}
	if got := dto.Packages[0].PackageArguments; len(got) != 1 || got[0] != "serve" {
		t.Errorf("PackageArguments = %v", got)
	}
	if len(dto.Headers) != 1 {
		t.Fatalf("headers = %d, want 1", len(dto.Headers))
	}
	h := dto.Headers[0]
	if h.Name != "Authorization" || h.Value != "Bearer {api_key}" || !h.IsRequired || !h.IsSecret {
		t.Errorf("header = %+v", h)
	}
}

func TestGetRegistryServerHandlerNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	svc := &Service{registryClient: NewRegistryClientWithBaseURL(ts.URL)}
	h := NewHandler(svc)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/mcp-registry/servers/io.github.github%2Fdoes-not-exist", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name")
	c.SetParamValues("io.github.github/does-not-exist")

	err := h.GetRegistryServer(c)
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperror.Error, got %T: %v", err, err)
	}
	if appErr.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want 404", appErr.HTTPStatus)
	}
}
