package swaggo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Parser 解析 Go 原始碼，提取 API 資訊
type Parser struct {
	fset     *token.FileSet
	packages map[string]*ast.Package
	files    []*ast.File

	Routes   []*RouteInfo
	Handlers map[string]*HandlerInfo
	Types    map[string]*TypeInfo

	controllerInstances map[string]string
	routeRegistrars     map[string]*RouteRegistrar // 記錄接受 RouterGroup 參數的函數
	excludeDirs         []string
	parseVendor         bool
	parseDependency     bool
}

// RouteInfo 路由資訊
type RouteInfo struct {
	Method      string
	Path        string
	HandlerName string
	Handler     *HandlerInfo
	Group       string
}

// HandlerInfo Handler 函數資訊
type HandlerInfo struct {
	Name        string
	FullName    string
	Package     string
	Receiver    string
	FilePath    string
	Summary     string
	Description string
	Parameters  []*ParameterInfo
	RequestBody *TypeInfo
	Responses   map[int]*ResponseInfo
}

// ParameterInfo 參數資訊
type ParameterInfo struct {
	Name     string
	Type     string
	In       string
	Required bool
	Default  string
	Comment  string
}

// ResponseInfo 回應資訊
type ResponseInfo struct {
	StatusCode  int
	Type        *TypeInfo
	IsArray     bool
	Description string
}

// TypeInfo 型別資訊
type TypeInfo struct {
	Name     string
	FullName string
	Package  string
	Kind     string
	Fields   []*FieldInfo
	Element  *TypeInfo
	Comment  string
}

// FieldInfo 欄位資訊
type FieldInfo struct {
	Name     string
	Type     string
	TypeInfo *TypeInfo
	JSONName string
	Comment  string
	Required bool
	Example  string
	Tags     map[string]string
}

func NewParser() *Parser {
	return &Parser{
		fset:                token.NewFileSet(),
		packages:            make(map[string]*ast.Package),
		Handlers:            make(map[string]*HandlerInfo),
		Types:               make(map[string]*TypeInfo),
		controllerInstances: make(map[string]string),
		routeRegistrars:     make(map[string]*RouteRegistrar),
	}
}

func (p *Parser) ParseDir(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()

			if name == ".git" {
				return filepath.SkipDir
			}
			if name == "vendor" && !p.parseVendor {
				return filepath.SkipDir
			}
			for _, exc := range p.excludeDirs {
				if name == exc {
					return filepath.SkipDir
				}
			}

			pkgs, err := parser.ParseDir(p.fset, path, func(fi os.FileInfo) bool {
				return !strings.HasSuffix(fi.Name(), "_test.go")
			}, parser.ParseComments)
			if err != nil {
				return nil
			}
			for name, pkg := range pkgs {
				p.packages[name] = pkg
				for _, file := range pkg.Files {
					p.files = append(p.files, file)
				}
			}
		}
		return nil
	})
}

func (p *Parser) ParseFile(filename string) error {
	file, err := parser.ParseFile(p.fset, filename, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	p.files = append(p.files, file)
	return nil
}

func (p *Parser) Analyze() error {
	for _, file := range p.files {
		p.extractTypes(file)
	}
	for _, file := range p.files {
		p.extractHandlers(file)
	}

	// P3 修復：收集並註冊閉包工廠函數的 handler
	closureFactories := p.collectClosureFactories()
	p.registerClosureHandlers(closureFactories)

	for _, file := range p.files {
		p.extractControllerInstances(file)
	}

	// 先收集 route registrars，這樣 extractRoutes 可以跳過這些函數
	p.routeRegistrars = p.collectRouteRegistrars()

	for _, file := range p.files {
		p.extractRoutes(file)
	}

	// P1 修復：追蹤跨檔案的 RouterGroup 傳遞
	callSites := p.findCallSites(p.routeRegistrars)
	for _, cs := range callSites {
		p.extractRoutesWithPrefix(cs.Registrar, cs.GroupPrefix)
	}

	for _, route := range p.Routes {
		if handler, ok := p.Handlers[route.HandlerName]; ok {
			route.Handler = handler
			p.addPathParams(route)
		} else {
			for key, handler := range p.Handlers {
				if strings.HasSuffix(key, "."+p.getSimpleName(route.HandlerName)) {
					route.Handler = handler
					route.HandlerName = key
					p.addPathParams(route)
					break
				}
			}
		}
	}

	return nil
}

func (p *Parser) addPathParams(route *RouteInfo) {
	if route.Handler == nil {
		return
	}

	re := regexp.MustCompile(`[:\*](\w+)`)
	matches := re.FindAllStringSubmatch(route.Path, -1)

	existingParams := make(map[string]bool)
	for _, param := range route.Handler.Parameters {
		if param.In == "path" {
			existingParams[param.Name] = true
		}
	}

	for _, match := range matches {
		if len(match) > 1 && !existingParams[match[1]] {
			route.Handler.Parameters = append(route.Handler.Parameters, &ParameterInfo{
				Name:     match[1],
				Type:     "string",
				In:       "path",
				Required: true,
			})
		}
	}
}

func (p *Parser) findType(name string) *TypeInfo {
	if t, ok := p.Types[name]; ok {
		return t
	}
	simpleName := p.getSimpleName(name)
	if t, ok := p.Types[simpleName]; ok {
		return t
	}
	return nil
}

func (p *Parser) findVariableType(name string) string {
	for _, file := range p.files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			for _, stmt := range fn.Body.List {
				switch s := stmt.(type) {
				case *ast.DeclStmt:
					if gd, ok := s.Decl.(*ast.GenDecl); ok {
						for _, spec := range gd.Specs {
							if vs, ok := spec.(*ast.ValueSpec); ok {
								for _, n := range vs.Names {
									if n.Name == name && vs.Type != nil {
										return p.typeToString(vs.Type)
									}
								}
							}
						}
					}
				case *ast.AssignStmt:
					for i, lhs := range s.Lhs {
						if id, ok := lhs.(*ast.Ident); ok && id.Name == name {
							if i < len(s.Rhs) {
								if cl, ok := s.Rhs[i].(*ast.CompositeLit); ok {
									return p.typeToString(cl.Type)
								}
							}
						}
					}
				}
			}
		}
	}
	return name
}
