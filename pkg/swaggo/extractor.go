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

	p.extractDynamicRoutes(file, pkgName)

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

		if len(call.Args) < 2 {
			return true
		}

		path := p.extractStringArg(call.Args[0])

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
			Method:      method,
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
		if sel, ok := h.Fun.(*ast.SelectorExpr); ok {
			return p.resolveHandlerName(sel, currentPkg)
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
