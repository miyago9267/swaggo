package swaggo

import (
	"go/ast"
	"go/token"
	"strings"
)

func (p *Parser) extractTypes(file *ast.File) {
	pkgName := file.Name.Name

	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}

		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}

		fullName := pkgName + "." + ts.Name.Name
		typeInfo := &TypeInfo{
			Name:     ts.Name.Name,
			FullName: fullName,
			Package:  pkgName,
			Kind:     "struct",
		}

		if ts.Doc != nil {
			typeInfo.Comment = strings.TrimSpace(ts.Doc.Text())
		}

		for _, field := range st.Fields.List {
			if len(field.Names) == 0 {
				continue
			}

			fi := &FieldInfo{
				Name: field.Names[0].Name,
				Type: p.typeToString(field.Type),
				Tags: make(map[string]string),
			}

			if field.Tag != nil {
				tag := strings.Trim(field.Tag.Value, "`")
				fi.Tags = parseStructTags(tag)

				if jsonTag, ok := fi.Tags["json"]; ok {
					parts := strings.Split(jsonTag, ",")
					if parts[0] != "-" {
						fi.JSONName = parts[0]
					}
				}
				if fi.JSONName == "" {
					fi.JSONName = fi.Name
				}

				if bindTag, ok := fi.Tags["binding"]; ok {
					fi.Required = strings.Contains(bindTag, "required")
				}

				if example, ok := fi.Tags["example"]; ok {
					fi.Example = example
				}
			} else {
				fi.JSONName = fi.Name
			}

			if field.Comment != nil {
				fi.Comment = strings.TrimSpace(field.Comment.Text())
			} else if field.Doc != nil {
				fi.Comment = strings.TrimSpace(field.Doc.Text())
			}

			typeInfo.Fields = append(typeInfo.Fields, fi)
		}

		p.Types[fullName] = typeInfo
		p.Types[ts.Name.Name] = typeInfo

		return true
	})
}

func (p *Parser) extractHandlers(file *ast.File) {
	pkgName := file.Name.Name

	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}

		if !p.isGinHandler(fn) {
			return true
		}

		handler := &HandlerInfo{
			Name:      fn.Name.Name,
			Package:   pkgName,
			FilePath:  p.fset.Position(fn.Pos()).Filename,
			Responses: make(map[int]*ResponseInfo),
		}

		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			recvType := p.extractReceiverType(fn.Recv.List[0].Type)
			handler.Receiver = recvType
			handler.FullName = pkgName + "." + recvType + "." + fn.Name.Name
		} else {
			handler.FullName = pkgName + "." + fn.Name.Name
		}

		if fn.Doc != nil {
			lines := strings.Split(strings.TrimSpace(fn.Doc.Text()), "\n")
			if len(lines) > 0 {
				handler.Summary = strings.TrimSpace(lines[0])
			}
			if len(lines) > 1 {
				handler.Description = strings.TrimSpace(strings.Join(lines[1:], "\n"))
			}
		}

		p.analyzeHandlerBody(fn, handler)
		p.Handlers[handler.FullName] = handler

		return true
	})
}

func (p *Parser) extractControllerInstances(file *ast.File) {
	pkgName := file.Name.Name

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for i, name := range vs.Names {
						if i < len(vs.Values) {
							typeName := p.inferControllerType(vs.Values[i], pkgName)
							if typeName != "" {
								p.controllerInstances[name.Name] = typeName
							}
						}
					}
				}
			}

		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok {
					if i < len(node.Rhs) {
						typeName := p.inferControllerType(node.Rhs[i], pkgName)
						if typeName != "" {
							p.controllerInstances[ident.Name] = typeName
						}
					}
				}
			}
		}

		return true
	})
}

func (p *Parser) extractRoutes(file *ast.File) {
	pkgName := file.Name.Name
	groupPrefixes := make(map[string]string)

	// 收集這個檔案中的 registrar 函數位置，用於跳過
	registrarRanges := make(map[token.Pos]token.Pos) // start -> end
	for _, reg := range p.routeRegistrars {
		if reg.File == file && reg.FuncDecl != nil && reg.FuncDecl.Body != nil {
			registrarRanges[reg.FuncDecl.Body.Pos()] = reg.FuncDecl.Body.End()
		}
	}

	// 檢查節點是否在 registrar 函數內
	isInRegistrar := func(pos token.Pos) bool {
		for start, end := range registrarRanges {
			if pos >= start && pos <= end {
				return true
			}
		}
		return false
	}

	p.extractDynamicRoutes(file, pkgName)
	p.extractForRangeRoutes(file, pkgName)

	ast.Inspect(file, func(n ast.Node) bool {
		if assign, ok := n.(*ast.AssignStmt); ok {
			for i, rhs := range assign.Rhs {
				if call, ok := rhs.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
						if sel.Sel.Name == "Group" && len(call.Args) > 0 {
							if prefix := p.extractStringArg(call.Args[0]); prefix != "" {
								parentPrefix := ""
								if parentIdent, ok := sel.X.(*ast.Ident); ok {
									parentPrefix = groupPrefixes[parentIdent.Name]
								}
								if i < len(assign.Lhs) {
									if ident, ok := assign.Lhs[i].(*ast.Ident); ok {
										groupPrefixes[ident.Name] = parentPrefix + prefix
									}
								}
							}
						}
					}
				}
			}
			return true
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		method := sel.Sel.Name

		httpMethods := map[string]bool{
			"GET": true, "POST": true, "PUT": true, "DELETE": true,
			"PATCH": true, "OPTIONS": true, "HEAD": true,
			"Any": true, "Handle": true,
		}

		if !httpMethods[method] {
			return true
		}

		// 跳過 registrar 函數內的路由（會由 extractRoutesWithPrefix 處理）
		if isInRegistrar(call.Pos()) {
			return true
		}

		// Handle() 的簽名是 Handle(method, path, ...handlers)，需要至少 3 個引數
		// 其他 HTTP method 的簽名是 GET(path, ...handlers)，需要至少 2 個引數
		if method == "Handle" {
			if len(call.Args) < 3 {
				return true
			}
		} else {
			if len(call.Args) < 2 {
				return true
			}
		}

		var httpMethod, path string
		if method == "Handle" {
			httpMethod = p.extractStringArg(call.Args[0])
			path = p.extractStringArg(call.Args[1])
		} else {
			httpMethod = method
			path = p.extractStringArg(call.Args[0])
		}

		groupPrefix := ""
		if receiverIdent, ok := sel.X.(*ast.Ident); ok {
			groupPrefix = groupPrefixes[receiverIdent.Name]
		}

		if path == "" && groupPrefix == "" {
			return true
		}

		handlerArg := call.Args[len(call.Args)-1]
		handlerName := p.resolveHandlerName(handlerArg, pkgName)

		if handlerName == "" || shouldSkipHandler(handlerName) {
			return true
		}

		route := &RouteInfo{
			Method:      httpMethod,
			Path:        groupPrefix + path,
			HandlerName: handlerName,
			Group:       groupPrefix,
		}

		p.Routes = append(p.Routes, route)
		return true
	})
}

func (p *Parser) extractDynamicRoutes(file *ast.File, pkgName string) {
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}

		_, isArray := cl.Type.(*ast.ArrayType)
		if !isArray {
			return true
		}

		for _, elt := range cl.Elts {
			route := p.parseRouteDefinition(elt, pkgName)
			if route != nil {
				p.Routes = append(p.Routes, route)
			}
		}

		return true
	})
}

// extractForRangeRoutes 偵測 for-range 迴圈中的路由註冊
// 例如：for _, route := range routes { r.Handle(route.Method, route.Path, route.Handler) }
func (p *Parser) extractForRangeRoutes(file *ast.File, pkgName string) {
	sliceLiterals := p.collectSliceLiterals(file)
	if len(sliceLiterals) == 0 {
		return
	}

	groupPrefixes := p.collectGroupPrefixes(file)

	ast.Inspect(file, func(n ast.Node) bool {
		rangeStmt, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}

		// 取得迴圈值變數名（for _, route := range ... 中的 route）
		valueVar := ""
		if rangeStmt.Value != nil {
			if ident, ok := rangeStmt.Value.(*ast.Ident); ok {
				valueVar = ident.Name
			}
		}
		if valueVar == "" {
			return true
		}

		// 取得 range 目標變數名
		targetName := ""
		if ident, ok := rangeStmt.X.(*ast.Ident); ok {
			targetName = ident.Name
		}
		if targetName == "" {
			return true
		}

		elements, ok := sliceLiterals[targetName]
		if !ok || len(elements) == 0 {
			return true
		}

		p.processForRangeBody(rangeStmt.Body, valueVar, elements, pkgName, groupPrefixes)
		return true
	})
}

// collectSliceLiterals 收集檔案中所有 slice/array composite literal 的賦值
func (p *Parser) collectSliceLiterals(file *ast.File) map[string][]ast.Expr {
	result := make(map[string][]ast.Expr)

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range node.Rhs {
				cl, ok := rhs.(*ast.CompositeLit)
				if !ok {
					continue
				}
				if _, isArray := cl.Type.(*ast.ArrayType); !isArray {
					continue
				}
				if i < len(node.Lhs) {
					if ident, ok := node.Lhs[i].(*ast.Ident); ok {
						result[ident.Name] = cl.Elts
					}
				}
			}
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, value := range vs.Values {
					cl, ok := value.(*ast.CompositeLit)
					if !ok {
						continue
					}
					if _, isArray := cl.Type.(*ast.ArrayType); !isArray {
						continue
					}
					if i < len(vs.Names) {
						result[vs.Names[i].Name] = cl.Elts
					}
				}
			}
		}
		return true
	})

	return result
}

// processForRangeBody 處理 for-range body 中的路由呼叫
func (p *Parser) processForRangeBody(body *ast.BlockStmt, valueVar string, elements []ast.Expr, pkgName string, groupPrefixes map[string]string) {
	if body == nil {
		return
	}

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		method := sel.Sel.Name
		if !isHTTPMethod(method) {
			return true
		}

		groupPrefix := ""
		if ident, ok := sel.X.(*ast.Ident); ok {
			groupPrefix = groupPrefixes[ident.Name]
		}

		for _, elt := range elements {
			fieldValues := p.extractElementFieldValues(elt)
			if len(fieldValues) == 0 {
				continue
			}

			route := p.resolveForRangeRouteCall(call, method, valueVar, fieldValues, pkgName, groupPrefix)
			if route != nil && !p.routeExists(route.Method, route.Path) {
				p.Routes = append(p.Routes, route)
			}
		}

		return true
	})
}

// extractElementFieldValues 從 composite literal 元素中提取欄位值
func (p *Parser) extractElementFieldValues(expr ast.Expr) map[string]ast.Expr {
	cl, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}

	result := make(map[string]ast.Expr)
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok {
			result[key.Name] = kv.Value
		}
	}
	return result
}

// resolveForRangeRouteCall 解析 for-range body 中的單一路由呼叫
func (p *Parser) resolveForRangeRouteCall(call *ast.CallExpr, method string, valueVar string, fieldValues map[string]ast.Expr, pkgName string, groupPrefix string) *RouteInfo {
	var httpMethod, path, handlerName string

	if method == "Handle" {
		if len(call.Args) < 3 {
			return nil
		}
		httpMethod = p.resolveFieldAccess(call.Args[0], valueVar, fieldValues)
		path = p.resolveFieldAccess(call.Args[1], valueVar, fieldValues)
		handlerName = p.resolveHandlerFieldAccess(call.Args[len(call.Args)-1], valueVar, fieldValues, pkgName)
	} else {
		if len(call.Args) < 2 {
			return nil
		}
		httpMethod = method
		path = p.resolveFieldAccess(call.Args[0], valueVar, fieldValues)
		handlerName = p.resolveHandlerFieldAccess(call.Args[len(call.Args)-1], valueVar, fieldValues, pkgName)
	}

	if httpMethod == "" || path == "" || handlerName == "" {
		return nil
	}

	if shouldSkipHandler(handlerName) {
		return nil
	}

	return &RouteInfo{
		Method:      httpMethod,
		Path:        groupPrefix + path,
		HandlerName: handlerName,
		Group:       groupPrefix,
	}
}

// resolveFieldAccess 解析字串欄位：可能是字串字面量或 loop variable 的欄位存取
func (p *Parser) resolveFieldAccess(expr ast.Expr, valueVar string, fieldValues map[string]ast.Expr) string {
	if str := p.extractStringArg(expr); str != "" {
		return str
	}

	if sel, ok := expr.(*ast.SelectorExpr); ok {
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == valueVar {
			if val, ok := fieldValues[sel.Sel.Name]; ok {
				return p.extractStringArg(val)
			}
		}
	}

	return ""
}

// resolveHandlerFieldAccess 解析 handler 欄位：可能是直接引用或 loop variable 的欄位存取
func (p *Parser) resolveHandlerFieldAccess(expr ast.Expr, valueVar string, fieldValues map[string]ast.Expr, pkgName string) string {
	if name := p.resolveHandlerName(expr, pkgName); name != "" {
		return name
	}

	if sel, ok := expr.(*ast.SelectorExpr); ok {
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == valueVar {
			if val, ok := fieldValues[sel.Sel.Name]; ok {
				return p.resolveHandlerName(val, pkgName)
			}
		}
	}

	return ""
}

func (p *Parser) analyzeHandlerBody(fn *ast.FuncDecl, handler *HandlerInfo) {
	if fn.Body == nil {
		return
	}

	localVarTypes := make(map[string]string)

	for _, stmt := range fn.Body.List {
		switch s := stmt.(type) {
		case *ast.DeclStmt:
			if gd, ok := s.Decl.(*ast.GenDecl); ok {
				for _, spec := range gd.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok {
						for _, n := range vs.Names {
							if vs.Type != nil {
								localVarTypes[n.Name] = p.typeToString(vs.Type)
							}
						}
					}
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					if i < len(s.Rhs) {
						if cl, ok := s.Rhs[i].(*ast.CompositeLit); ok {
							localVarTypes[id.Name] = p.typeToString(cl.Type)
						}
					}
				}
			}
		}
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		method := sel.Sel.Name

		switch method {
		case "Param":
			if len(call.Args) > 0 {
				if name := p.extractStringArg(call.Args[0]); name != "" {
					handler.Parameters = append(handler.Parameters, &ParameterInfo{
						Name:     name,
						Type:     "string",
						In:       "path",
						Required: true,
					})
				}
			}

		case "Query":
			if len(call.Args) > 0 {
				if name := p.extractStringArg(call.Args[0]); name != "" {
					handler.Parameters = append(handler.Parameters, &ParameterInfo{
						Name: name,
						Type: inferQueryParamType(name),
						In:   "query",
					})
				}
			}

		case "DefaultQuery":
			if len(call.Args) >= 2 {
				if name := p.extractStringArg(call.Args[0]); name != "" {
					handler.Parameters = append(handler.Parameters, &ParameterInfo{
						Name:    name,
						Type:    inferQueryParamType(name),
						In:      "query",
						Default: p.extractStringArg(call.Args[1]),
					})
				}
			}

		case "GetHeader":
			if len(call.Args) > 0 {
				if name := p.extractStringArg(call.Args[0]); name != "" {
					handler.Parameters = append(handler.Parameters, &ParameterInfo{
						Name: name,
						Type: "string",
						In:   "header",
					})
				}
			}

		case "ShouldBindQuery", "BindQuery":
			if len(call.Args) > 0 {
				typeName := p.extractTypeFromBindArgWithLocals(call.Args[0], localVarTypes)
				if typeName != "" {
					if typeInfo := p.findType(typeName); typeInfo != nil {
						for _, field := range typeInfo.Fields {
							paramName := field.JSONName
							if formTag, ok := field.Tags["form"]; ok {
								parts := strings.Split(formTag, ",")
								if parts[0] != "" && parts[0] != "-" {
									paramName = parts[0]
								}
							}
							handler.Parameters = append(handler.Parameters, &ParameterInfo{
								Name:     paramName,
								Type:     field.Type,
								In:       "query",
								Required: field.Required,
							})
						}
					}
				}
			}

		case "ShouldBindUri", "BindUri":
			if len(call.Args) > 0 {
				typeName := p.extractTypeFromBindArgWithLocals(call.Args[0], localVarTypes)
				if typeName != "" {
					if typeInfo := p.findType(typeName); typeInfo != nil {
						for _, field := range typeInfo.Fields {
							paramName := field.JSONName
							if uriTag, ok := field.Tags["uri"]; ok {
								parts := strings.Split(uriTag, ",")
								if parts[0] != "" && parts[0] != "-" {
									paramName = parts[0]
								}
							}
							handler.Parameters = append(handler.Parameters, &ParameterInfo{
								Name:     paramName,
								Type:     field.Type,
								In:       "path",
								Required: true,
							})
						}
					}
				}
			}

		case "ShouldBindJSON", "BindJSON", "ShouldBind", "Bind":
			if len(call.Args) > 0 {
				typeName := p.extractTypeFromBindArgWithLocals(call.Args[0], localVarTypes)
				if typeName != "" {
					if typeInfo := p.findType(typeName); typeInfo != nil {
						handler.RequestBody = typeInfo
					} else {
						handler.RequestBody = &TypeInfo{Name: typeName, Kind: "struct"}
					}
				}
			}

		case "JSON":
			if len(call.Args) >= 2 {
				statusCode := p.extractStatusCode(call.Args[0])
				if statusCode > 0 {
					resp := &ResponseInfo{StatusCode: statusCode}
					resp.Type, resp.IsArray = p.extractResponseTypeWithLocals(call.Args[1], localVarTypes)
					handler.Responses[statusCode] = resp
				}
			}

		case "String":
			if len(call.Args) >= 1 {
				statusCode := p.extractStatusCode(call.Args[0])
				if statusCode > 0 {
					handler.Responses[statusCode] = &ResponseInfo{
						StatusCode: statusCode,
						Type:       &TypeInfo{Kind: "primitive", Name: "string"},
					}
				}
			}
		}

		return true
	})
}

func (p *Parser) isGinHandler(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return false
	}
	for _, param := range fn.Type.Params.List {
		typeStr := p.typeToString(param.Type)
		if strings.Contains(typeStr, "gin.Context") || strings.Contains(typeStr, "Context") {
			return true
		}
	}
	return false
}

func (p *Parser) extractReceiverType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return p.extractReceiverType(t.X)
	}
	return ""
}

func (p *Parser) inferControllerType(expr ast.Expr, currentPkg string) string {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return p.inferControllerType(e.X, currentPkg)
		}
	case *ast.CompositeLit:
		return p.typeExprToFullName(e.Type, currentPkg)
	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			funcName := sel.Sel.Name
			if strings.HasPrefix(funcName, "New") {
				typeName := strings.TrimPrefix(funcName, "New")
				if pkgIdent, ok := sel.X.(*ast.Ident); ok {
					return pkgIdent.Name + "." + typeName
				}
			}
		}
		if ident, ok := e.Fun.(*ast.Ident); ok {
			funcName := ident.Name
			if strings.HasPrefix(funcName, "New") {
				typeName := strings.TrimPrefix(funcName, "New")
				return currentPkg + "." + typeName
			}
		}
	}
	return ""
}

func (p *Parser) typeExprToFullName(expr ast.Expr, currentPkg string) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return currentPkg + "." + t.Name
	case *ast.SelectorExpr:
		if pkgIdent, ok := t.X.(*ast.Ident); ok {
			return pkgIdent.Name + "." + t.Sel.Name
		}
	case *ast.StarExpr:
		return p.typeExprToFullName(t.X, currentPkg)
	}
	return ""
}

func (p *Parser) resolveHandlerName(expr ast.Expr, currentPkg string) string {
	switch h := expr.(type) {
	case *ast.Ident:
		return currentPkg + "." + h.Name
	case *ast.SelectorExpr:
		if pkgOrVar, ok := h.X.(*ast.Ident); ok {
			varName := pkgOrVar.Name
			methodName := h.Sel.Name

			if ctrlType, ok := p.controllerInstances[varName]; ok {
				return ctrlType + "." + methodName
			}
			return varName + "." + methodName
		}
	case *ast.CallExpr:
		// 工廠函數呼叫: MakeHandler() 或 pkg.MakeHandler()
		switch fn := h.Fun.(type) {
		case *ast.Ident:
			// 同 package 呼叫: MakeGreetHandler("Hello")
			return currentPkg + "." + fn.Name
		case *ast.SelectorExpr:
			// 跨 package 呼叫: closure.MakeGreetHandler("Hello")
			if pkg, ok := fn.X.(*ast.Ident); ok {
				return pkg.Name + "." + fn.Sel.Name
			}
		}
	}
	return ""
}

func (p *Parser) parseRouteDefinition(expr ast.Expr, pkgName string) *RouteInfo {
	cl, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}

	route := &RouteInfo{}

	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}

		switch strings.ToLower(key.Name) {
		case "method":
			route.Method = p.extractStringArg(kv.Value)
		case "path":
			route.Path = p.extractStringArg(kv.Value)
		case "handler":
			route.HandlerName = p.resolveHandlerName(kv.Value, pkgName)
		}
	}

	if route.Method != "" && route.Path != "" && route.HandlerName != "" {
		return route
	}

	return nil
}

func (p *Parser) extractResponseTypeWithLocals(expr ast.Expr, localVars map[string]string) (*TypeInfo, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		typeName := e.Name

		if typeName == "nil" {
			return nil, false
		}

		if typeInfo := p.findType(typeName); typeInfo != nil {
			return typeInfo, false
		}

		varType := ""
		if localVars != nil {
			varType = localVars[typeName]
		}
		if varType == "" {
			varType = p.findVariableType(typeName)
		}

		varType = strings.TrimPrefix(varType, "*")

		if strings.HasPrefix(varType, "[]") {
			elemType := strings.TrimPrefix(varType, "[]")
			if typeInfo := p.findType(elemType); typeInfo != nil {
				return typeInfo, true
			}
			return &TypeInfo{Name: elemType, Kind: "struct"}, true
		}
		if typeInfo := p.findType(varType); typeInfo != nil {
			return typeInfo, false
		}
		return &TypeInfo{Name: varType, Kind: "struct"}, false

	case *ast.CompositeLit:
		switch t := e.Type.(type) {
		case *ast.ArrayType:
			elemType := p.typeToString(t.Elt)
			if typeInfo := p.findType(elemType); typeInfo != nil {
				return typeInfo, true
			}
			return &TypeInfo{Name: elemType, Kind: "struct"}, true
		case *ast.MapType:
			return &TypeInfo{Kind: "map", Name: "object"}, false
		case *ast.SelectorExpr:
			if t.Sel.Name == "H" {
				return &TypeInfo{Kind: "map", Name: "object"}, false
			}
		case *ast.Ident:
			if t.Name == "H" {
				return &TypeInfo{Kind: "map", Name: "object"}, false
			}
			if typeInfo := p.findType(t.Name); typeInfo != nil {
				return typeInfo, false
			}
		}

	case *ast.CallExpr:
		return &TypeInfo{Kind: "any", Name: "object"}, false

	case *ast.UnaryExpr:
		return p.extractResponseTypeWithLocals(e.X, localVars)
	}

	return &TypeInfo{Kind: "any", Name: "object"}, false
}
