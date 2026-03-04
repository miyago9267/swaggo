package swaggo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestGetGinParamType_PointerTypes(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected string
	}{
		{
			name:     "*gin.RouterGroup",
			src:      `package x; func f(r *gin.RouterGroup) {}`,
			expected: "RouterGroup",
		},
		{
			name:     "*gin.Engine",
			src:      `package x; func f(r *gin.Engine) {}`,
			expected: "Engine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			paramType := extractParamType(t, p, tt.src)
			if paramType != tt.expected {
				t.Errorf("getGinParamType() = %q, want %q", paramType, tt.expected)
			}
		})
	}
}

func TestGetGinParamType_InterfaceTypes(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected string
	}{
		{
			name:     "gin.IRouter",
			src:      `package x; func f(r gin.IRouter) {}`,
			expected: "IRouter",
		},
		{
			name:     "gin.IRoutes",
			src:      `package x; func f(r gin.IRoutes) {}`,
			expected: "IRoutes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			paramType := extractParamType(t, p, tt.src)
			if paramType != tt.expected {
				t.Errorf("getGinParamType() = %q, want %q", paramType, tt.expected)
			}
		})
	}
}

func TestGetGinParamType_Unsupported(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "string param",
			src:  `package x; func f(s string) {}`,
		},
		{
			name: "*http.Server",
			src:  `package x; func f(s *http.Server) {}`,
		},
		{
			name: "gin.Context (not a router type)",
			src:  `package x; func f(c gin.Context) {}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			paramType := extractParamType(t, p, tt.src)
			if paramType != "" {
				t.Errorf("getGinParamType() = %q, want empty", paramType)
			}
		})
	}
}

func TestCollectRouteRegistrars_InterfaceParam(t *testing.T) {
	src := `package handlers

import "github.com/gin-gonic/gin"

func RegisterUserRoutes(r gin.IRouter) {
	r.GET("/users", ListUsers)
	r.POST("/users", CreateUser)
}
`
	p := NewParser()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "handlers.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	p.files = append(p.files, file)

	registrars := p.collectRouteRegistrars()
	if len(registrars) == 0 {
		t.Fatal("expected to find registrar with gin.IRouter param, got 0")
	}

	found := false
	for _, reg := range registrars {
		if reg.Name == "RegisterUserRoutes" && reg.ParamType == "IRouter" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected registrar RegisterUserRoutes with ParamType=IRouter")
	}
}

func TestHandleMethodParsing(t *testing.T) {
	src := `package main

import "github.com/gin-gonic/gin"

func SetupRoutes() {
	r := gin.Default()

	r.Handle("GET", "/items", ListItems)
	r.Handle("POST", "/items", CreateItem)
	r.Handle("DELETE", "/items/:id", DeleteItem)

	r.GET("/health", HealthCheck)
}

func ListItems(c *gin.Context)   {}
func CreateItem(c *gin.Context)  {}
func DeleteItem(c *gin.Context)  {}
func HealthCheck(c *gin.Context) {}
`
	p := NewParser()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	p.fset = fset
	p.files = append(p.files, file)

	if err := p.Analyze(); err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	expected := map[string]string{
		"GET:/items":     "main.ListItems",
		"POST:/items":    "main.CreateItem",
		"DELETE:/items/:id": "main.DeleteItem",
		"GET:/health":    "main.HealthCheck",
	}

	found := make(map[string]bool)
	for _, route := range p.Routes {
		key := route.Method + ":" + route.Path
		found[key] = true

		if expectedHandler, ok := expected[key]; ok {
			if route.HandlerName != expectedHandler {
				t.Errorf("route %s: handler = %q, want %q", key, route.HandlerName, expectedHandler)
			}
		}
	}

	for key := range expected {
		if !found[key] {
			t.Errorf("expected route %s not found", key)
		}
	}
}

func TestHandleMethodInRegistrar(t *testing.T) {
	src := `package api

import "github.com/gin-gonic/gin"

func RegisterRoutes(r *gin.RouterGroup) {
	r.Handle("PUT", "/products/:id", UpdateProduct)
}

func UpdateProduct(c *gin.Context) {}
`
	p := NewParser()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "api.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	p.fset = fset
	p.files = append(p.files, file)

	// 模擬 registrar 被呼叫時帶 prefix
	p.routeRegistrars = p.collectRouteRegistrars()
	for _, reg := range p.routeRegistrars {
		p.extractRoutesWithPrefix(reg, "/api/v1")
	}

	var found bool
	for _, route := range p.Routes {
		if route.Method == "PUT" && route.Path == "/api/v1/products/:id" {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected route PUT /api/v1/products/:id not found")
		for _, route := range p.Routes {
			t.Logf("  found: %s %s -> %s", route.Method, route.Path, route.HandlerName)
		}
	}
}

func TestForRangeWithHandle(t *testing.T) {
	src := `package main

import "github.com/gin-gonic/gin"

type Route struct {
	Method  string
	Path    string
	Handler gin.HandlerFunc
}

func SetupRoutes() {
	r := gin.Default()

	routes := []Route{
		{Method: "GET",    Path: "/users",     Handler: ListUsers},
		{Method: "POST",   Path: "/users",     Handler: CreateUser},
		{Method: "DELETE", Path: "/users/:id", Handler: DeleteUser},
	}

	for _, route := range routes {
		r.Handle(route.Method, route.Path, route.Handler)
	}
}

func ListUsers(c *gin.Context)   {}
func CreateUser(c *gin.Context)  {}
func DeleteUser(c *gin.Context)  {}
`
	p := NewParser()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	p.fset = fset
	p.files = append(p.files, file)

	if err := p.Analyze(); err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	expected := map[string]string{
		"GET:/users":        "main.ListUsers",
		"POST:/users":       "main.CreateUser",
		"DELETE:/users/:id": "main.DeleteUser",
	}

	found := make(map[string]bool)
	for _, route := range p.Routes {
		key := route.Method + ":" + route.Path
		found[key] = true

		if expectedHandler, ok := expected[key]; ok {
			if route.HandlerName != expectedHandler {
				t.Errorf("route %s: handler = %q, want %q", key, route.HandlerName, expectedHandler)
			}
		}
	}

	for key := range expected {
		if !found[key] {
			t.Errorf("expected route %s not found", key)
		}
	}
}

func TestForRangeWithGroupPrefix(t *testing.T) {
	src := `package main

import "github.com/gin-gonic/gin"

type Route struct {
	Method  string
	Path    string
	Handler gin.HandlerFunc
}

func SetupRoutes() {
	r := gin.Default()
	api := r.Group("/api/v1")

	routes := []Route{
		{Method: "GET",  Path: "/orders",     Handler: ListOrders},
		{Method: "POST", Path: "/orders",     Handler: CreateOrder},
	}

	for _, route := range routes {
		api.Handle(route.Method, route.Path, route.Handler)
	}
}

func ListOrders(c *gin.Context)  {}
func CreateOrder(c *gin.Context) {}
`
	p := NewParser()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	p.fset = fset
	p.files = append(p.files, file)

	if err := p.Analyze(); err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	expected := map[string]string{
		"GET:/api/v1/orders":  "main.ListOrders",
		"POST:/api/v1/orders": "main.CreateOrder",
	}

	found := make(map[string]bool)
	for _, route := range p.Routes {
		key := route.Method + ":" + route.Path
		found[key] = true
	}

	for key := range expected {
		if !found[key] {
			t.Errorf("expected route %s not found", key)
			for _, route := range p.Routes {
				t.Logf("  found: %s %s -> %s", route.Method, route.Path, route.HandlerName)
			}
		}
	}
}

func TestForRangeWithPackageLevelVar(t *testing.T) {
	src := `package main

import "github.com/gin-gonic/gin"

type Route struct {
	Method  string
	Path    string
	Handler gin.HandlerFunc
}

var routes = []Route{
	{Method: "GET",  Path: "/health", Handler: HealthCheck},
	{Method: "GET",  Path: "/ready",  Handler: ReadyCheck},
}

func SetupRoutes() {
	r := gin.Default()
	for _, route := range routes {
		r.Handle(route.Method, route.Path, route.Handler)
	}
}

func HealthCheck(c *gin.Context) {}
func ReadyCheck(c *gin.Context)  {}
`
	p := NewParser()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	p.fset = fset
	p.files = append(p.files, file)

	if err := p.Analyze(); err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	expected := []string{"GET:/health", "GET:/ready"}

	found := make(map[string]bool)
	for _, route := range p.Routes {
		found[route.Method+":"+route.Path] = true
	}

	for _, key := range expected {
		if !found[key] {
			t.Errorf("expected route %s not found", key)
		}
	}
}

// extractParamType 是測試 helper，解析 src 中第一個函數的第一個參數型別
func extractParamType(t *testing.T, p *Parser, src string) string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type.Params == nil {
			continue
		}
		for _, param := range fn.Type.Params.List {
			result := p.getGinParamType(param.Type)
			return result
		}
	}
	t.Fatal("no function param found in source")
	return ""
}
