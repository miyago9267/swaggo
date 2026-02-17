package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/miyago9267/swaggo/pkg/swaggo"
)

var version = "dev"

const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{TITLE}} - Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    html { box-sizing: border-box; overflow-y: scroll; }
    *, *:before, *:after { box-sizing: inherit; }
    body { margin: 0; background: #fafafa; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
  <script>
    window.onload = function() {
      SwaggerUIBundle({
        url: "./openapi.json",
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIStandalonePreset
        ],
        plugins: [
          SwaggerUIBundle.plugins.DownloadUrl
        ],
        layout: "StandaloneLayout"
      });
    };
  </script>
</body>
</html>`

func main() {
	var (
		dir         string
		entry       string
		output      string
		format      string
		title       string
		description string
		apiVersion  string
		host        string
		basePath    string
		showVersion bool
		quiet       bool
		generateUI  bool
		exclude     string
		parseVendor bool
		parseDeps   bool
	)

	flag.StringVar(&dir, "dir", ".", "Project root directory")
	flag.StringVar(&dir, "d", ".", "Project root directory (shorthand)")
	flag.StringVar(&entry, "entry", "", "Entry file (e.g. cmd/api/main.go). Only parses imported packages")
	flag.StringVar(&entry, "e", "", "Entry file (shorthand)")
	flag.StringVar(&output, "output", "docs", "Output directory")
	flag.StringVar(&output, "o", "docs", "Output directory (shorthand)")
	flag.StringVar(&format, "format", "both", "Output format: json, yaml, both")
	flag.StringVar(&title, "title", "API Documentation", "API title")
	flag.StringVar(&title, "t", "API Documentation", "API title (shorthand)")
	flag.StringVar(&description, "desc", "", "API description")
	flag.StringVar(&apiVersion, "version", "1.0.0", "API version")
	flag.StringVar(&host, "host", "", "API host (e.g. localhost:8080)")
	flag.StringVar(&basePath, "basePath", "/", "API base path")
	flag.BoolVar(&showVersion, "v", false, "Show version")
	flag.BoolVar(&quiet, "q", false, "Quiet mode")
	flag.BoolVar(&quiet, "quiet", false, "Quiet mode")
	flag.BoolVar(&generateUI, "ui", true, "Generate Swagger UI HTML")
	flag.StringVar(&exclude, "exclude", "", "Directories to exclude (comma separated)")
	flag.StringVar(&exclude, "x", "", "Directories to exclude (shorthand)")
	flag.BoolVar(&parseVendor, "parseVendor", false, "Parse vendor directory")
	flag.BoolVar(&parseDeps, "parseDependency", false, "Parse external dependencies")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `swaggo - Generate OpenAPI docs from Gin handlers

Usage:
  swaggo [flags]

Examples:
  # Scan entire project
  swaggo -d ./myproject -t "My API"

  # Scan from specific entry (recommended for monorepo/microservices)
  swaggo -d . -e cmd/api/main.go -o docs/api
  swaggo -d . -e cmd/admin/main.go -o docs/admin

  # Exclude directories
  swaggo -d . -x test,mock,scripts

Flags:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Entry Mode (-e):
  When -entry is specified, swaggo only parses packages that are
  imported (directly or transitively) from the entry file.
  This is useful for monorepos with multiple services.

  Without -entry, swaggo scans all .go files in the directory.
`)
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("swaggo %s\n", version)
		os.Exit(0)
	}

	log := func(format string, args ...any) {
		if !quiet {
			fmt.Printf(format, args...)
		}
	}

	log("swaggo %s\n", version)
	log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	if err := os.MkdirAll(output, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	gen := swaggo.New().
		WithTitle(title).
		WithDescription(description).
		WithVersion(apiVersion).
		WithHost(host).
		WithBasePath(basePath)

	if exclude != "" {
		excludeDirs := strings.Split(exclude, ",")
		for i := range excludeDirs {
			excludeDirs[i] = strings.TrimSpace(excludeDirs[i])
		}
		gen.WithExclude(excludeDirs...)
	}

	gen.SetParseVendor(parseVendor)
	gen.SetParseDependency(parseDeps)

	absDir, _ := filepath.Abs(dir)

	if entry != "" {
		// Entry mode: 從入口追蹤 import
		entryPath := filepath.Join(absDir, entry)
		if _, err := os.Stat(entryPath); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Entry file not found: %s\n", entryPath)
			os.Exit(1)
		}
		log("Entry: %s\n", entry)
		log("Root:  %s\n", absDir)

		if err := gen.ParseFromEntry(entryPath, absDir); err != nil {
			fmt.Fprintf(os.Stderr, "Parse error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Full scan mode
		log("Parsing: %s\n", absDir)

		if err := gen.ParseSource(dir); err != nil {
			fmt.Fprintf(os.Stderr, "Parse error: %v\n", err)
			os.Exit(1)
		}
	}

	stats := gen.Stats()
	log("Found %d routes\n", stats.Routes)
	log("Found %d handlers\n", stats.Handlers)
	log("Found %d type definitions\n", stats.Types)
	log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	spec, err := gen.Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Generate error: %v\n", err)
		os.Exit(1)
	}

	outputBase := filepath.Join(output, "openapi")

	if format == "json" || format == "both" {
		data, err := spec.ToJSON()
		if err != nil {
			fmt.Fprintf(os.Stderr, "JSON serialize error: %v\n", err)
			os.Exit(1)
		}
		outPath := outputBase + ".json"
		if err := os.WriteFile(outPath, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Write JSON error: %v\n", err)
			os.Exit(1)
		}
		log("Generated %s (%d bytes)\n", outPath, len(data))
	}

	if format == "yaml" || format == "both" {
		data, err := spec.ToYAML()
		if err != nil {
			fmt.Fprintf(os.Stderr, "YAML serialize error: %v\n", err)
			os.Exit(1)
		}
		outPath := outputBase + ".yaml"
		if err := os.WriteFile(outPath, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Write YAML error: %v\n", err)
			os.Exit(1)
		}
		log("Generated %s (%d bytes)\n", outPath, len(data))
	}

	if generateUI {
		html := strings.ReplaceAll(swaggerUIHTML, "{{TITLE}}", title)
		uiPath := filepath.Join(output, "index.html")
		if err := os.WriteFile(uiPath, []byte(html), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Write Swagger UI error: %v\n", err)
			os.Exit(1)
		}
		log("Generated %s\n", uiPath)
	}

	log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	log("Done! %d endpoints generated\n", len(spec.Paths))

	if !quiet && len(spec.Paths) > 0 {
		log("\nEndpoints:\n")
		for path, item := range spec.Paths {
			if item.Get != nil {
				log("  GET    %s\n", path)
			}
			if item.Post != nil {
				log("  POST   %s\n", path)
			}
			if item.Put != nil {
				log("  PUT    %s\n", path)
			}
			if item.Delete != nil {
				log("  DELETE %s\n", path)
			}
			if item.Patch != nil {
				log("  PATCH  %s\n", path)
			}
		}
	}
}
