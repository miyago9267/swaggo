# Swaggo

![swaggo](https://raw.githubusercontent.com/miyago9267/swaggo/main/assets/swaggo.svg)

🌍 *[English](docs/README_EN.md) ∙ [繁體中文](README.md)*

[![Go Reference](https://pkg.go.dev/badge/github.com/miyago9267/swaggo.svg)](https://pkg.go.dev/github.com/miyago9267/swaggo)
[![Go Report Card](https://goreportcard.com/badge/github.com/miyago9267/swaggo)](https://goreportcard.com/report/github.com/miyago9267/swaggo)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Release](https://img.shields.io/github/release/miyago9267/swaggo.svg?style=flat-square)](https://github.com/miyago9267/swaggo/releases)

> **受夠了寫一堆 `@Summary`、`@Param`、`@Success` 註解了嗎？**
>
> swaggo 來了！直接接解析你的程式碼，自動產生 API 文檔。

從 [Gin](https://github.com/gin-gonic/gin) handler 自動產生 [OpenAPI 3.0](https://swagger.io/specification/) 文檔，透過 Go AST 解析，不需要寫任何註解。

## 目錄

- [Swaggo](#swaggo)
  - [目錄](#目錄)
  - [快速開始](#快速開始)
  - [CLI 選項](#cli-選項)
    - [範例](#範例)
    - [入口模式 (-e)](#入口模式--e)
  - [運作原理](#運作原理)
    - [路由偵測](#路由偵測)
    - [參數偵測](#參數偵測)
    - [Request Body 偵測](#request-body-偵測)
    - [Response 偵測](#response-偵測)
    - [Struct Tag 支援](#struct-tag-支援)
  - [註解慣例](#註解慣例)
  - [程式碼使用](#程式碼使用)
  - [Swagger UI 整合](#swagger-ui-整合)
  - [限制](#限制)
  - [與 swaggo/swag 的比較](#與-swaggoswag-的比較)
  - [License](#license)

## 快速開始

1. 安裝 swaggo：

```bash
go install github.com/miyago9267/swaggo/cmd/swaggo@latest
```

1. 在專案根目錄執行：

```bash
swaggo -dir . -title "My API"
```

1. 輸出：

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

1. 開啟 `docs/index.html` 即可瀏覽 Swagger UI。

## CLI 選項

```text
swaggo [flags]

Flags:
  -d, -dir string           專案根目錄（預設 "."）
  -e, -entry string         入口檔案（如 cmd/api/main.go），只解析 import 到的 package
  -o, -output string        輸出目錄（預設 "docs"）
  -t, -title string         API 標題（預設 "API Documentation"）
  -desc string              API 描述
  -version string           API 版本（預設 "1.0.0"）
  -host string              API host（例如 localhost:8080）
  -basePath string          API base path（預設 "/"）
  -format string            輸出格式：json, yaml, both（預設 "both"）
  -ui                       產生 Swagger UI HTML（預設 true）
  -x, -exclude string       排除的目錄（逗號分隔）
  -parseVendor              解析 vendor 目錄（預設 false）
  -parseDependency          解析外部依賴（預設 false）
  -q, -quiet                安靜模式，只輸出錯誤
  -v                        顯示版本
```

### 範例

```bash
# 基本用法 - 掃描整個專案
swaggo -d ./myproject -t "My API"

# 從指定入口掃描（推薦用於 monorepo/微服務）
swaggo -d . -e cmd/api/main.go -o docs/api
swaggo -d . -e cmd/admin/main.go -o docs/admin

# 排除目錄
swaggo -d . -x test,mock,scripts

# 設定 host 和 base path
swaggo -d . -host localhost:8080 -basePath /api/v1

# 安靜模式（CI/CD）
swaggo -d . -q
```

### 入口模式 (-e)

當指定 `-entry` 時，swaggo 只解析從入口檔案直接或間接 import 的 package。這對於 monorepo 或多服務專案特別有用：

```bash
project/
├── cmd/
│   ├── api/main.go      # API 服務入口
│   └── admin/main.go    # Admin 服務入口
├── internal/
│   ├── api/             # API handler
│   ├── admin/           # Admin handler
│   └── shared/          # 共用程式碼
└── go.mod

# 只產生 API 服務的文檔
swaggo -d . -e cmd/api/main.go -o docs/api

# 只產生 Admin 服務的文檔
swaggo -d . -e cmd/admin/main.go -o docs/admin
```

不指定 `-entry` 時，會掃描目錄下所有 `.go` 檔案。

## 運作原理

swaggo 使用 `go/ast` 解析原始碼，自動偵測 API 定義，不需要任何註解。

### 路由偵測

偵測所有 Gin 路由註冊：

```go
r := gin.Default()
r.GET("/health", HealthCheck)
r.POST("/users", CreateUser)

// Route group 會正確處理前綴
api := r.Group("/api/v1")
api.GET("/products", ListProducts)      // → /api/v1/products
api.GET("/products/:id", GetProduct)    // → /api/v1/products/{id}
```

支援的方法：`GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `OPTIONS`, `HEAD`

### 參數偵測

| Gin 方法 | OpenAPI 位置 | 範例 |
| ------- | ----------- | ---- |
| `c.Param("id")` | path | `/{id}` |
| `c.Query("page")` | query | `?page=1` |
| `c.DefaultQuery("limit", "10")` | query（含預設值） | `?limit=10` |
| `c.GetHeader("Authorization")` | header | `Authorization: Bearer ...` |
| `c.ShouldBindQuery(&req)` | query（從 struct） | 多個 query 參數 |
| `c.ShouldBindUri(&req)` | path（從 struct） | 多個 path 參數 |

Query 參數會自動推斷型別：

- `page`, `limit`, `offset`, `size` → `integer`
- `active`, `enabled`, `deleted` → `boolean`
- 其他 → `string`

### Request Body 偵測

從 binding 方法偵測 request body：

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

支援的方法：`ShouldBindJSON`, `BindJSON`, `ShouldBind`, `Bind`

### Response 偵測

從 `c.JSON()` 偵測回應型別：

```go
// 單一物件
c.JSON(http.StatusOK, user)

// 陣列
c.JSON(http.StatusOK, users)  // []User → array of User

// Status code 偵測
c.JSON(200, data)
c.JSON(http.StatusCreated, data)
c.JSON(http.StatusBadRequest, gin.H{"error": "invalid"})
```

### Struct Tag 支援

| Tag | 說明 | 範例 |
| --- | --- | ---- |
| `json` | JSON 欄位名稱 | `json:"user_name"` |
| `binding:"required"` | 標記必填 | `binding:"required"` |
| `example` | 範例值 | `example:"john@example.com"` |
| `form` | Query 參數名稱（ShouldBindQuery 用） | `form:"page_size"` |
| `uri` | Path 參數名稱（ShouldBindUri 用） | `uri:"user_id"` |

```go
type Product struct {
    ID          int       `json:"id"`
    Name        string    `json:"name" binding:"required" example:"iPhone 15"`
    Price       float64   `json:"price" binding:"required"`
    Description string    `json:"description,omitempty"`
    CreatedAt   time.Time `json:"created_at"`  // → format: date-time
}
```

## 註解慣例

雖然不強制，swaggo 會讀取函數的 doc comment 作為 summary 和 description：

```go
// GetUser 取得使用者資訊
// 根據 ID 查詢使用者，找不到回傳 404
func GetUser(c *gin.Context) {
    // 第一行 → summary
    // 其餘行 → description
}
```

## 程式碼使用

```go
package main

import (
    "github.com/miyago9267/swaggo/pkg/swaggo"
)

func main() {
    // 基本用法 - 掃描整個專案
    gen := swaggo.New().
        WithTitle("My API").
        WithDescription("API description").
        WithVersion("1.0.0").
        WithHost("localhost:8080").
        WithBasePath("/api/v1").
        WithProjectRoot(".")

    // 解析原始碼
    if err := gen.Parse(); err != nil {
        panic(err)
    }

    // 或指定入口檔案（微服務/monorepo）
    gen2 := swaggo.New().
        WithTitle("API Service").
        WithProjectRoot(".").
        WithEntry("cmd/api/main.go").   // 只解析這個入口 import 的 package
        WithExclude("test", "mock")

    if err := gen2.Parse(); err != nil {
        panic(err)
    }

    // 取得統計
    stats := gen.Stats()
    fmt.Printf("Routes: %d, Handlers: %d, Types: %d\n",
        stats.Routes, stats.Handlers, stats.Types)

    // 產生 OpenAPI spec
    spec, err := gen.Generate()
    if err != nil {
        panic(err)
    }

    // 輸出
    jsonData, _ := spec.ToJSON()
    yamlData, _ := spec.ToYAML()
}
```

## Swagger UI 整合

swaggo 會產生可直接使用的 Swagger UI HTML。在 Gin 中提供：

```go
// 提供產生的檔案
r.StaticFile("/swagger", "./docs/index.html")
r.StaticFile("/swagger/openapi.json", "./docs/openapi.json")
r.StaticFile("/swagger/openapi.yaml", "./docs/openapi.yaml")

// 或提供整個 docs 目錄
r.Static("/swagger", "./docs")
```

然後造訪 `http://localhost:8080/swagger` 即可瀏覽文檔。

## 限制

某些 Go 模式無法在編譯時期完整分析：

| 限制 | 原因 |
| --- | ---- |
| `interface{}` / `any` 欄位 | 編譯時期無法確定實際型別 |
| `gin.H{}` 回應 | 動態 map 內容無法靜態分析 |
| 泛型型別 | Go 泛型支援有限 |
| 動態路由 | 執行時期註冊的路由無法偵測 |

## 與 swaggo/swag 的比較

| 功能 | swaggo（本專案） | swaggo/swag |
| --- | --------------- | ----------- |
| 需要註解 | ❌ 不需要 | ✅ 需要 |
| OpenAPI 版本 | 3.0 | 2.0 |
| 設定複雜度 | 低 | 中 |
| 自訂彈性 | 有限 | 豐富 |
| 學習曲線 | 極低 | 中等 |

**適合使用 swaggo（本專案）的情況：**

- 中小型專案快速產生文檔
- 不想維護註解
- 需要 OpenAPI 3.0

**適合使用 swaggo/swag 的情況：**

- 需要細緻控制
- 複雜的 API 文檔
- 需要大量自訂

## License

[MIT](LICENSE)
