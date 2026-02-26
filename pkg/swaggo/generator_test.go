package swaggo

import (
	"testing"
)

func TestNew(t *testing.T) {
	gen := New()

	if gen.Title != "API Documentation" {
		t.Errorf("expected default title 'API Documentation', got '%s'", gen.Title)
	}
	if gen.Version != "1.0.0" {
		t.Errorf("expected default version '1.0.0', got '%s'", gen.Version)
	}
	if gen.BasePath != "/" {
		t.Errorf("expected default basePath '/', got '%s'", gen.BasePath)
	}
}

func TestGeneratorChaining(t *testing.T) {
	gen := New().
		WithTitle("Test API").
		WithDescription("Test Description").
		WithVersion("2.0.0").
		WithHost("localhost:8080").
		WithBasePath("/api/v1").
		WithExclude("vendor", "test")

	if gen.Title != "Test API" {
		t.Errorf("expected title 'Test API', got '%s'", gen.Title)
	}
	if gen.Description != "Test Description" {
		t.Errorf("expected description 'Test Description', got '%s'", gen.Description)
	}
	if gen.Version != "2.0.0" {
		t.Errorf("expected version '2.0.0', got '%s'", gen.Version)
	}
	if gen.Host != "localhost:8080" {
		t.Errorf("expected host 'localhost:8080', got '%s'", gen.Host)
	}
	if gen.BasePath != "/api/v1" {
		t.Errorf("expected basePath '/api/v1', got '%s'", gen.BasePath)
	}
	if len(gen.excludeDirs) != 2 {
		t.Errorf("expected 2 exclude dirs, got %d", len(gen.excludeDirs))
	}
}

func TestGeneratorWithEntry(t *testing.T) {
	gen := New().
		WithProjectRoot("/tmp/project").
		WithEntry("cmd/main.go")

	if gen.projectRoot != "/tmp/project" {
		t.Errorf("expected projectRoot '/tmp/project', got '%s'", gen.projectRoot)
	}
	if gen.entryFile != "cmd/main.go" {
		t.Errorf("expected entryFile 'cmd/main.go', got '%s'", gen.entryFile)
	}
}

func TestGenerateEmptySpec(t *testing.T) {
	gen := New().
		WithTitle("Empty API").
		WithVersion("1.0.0")

	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spec.OpenAPI != "3.0.3" {
		t.Errorf("expected OpenAPI '3.0.3', got '%s'", spec.OpenAPI)
	}
	if spec.Info.Title != "Empty API" {
		t.Errorf("expected title 'Empty API', got '%s'", spec.Info.Title)
	}
	if spec.Info.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got '%s'", spec.Info.Version)
	}
	if len(spec.Paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(spec.Paths))
	}
}

func TestSpecToJSON(t *testing.T) {
	gen := New().WithTitle("JSON Test")
	spec, _ := gen.Generate()

	data, err := spec.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON error: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty JSON output")
	}
}

func TestSpecToYAML(t *testing.T) {
	gen := New().WithTitle("YAML Test")
	spec, _ := gen.Generate()

	data, err := spec.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML error: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty YAML output")
	}
}
