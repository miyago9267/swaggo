package swaggo

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

func (p *Parser) getSimpleName(fullName string) string {
	parts := strings.Split(fullName, ".")
	return parts[len(parts)-1]
}

func (p *Parser) typeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return p.typeToString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + p.typeToString(t.X)
	case *ast.ArrayType:
		return "[]" + p.typeToString(t.Elt)
	case *ast.MapType:
		return "map[" + p.typeToString(t.Key) + "]" + p.typeToString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	default:
		return "any"
	}
}

func (p *Parser) extractStringArg(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			s, _ := strconv.Unquote(e.Value)
			return s
		}
	}
	return ""
}

func (p *Parser) extractTypeFromBindArgWithLocals(expr ast.Expr, localVars map[string]string) string {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return p.extractTypeFromBindArgWithLocals(e.X, localVars)
		}
	case *ast.Ident:
		if localVars != nil {
			if typeName, ok := localVars[e.Name]; ok {
				return strings.TrimPrefix(typeName, "*")
			}
		}
		return p.findVariableType(e.Name)
	case *ast.CompositeLit:
		return p.typeToString(e.Type)
	}
	return ""
}

func (p *Parser) extractStatusCode(expr ast.Expr) int {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.INT {
			code, _ := strconv.Atoi(e.Value)
			return code
		}
	case *ast.SelectorExpr:
		return httpStatusCode(e.Sel.Name)
	case *ast.Ident:
		return httpStatusCode(e.Name)
	}
	return 0
}

func parseStructTags(tag string) map[string]string {
	tags := make(map[string]string)

	for tag != "" {
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}

		i = 0
		for i < len(tag) && tag[i] != ':' && tag[i] != '"' && tag[i] != ' ' {
			i++
		}
		if i >= len(tag) {
			break
		}
		key := tag[:i]
		tag = tag[i:]

		if tag == "" || tag[0] != ':' {
			break
		}
		tag = tag[1:]

		if tag == "" || tag[0] != '"' {
			break
		}
		tag = tag[1:]

		i = 0
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			break
		}

		value := tag[:i]
		tags[key] = value
		tag = tag[i+1:]
	}

	return tags
}

func httpStatusCode(name string) int {
	codes := map[string]int{
		"StatusContinue":            100,
		"StatusOK":                  200,
		"StatusCreated":             201,
		"StatusAccepted":            202,
		"StatusNoContent":           204,
		"StatusMovedPermanently":    301,
		"StatusFound":               302,
		"StatusBadRequest":          400,
		"StatusUnauthorized":        401,
		"StatusForbidden":           403,
		"StatusNotFound":            404,
		"StatusMethodNotAllowed":    405,
		"StatusConflict":            409,
		"StatusUnprocessableEntity": 422,
		"StatusTooManyRequests":     429,
		"StatusInternalServerError": 500,
		"StatusBadGateway":          502,
		"StatusServiceUnavailable":  503,
	}
	return codes[name]
}

func inferQueryParamType(name string) string {
	integerParams := map[string]bool{
		"page": true, "limit": true, "offset": true, "size": true,
		"per_page": true, "page_size": true, "count": true,
		"id": true, "user_id": true, "order_id": true, "product_id": true,
	}
	boolParams := map[string]bool{
		"active": true, "enabled": true, "deleted": true, "archived": true,
		"is_active": true, "is_deleted": true, "published": true,
	}

	lower := strings.ToLower(name)
	if integerParams[lower] {
		return "integer"
	}
	if boolParams[lower] {
		return "boolean"
	}
	return "string"
}

func shouldSkipHandler(name string) bool {
	skipPatterns := []string{
		"StaticFile", "Static", "StaticFS",
		"func1", "func2", "func3", "func4", "func5",
	}
	for _, pattern := range skipPatterns {
		if strings.Contains(name, pattern) {
			return true
		}
	}
	return false
}
