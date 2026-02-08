# swaggo

![swaggo](https://raw.githubusercontent.com/miyago9267/swaggo/main/assets/swaggo.svg)

🌍 *[English](README_EN.md) ∙ [繁體中文](../README.md)*

[![Go Reference](https://pkg.go.dev/badge/github.com/miyago9267/swaggo.svg)](https://pkg.go.dev/github.com/miyago9267/swaggo)
[![Go Report Card](https://goreportcard.com/badge/github.com/miyago9267/swaggo)](https://goreportcard.com/report/github.com/miyago9267/swaggo)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Release](https://img.shields.io/github/release/miyago9267/swaggo.svg?style=flat-square)](https://github.com/miyago9267/swaggo/releases)

Automatically generate [OpenAPI 3.0](https://swagger.io/specification/) documentation from [Gin](https://github.com/gin-gonic/gin) handlers using Go AST parsing. No annotations required.

## Contents

- [Getting Started](#getting-started)
- [CLI Options](#cli-options)
- [How It Works](#how-it-works)
  - [Route Detection](#route-detection)
  - [Parameter Detection](#parameter-detection)
  - [Request Body Detection](#request-body-detection)
  - [Response Detection](#response-detection)
  - [Struct Tag Support](#struct-tag-support)
- [Comment Convention](#comment-convention)
- [Programmatic Usage](#programmatic-usage)
- [Swagger UI Integration](#swagger-ui-integration)
- [Limitations](#limitations)
- [Examples](EXAMPLE.md)
- [License](#license)

## Getting Started

1. Install swaggo:

```bash
go install github.com/miyago9267/swaggo/cmd/swaggo@latest
```

1. Run in your project root:

```bash
swaggo -dir . -title "My API"
```

1. Output:

```text
swaggo dev
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Parsing: /path/to/your/project
Found 15 routes
Found 15 handlers
Found 9 type definitions
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Generated docs/openapi.json (17566 bytes)
Generated docs/openapi.yaml (15369 bytes)
Generated docs/index.html
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Done! 11 endpoints generated

Endpoints:
  GET    /health
  GET    /api/v1/products
  POST   /api/v1/products
  ...
```

1. Open `docs/index.html` in your browser to view Swagger UI.

## CLI Options

```text
swaggo [flags]

Flags:
  -d, -dir string           Directory to parse (default ".")
  -o, -output string        Output directory (default "docs")
  -t, -title string         API title (default "API Documentation")
  -desc string              API description
  -version string           API version (default "1.0.0")
  -host string              API host (e.g. localhost:8080)
  -basePath string          API base path (default "/")
  -format string            Output format: json, yaml, both (default "both")
  -ui                       Generate Swagger UI HTML (default true)
  -exclude string           Directories to exclude (comma separated)
  -parseVendor              Parse vendor directory (default false)
  -parseDependency          Parse external dependencies (default false)
  -q, -quiet                Quiet mode, only output errors
  -v                        Show version
```

### Examples

```bash
# Basic usage
swaggo -dir ./cmd/server -title "My API"

# Custom output directory
swaggo -d . -o ./api/docs -format json

# With host and base path
swaggo -dir . -host localhost:8080 -basePath /api/v1

# Exclude directories
swaggo -dir . -exclude "test,mock,internal"

# Quiet mode (CI/CD)
swaggo -d . -q
```

## How It Works

swaggo parses your Go source code using the `go/ast` package and automatically detects API definitions without requiring any annotations.

### Route Detection

Detects all Gin route registrations:

```go
r := gin.Default()
r.GET("/health", HealthCheck)
r.POST("/users", CreateUser)

// Route groups with prefix
api := r.Group("/api/v1")
api.GET("/products", ListProducts)      // → /api/v1/products
api.GET("/products/:id", GetProduct)    // → /api/v1/products/{id}
```

Supported methods: `GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `OPTIONS`, `HEAD`

### Parameter Detection

| Gin Method | OpenAPI Location | Example |
| ---------- | ---------------- | ------- |
| `c.Param("id")` | path | `/{id}` |
| `c.Query("page")` | query | `?page=1` |
| `c.DefaultQuery("limit", "10")` | query (with default) | `?limit=10` |
| `c.GetHeader("Authorization")` | header | `Authorization: Bearer ...` |
| `c.ShouldBindQuery(&req)` | query (from struct) | Multiple query params |
| `c.ShouldBindUri(&req)` | path (from struct) | Multiple path params |

Query parameters are automatically type-inferred:

- `page`, `limit`, `offset`, `size` → `integer`
- `active`, `enabled`, `deleted` → `boolean`
- Others → `string`

### Request Body Detection

Detects request body from binding methods:

```go
func CreateUser(c *gin.Context) {
    var req CreateUserRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        // ...
    }
}

type CreateUserRequest struct {
    Name  string `json:"name" binding:"required"`
    Email string `json:"email" binding:"required,email"`
    Age   int    `json:"age"`
}
```

Supported methods: `ShouldBindJSON`, `BindJSON`, `ShouldBind`, `Bind`

### Response Detection

Detects response types from `c.JSON()` calls:

```go
// Single object response
c.JSON(http.StatusOK, user)

// Array response
c.JSON(http.StatusOK, users)  // []User → array of User

// Status code detection
c.JSON(200, data)
c.JSON(http.StatusCreated, data)
c.JSON(http.StatusBadRequest, gin.H{"error": "invalid"})
```

### Struct Tag Support

| Tag | Description | Example |
| --- | ----------- | ------- |
| `json` | JSON field name | `json:"user_name"` |
| `binding:"required"` | Mark field as required | `binding:"required"` |
| `example` | Example value | `example:"john@example.com"` |
| `form` | Query parameter name (for ShouldBindQuery) | `form:"page_size"` |
| `uri` | Path parameter name (for ShouldBindUri) | `uri:"user_id"` |

```go
type Product struct {
    ID          int       `json:"id"`
    Name        string    `json:"name" binding:"required" example:"iPhone 15"`
    Price       float64   `json:"price" binding:"required"`
    Description string    `json:"description,omitempty"`
    CreatedAt   time.Time `json:"created_at"`  // → format: date-time
}
```

## Comment Convention

While not required, swaggo reads function doc comments for summary and description:

```go
// GetUser retrieves user information
// Returns user details by ID. Returns 404 if not found.
func GetUser(c *gin.Context) {
    // First line → summary
    // Remaining lines → description
}
```

## Programmatic Usage

```go
package main

import (
    "github.com/miyago9267/swaggo/pkg/swaggo"
)

func main() {
    gen := swaggo.New().
        WithTitle("My API").
        WithDescription("API description").
        WithVersion("1.0.0").
        WithHost("localhost:8080").
        WithBasePath("/api/v1")

    // Parse source code
    if err := gen.ParseSource("./internal/api"); err != nil {
        panic(err)
    }

    // Get stats
    stats := gen.Stats()
    fmt.Printf("Routes: %d, Handlers: %d, Types: %d\n",
        stats.Routes, stats.Handlers, stats.Types)

    // Generate OpenAPI spec
    spec, err := gen.Generate()
    if err != nil {
        panic(err)
    }

    // Output
    jsonData, _ := spec.ToJSON()
    yamlData, _ := spec.ToYAML()
}
```

## Swagger UI Integration

swaggo generates a ready-to-use Swagger UI HTML file. To serve it in your Gin app:

```go
// Serve generated files
r.StaticFile("/swagger", "./docs/index.html")
r.StaticFile("/swagger/openapi.json", "./docs/openapi.json")
r.StaticFile("/swagger/openapi.yaml", "./docs/openapi.yaml")

// Or serve the entire docs directory
r.Static("/swagger", "./docs")
```

Then visit `http://localhost:8080/swagger` to view the documentation.

## Limitations

Some Go patterns cannot be fully analyzed at compile time:

| Limitation | Reason |
| ---------- | ------ |
| `interface{}` / `any` fields | Cannot determine actual type at compile time |
| `gin.H{}` responses | Dynamic map content cannot be statically analyzed |
| Generic types | Limited support for Go generics |
| Dynamic routes | Routes registered at runtime cannot be detected |

## Comparison with swaggo/swag

| Feature | swaggo (this) | swaggo/swag |
| ------- | ------------- | ----------- |
| Annotations required | ❌ No | ✅ Yes |
| OpenAPI version | 3.0 | 2.0 |
| Setup complexity | Low | Medium |
| Customization | Limited | Extensive |
| Learning curve | Minimal | Moderate |

**When to use swaggo (this):**

- Quick documentation for small/medium projects
- Don't want to maintain annotations
- OpenAPI 3.0 required

**When to use swaggo/swag:**

- Need fine-grained control
- Complex API documentation
- Extensive customization needed

## License

[MIT](../LICENSE)
