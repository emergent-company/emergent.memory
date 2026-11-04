package installer

import (
	"fmt"
	"strings"
	"testing"
)

type sandboxBuildCall struct {
	dockerfile string
	image      string
	gitToken   string
}

type fakeSandboxImageBuilder struct {
	calls []sandboxBuildCall
	fail  map[string]error
}

func (f *fakeSandboxImageBuilder) BuildSandboxImages(dockerfileContent, imageName, gitTokenEnv string) error {
	f.calls = append(f.calls, sandboxBuildCall{
		dockerfile: dockerfileContent,
		image:      imageName,
		gitToken:   gitTokenEnv,
	})
	return f.fail[imageName]
}

// sandboxRecordingOutput keeps the formatted messages so a test can assert what an
// operator actually sees.
type sandboxRecordingOutput struct {
	successes []string
	warns     []string
}

func (r *sandboxRecordingOutput) Info(format string, args ...interface{}) {}
func (r *sandboxRecordingOutput) Success(format string, args ...interface{}) {
	r.successes = append(r.successes, fmt.Sprintf(format, args...))
}
func (r *sandboxRecordingOutput) Warn(format string, args ...interface{}) {
	r.warns = append(r.warns, fmt.Sprintf(format, args...))
}
func (r *sandboxRecordingOutput) Error(format string, args ...interface{}) {}
func (r *sandboxRecordingOutput) Step(format string, args ...interface{})  {}

// TestBuildSandboxImagesPassesGitTokenToEveryImage pins #2289: the token env
// var name must reach all three builds, not only the workspace base image.
// The SDK Dockerfiles clone the same repository, so a build without the
// token fails while the base image succeeds.
func TestBuildSandboxImagesPassesGitTokenToEveryImage(t *testing.T) {
	builder := &fakeSandboxImageBuilder{}
	out := &sandboxRecordingOutput{}

	buildSandboxImages(builder, "GH_TOKEN", out)

	if len(builder.calls) != 3 {
		t.Fatalf("expected 3 build calls, got %d: %+v", len(builder.calls), builder.calls)
	}

	want := []struct {
		dockerfile string
		image      string
	}{
		{GetWorkspaceBaseDockerfile(), WorkspaceBaseImage},
		{GetPythonSDKDockerfile(), "emergent-memory-python-sdk:latest"},
		{GetGoSDKDockerfile(), "emergent-memory-go-sdk:latest"},
	}
	for i, w := range want {
		call := builder.calls[i]
		if call.image != w.image {
			t.Errorf("build %d: image = %q, want %q", i, call.image, w.image)
		}
		if call.dockerfile != w.dockerfile {
			t.Errorf("build %d (%s): dockerfile content does not match the template", i, w.image)
		}
		if call.gitToken != "GH_TOKEN" {
			t.Errorf("build %d (%s): gitTokenEnv = %q, want %q", i, w.image, call.gitToken, "GH_TOKEN")
		}
	}
	if len(out.successes) != 3 {
		t.Errorf("expected 3 success messages, got %v", out.successes)
	}
}

// TestBuildSandboxImagesPassesEmptyTokenUnchanged makes sure the pass-through
// is not a hardcoded env var name: with no token in the environment the build
// must run unauthenticated (and still be attempted).
func TestBuildSandboxImagesPassesEmptyTokenUnchanged(t *testing.T) {
	builder := &fakeSandboxImageBuilder{}
	out := &sandboxRecordingOutput{}

	buildSandboxImages(builder, "", out)

	if len(builder.calls) != 3 {
		t.Fatalf("expected 3 build calls, got %d", len(builder.calls))
	}
	for i, call := range builder.calls {
		if call.gitToken != "" {
			t.Errorf("build %d (%s): gitTokenEnv = %q, want empty", i, call.image, call.gitToken)
		}
	}
}

// TestBuildSandboxImagesFailureNamesWhatWillNotWork covers the non-fatal
// decision in #2289: the warning must say which base_image stops working, not
// just echo the docker error.
func TestBuildSandboxImagesFailureNamesWhatWillNotWork(t *testing.T) {
	builder := &fakeSandboxImageBuilder{
		fail: map[string]error{
			"emergent-memory-python-sdk:latest": fmt.Errorf("clone failed"),
		},
	}
	out := &sandboxRecordingOutput{}

	buildSandboxImages(builder, "GH_TOKEN", out)

	if len(out.warns) != 1 {
		t.Fatalf("expected 1 warning, got %v", out.warns)
	}
	warn := out.warns[0]
	for _, want := range []string{"Python SDK", "non-fatal", "clone failed", `base_image "emergent-memory-python-sdk:latest"`} {
		if !strings.Contains(warn, want) {
			t.Errorf("warning %q does not contain %q", warn, want)
		}
	}
	if len(out.successes) != 2 {
		t.Errorf("expected the other two builds to succeed, got %v", out.successes)
	}
}

// TestGitTokenSecretEnv pins the token env var precedence the four build call
// sites share: GH_TOKEN wins over GITHUB_TOKEN, and neither means no secret.
func TestGitTokenSecretEnv(t *testing.T) {
	t.Run("GH_TOKEN wins", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "gh")
		t.Setenv("GITHUB_TOKEN", "github")
		if got := gitTokenSecretEnv(); got != "GH_TOKEN" {
			t.Errorf("gitTokenSecretEnv() = %q, want GH_TOKEN", got)
		}
	})

	t.Run("GITHUB_TOKEN fallback", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "github")
		if got := gitTokenSecretEnv(); got != "GITHUB_TOKEN" {
			t.Errorf("gitTokenSecretEnv() = %q, want GITHUB_TOKEN", got)
		}
	})

	t.Run("no token", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "")
		if got := gitTokenSecretEnv(); got != "" {
			t.Errorf("gitTokenSecretEnv() = %q, want empty", got)
		}
	})
}
