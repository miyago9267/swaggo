# swaggo

從 Gin handler 自動產生 Swagger/OpenAPI 文檔，不需要寫那堆煩人的註解。

## 安裝

```bash
go install github.com/miyago9267/swaggo/cmd/swaggo@latest
```

## 使用

```bash
swaggo -dir ./your-project -title "My API"
```

完整參數：

```text
-dir        要解析的目錄（預設：.）
-output     輸出路徑，不含副檔名（預設：docs/openapi）
-format     輸出格式：json, yaml, both（預設：both）
-title      API 標題
-desc       API 描述
-version    API 版本（預設：1.0.0）
```

## 運作原理

swaggo 透過 Go 的 AST 解析你的原始碼，自動偵測：

路由註冊：

- `r.GET()`, `r.POST()` 等
- Route group 會正確處理前綴

參數：

- `c.Param("id")` 偵測為 path parameter
- `c.Query("page")` 偵測為 query parameter
- `c.DefaultQuery("limit", "10")` 會記錄預設值
- `c.GetHeader("Authorization")` 偵測為 header parameter

Request Body：

- `c.ShouldBindJSON(&req)` 會找到對應的 struct 型別

Response：

- `c.JSON(200, data)` 會推斷回應型別
- 支援 `http.StatusOK` 這類常數
- 陣列回應會正確標記為 array

Struct 欄位：

- `json` tag 決定欄位名稱
- `binding:"required"` 標記必填欄位
- `example:"value"` 設定範例值

## 註解慣例

雖然不強制，但 swaggo 會讀取函數的 doc comment：

```go
// GetUser 取得使用者資訊
// 根據 ID 查詢使用者，找不到回傳 404
func GetUser(c *gin.Context) {
    // 第一行變成 summary
    // 其餘變成 description
}
```

## 程式碼使用

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
