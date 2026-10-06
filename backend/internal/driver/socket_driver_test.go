package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathTranslation(t *testing.T) {
	d := &SocketDriver{
		mounts: []hostMount{
			{
				Source:      "/home/farmers00/docker",
				Destination: "/root/docker",
			},
		},
		mountsInit: true,
	}

	ctx := context.Background()

	tests := []struct {
		name              string
		input             string
		expectedHost      string
		expectedContainer string
	}{
		{
			name:              "Container path to host",
			input:             "/root/docker/myapp",
			expectedHost:      "/home/farmers00/docker/myapp",
			expectedContainer: "/root/docker/myapp",
		},
		{
			name:              "Host path to container",
			input:             "/home/farmers00/docker/myapp",
			expectedHost:      "/home/farmers00/docker/myapp",
			expectedContainer: "/root/docker/myapp",
		},
		{
			name:              "Base container dir",
			input:             "/root/docker",
			expectedHost:      "/home/farmers00/docker",
			expectedContainer: "/root/docker",
		},
		{
			name:              "Base host dir",
			input:             "/home/farmers00/docker",
			expectedHost:      "/home/farmers00/docker",
			expectedContainer: "/root/docker",
		},
		{
			name:              "Nested subdir in container",
			input:             "/root/docker/stacks/web/v1",
			expectedHost:      "/home/farmers00/docker/stacks/web/v1",
			expectedContainer: "/root/docker/stacks/web/v1",
		},
		{
			name:              "Unrelated path",
			input:             "/etc/nginx",
			expectedHost:      "/etc/nginx",
			expectedContainer: "/etc/nginx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHost := d.ToHostPath(ctx, tt.input)
			if gotHost != tt.expectedHost {
				t.Errorf("ToHostPath(%q) = %q; want %q", tt.input, gotHost, tt.expectedHost)
			}

			gotContainer := d.ToContainerPath(ctx, tt.input)
			if gotContainer != tt.expectedContainer {
				t.Errorf("ToContainerPath(%q) = %q; want %q", tt.input, gotContainer, tt.expectedContainer)
			}
		})
	}
}

func TestSanitizeCompose(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantMod     bool
		wantOld     string
		wantNew     string
		containsStr string
	}{
		{
			name:        "Uppercase top-level name",
			input:       "name: Ollama\nservices:\n  ollama:\n    image: ollama/ollama\n",
			wantMod:     true,
			wantOld:     "Ollama",
			wantNew:     "ollama",
			containsStr: "name: ollama\n",
		},
		{
			name:        "User case - name with space without quotes",
			input:       "name: ollama stack\nservices:\n  open-webui:\n    image: ghcr.io/open-webui/open-webui:cuda\n",
			wantMod:     true,
			wantOld:     "ollama stack",
			wantNew:     "ollama-stack",
			containsStr: "name: ollama-stack\n",
		},
		{
			name:        "Name with space and quotes",
			input:       "name: \"Ollama LLM\" # my server\nservices:\n  web:\n    image: nginx\n",
			wantMod:     true,
			wantOld:     "Ollama LLM",
			wantNew:     "ollama-llm",
			containsStr: "name: ollama-llm # my server\n",
		},
		{
			name:        "Already valid name",
			input:       "name: ollama\nservices:\n  ollama:\n    image: ollama/ollama\n",
			wantMod:     false,
			wantOld:     "",
			wantNew:     "",
			containsStr: "name: ollama\n",
		},
		{
			name:        "No top-level name",
			input:       "version: '3.8'\nservices:\n  ollama:\n    name: test\n",
			wantMod:     false,
			wantOld:     "",
			wantNew:     "",
			containsStr: "version: '3.8'\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newContent, mod, oldN, newN := sanitizeComposeContent(tt.input)
			if mod != tt.wantMod {
				t.Errorf("sanitizeComposeContent() modified = %v; want %v", mod, tt.wantMod)
			}
			if oldN != tt.wantOld {
				t.Errorf("sanitizeComposeContent() oldName = %q; want %q", oldN, tt.wantOld)
			}
			if newN != tt.wantNew {
				t.Errorf("sanitizeComposeContent() newName = %q; want %q", newN, tt.wantNew)
			}
			if !strings.Contains(newContent, tt.containsStr) {
				t.Errorf("sanitizeComposeContent() result %q does not contain %q", newContent, tt.containsStr)
			}
		})
	}
}

func TestGetComposeProjectName(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Compose file with name: ollama-stack
	f1 := filepath.Join(tmpDir, "compose1.yml")
	_ = os.WriteFile(f1, []byte("name: ollama-stack\nservices:\n  app:\n    image: test\n"), 0644)
	if got := getComposeProjectName(f1, "/home/user/docker/ollama"); got != "ollama-stack" {
		t.Errorf("getComposeProjectName() = %q; want 'ollama-stack'", got)
	}

	// 2. Compose file with name: ollama stack (unquoted space)
	f2 := filepath.Join(tmpDir, "compose2.yml")
	_ = os.WriteFile(f2, []byte("name: ollama stack\nservices:\n  app:\n    image: test\n"), 0644)
	if got := getComposeProjectName(f2, "/home/user/docker/ollama"); got != "ollama-stack" {
		t.Errorf("getComposeProjectName() = %q; want 'ollama-stack'", got)
	}

	// 3. Compose file without name:
	f3 := filepath.Join(tmpDir, "compose3.yml")
	_ = os.WriteFile(f3, []byte("services:\n  app:\n    image: test\n"), 0644)
	if got := getComposeProjectName(f3, "/home/user/docker/my-app"); got != "my-app" {
		t.Errorf("getComposeProjectName() = %q; want 'my-app'", got)
	}

	// 4. Non-existent compose file falls back to host path base
	if got := getComposeProjectName("", "/home/user/docker/web_server"); got != "web_server" {
		t.Errorf("getComposeProjectName() = %q; want 'web_server'", got)
	}
}

func TestParseEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	content := `
# Configuration file
PORT=8081
DATA_DIR=/custom/data # inline comment
export JWT_SECRET="super-secret-key"
SINGLE_QUOTED='single-quoted'
EMPTY_VAL=
`
	if err := os.WriteFile(envPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test .env file: %v", err)
	}

	envMap := parseEnvFile(envPath)
	if envMap["PORT"] != "8081" {
		t.Errorf("expected PORT=8081, got %q", envMap["PORT"])
	}
	if envMap["DATA_DIR"] != "/custom/data" {
		t.Errorf("expected DATA_DIR=/custom/data, got %q", envMap["DATA_DIR"])
	}
	if envMap["JWT_SECRET"] != "super-secret-key" {
		t.Errorf("expected JWT_SECRET=super-secret-key, got %q", envMap["JWT_SECRET"])
	}
	if envMap["SINGLE_QUOTED"] != "single-quoted" {
		t.Errorf("expected SINGLE_QUOTED=single-quoted, got %q", envMap["SINGLE_QUOTED"])
	}
	if _, ok := envMap["EMPTY_VAL"]; !ok {
		t.Errorf("expected EMPTY_VAL to be present in map")
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("simple"); got != "'simple'" {
		t.Errorf("expected 'simple', got %s", got)
	}
	if got := shellQuote("with spaces"); got != "'with spaces'" {
		t.Errorf("expected 'with spaces', got %s", got)
	}
	if got := shellQuote("it's cool"); got != "'it'\\''s cool'" {
		t.Errorf("expected 'it'\\''s cool', got %s", got)
	}
}

func TestBuildComposeEnv(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")
	_ = os.WriteFile(envPath, []byte("PORT=8081\nFOO=bar\n"), 0644)

	// Simulate DockerPulse internal process env
	t.Setenv("PORT", "8080")
	t.Setenv("DATA_DIR", "/data")
	t.Setenv("JWT_SECRET", "internal_secret")
	t.Setenv("OTHER_VAR", "keep_me")

	cleanEnv := buildComposeEnv(tmpDir)

	envMap := make(map[string]string)
	for _, e := range cleanEnv {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Internal vars should be stripped if not in .env
	if _, ok := envMap["DATA_DIR"]; ok {
		t.Errorf("DATA_DIR should have been stripped from compose env")
	}
	if _, ok := envMap["JWT_SECRET"]; ok {
		t.Errorf("JWT_SECRET should have been stripped from compose env")
	}

	// Non-internal env vars should be retained
	if envMap["OTHER_VAR"] != "keep_me" {
		t.Errorf("expected OTHER_VAR=keep_me, got %q", envMap["OTHER_VAR"])
	}

	// User-defined variables in .env should override internal vars
	if envMap["PORT"] != "8081" {
		t.Errorf("expected PORT=8081 from .env, got %q", envMap["PORT"])
	}
	if envMap["FOO"] != "bar" {
		t.Errorf("expected FOO=bar from .env, got %q", envMap["FOO"])
	}
}


