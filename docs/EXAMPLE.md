# Example - Store API

這是一個使用 swaggo 的範例專案，展示如何整合 Swagger UI。

## 執行範例

```bash
cd example
go run main.go
```

瀏覽器開啟 <http://localhost:8080/swagger> 查看 Swagger UI。

## 產生文檔

```bash
swaggo -dir ./example -title "Store API" -output ./example/docs/openapi
```

## 整合 Swagger UI

在你的 Gin 專案中加入：

```go
// 提供 OpenAPI spec
r.StaticFile("/swagger/openapi.json", "./docs/openapi.json")

// 提供 Swagger UI（使用 CDN）
r.StaticFile("/swagger", "./docs/swagger-ui.html")
```

swagger-ui.html 內容：

```html
<!DOCTYPE html>
<html>
<head>
  <title>API Docs</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: "/swagger/openapi.json",
      dom_id: '#swagger-ui'
    });
  </script>
</body>
</html>
```

## 專案結構

```tree
example/
├── main.go              # API handlers
└── docs/
    ├── openapi.json     # 產生的 OpenAPI spec
    ├── openapi.yaml
    └── swagger-ui.html  # Swagger UI 頁面
```
