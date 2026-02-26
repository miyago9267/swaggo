package swaggo

import (
	"testing"
)

func TestNewParser(t *testing.T) {
	p := NewParser()

	if p.fset == nil {
		t.Error("expected fset to be initialized")
	}
	if p.Handlers == nil {
		t.Error("expected Handlers map to be initialized")
	}
	if p.Types == nil {
		t.Error("expected Types map to be initialized")
	}
	if p.controllerInstances == nil {
		t.Error("expected controllerInstances map to be initialized")
	}
}

func TestParseNonExistentDir(t *testing.T) {
	p := NewParser()
	err := p.ParseDir("/nonexistent/path/that/should/not/exist")

	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}

func TestAnalyzeEmpty(t *testing.T) {
	p := NewParser()
	err := p.Analyze()

	if err != nil {
		t.Errorf("unexpected error analyzing empty parser: %v", err)
	}
	if len(p.Routes) != 0 {
		t.Errorf("expected 0 routes, got %d", len(p.Routes))
	}
}
