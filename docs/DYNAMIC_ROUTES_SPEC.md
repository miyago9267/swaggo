# Dynamic Routes Support Spec

## 背景

swaggo 目前無法處理「動態路由」— 透過 for-loop 批量定義、interface 型別參數、variadic 函數傳遞等方式註冊的 API 路由。這在中大型 Gin 專案中是常見模式。

## 目標模式

| #   | 模式                      | 說明                                                                                    | Phase |
| --- | ------------------------- | --------------------------------------------------------------------------------------- | ----- |
| A   | `gin.IRouter` 參數支援    | 函數接收 `gin.IRouter` / `gin.IRoutes` 時也能被辨識為 route registrar                   | 1     |
| B   | `Handle()` 參數修正       | `r.Handle(method, path, handler)` 的引數順序與 `r.GET(path, handler)` 不同，需特別處理  | 1     |
| C   | for-range + 已知 slice    | 偵測 `for _, r := range routes { group.Handle(...) }` 並解析 slice literal 內容         | 2     |
| D   | for-range interface 呼叫  | `for _, m := range modules { m.RegisterRoutes(r) }` 追蹤具體型別                        | 3     |
| E   | variadic 函數引數追蹤     | `SetupRoutes(mod1.Register, mod2.Register)` 追蹤傳入的函數引用                          | 3     |

依賴關係：`A ← D, E`、`B ← C`、`A` 和 `B` 互不依賴。

## Phase 1：基礎建設

### A - gin.IRouter / gin.IRoutes 參數支援

#### 現況

`getGinParamType()` 只處理 `*ast.StarExpr`（pointer type），因此只認 `*gin.RouterGroup` 和 `*gin.Engine`。

#### 問題

`gin.IRouter` 和 `gin.IRoutes` 是 interface（非 pointer），AST 結構為 `ast.SelectorExpr` 而非 `*ast.StarExpr > ast.SelectorExpr`。

#### 改動

修改 `getGinParamType()` 使其同時檢查：

1. `*ast.StarExpr > ast.SelectorExpr`（現有，pointer type）
2. `ast.SelectorExpr`（新增，interface type）

新增支援的型別：`gin.IRouter`、`gin.IRoutes`。

#### 影響範圍

- `callsite.go` — `getGinParamType()`
- 所有 registrar 偵測流程自動受益

### B - Handle() 參數修正

#### B 現況

`tryAddRouteFromCall()` 和 `extractRoutes()` 把所有 HTTP method 呼叫統一處理：

- `Args[0]` → path
- `Args[last]` → handler

#### B 問題

`r.Handle("GET", "/path", handler)` 的簽名是 `Handle(method, path, ...handlers)`：

- `Args[0]` → HTTP method（非 path）
- `Args[1]` → path
- `Args[2+]` → handlers

#### B 改動

當 `sel.Sel.Name == "Handle"` 時，調整 argument 索引：

- method = `Args[0]`（string literal）
- path = `Args[1]`
- handler = `Args[last]`

並且確保 `len(call.Args) >= 3`。

#### B 影響範圍

- `callsite.go` — `tryAddRouteFromCall()`
- `extractor.go` — `extractRoutes()` 內的 route 解析邏輯

## Progress

- [x] 完成痛點分析與模式盤點
- [x] 確定 phase 分期與優先順序
- [x] 撰寫 spec
- [x] Phase 1：A + B 實作
- [x] Phase 1：測試（callsite_test.go，6 個測試全過）
- [x] Phase 2：for-range + 已知 slice（C）— 含 Handle()、group prefix、package-level var 三個測試
- [ ] Phase 3：interface 迴圈呼叫 + variadic 追蹤（D + E）
