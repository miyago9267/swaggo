# swaggo

Automatically generate Swagger/OpenAPI documentation from Gin handlers without writing verbose annotations.

## Installation

```bash
go install github.com/miyago9267/swaggo/cmd/swaggo@latest
```

## Usage

```bash
swaggo -dir ./your-project -title "My API"
```

Full options:

```text
-dir        Directory to parse (default: .)
-output     Output path without extension (default: docs/openapi)
-format     Output format: json, yaml, both (default: both)
-title      API title
-desc       API description
-version    API version (default: 1.0.0)
```

## How It Works

swaggo parses your source code using Go's AST and detects:

Route registration:

- `r.GET()`, `r.POST()` etc.
- Route groups with proper prefix handling

Parameters:

- `c.Param("id")` detected as path parameter
- `c.Query("page")` detected as query parameter
- `c.DefaultQuery("limit", "10")` records default value
- `c.GetHeader("Authorization")` detected as header parameter

Request Body:

- `c.ShouldBindJSON(&req)` finds the corresponding struct type

Response:

- `c.JSON(200, data)` infers response type
- Supports constants like `http.StatusOK`
- Array responses are properly marked

Struct fields:

- `json` tag determines field name
- `binding:"required"` marks required fields
- `example:"value"` sets example value

## Comment Convention

While not required, swaggo reads function doc comments:

```go
// GetUser retrieves user information
// Queries user by ID, returns 404 if not found
func GetUser(c *gin.Context) {
    // First line becomes summary
    // Rest becomes description
}
```

## Programmatic Usage

```go
gen := swaggo.New().
    WithTitle("My API").
    WithVersion("1.0.0")

gen.ParseSource("./internal/api")

spec, _ := gen.Generate()
json, _ := spec.ToJSON()
yaml, _ := spec.ToYAML()
```

## License

MIT
