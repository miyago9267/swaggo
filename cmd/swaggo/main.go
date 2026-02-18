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

	flag.StringVar(&dir, "dir", ".", "")
	flag.StringVar(&dir, "d", ".", "")
	flag.StringVar(&entry, "entry", "", "")
	flag.StringVar(&entry, "e", "", "")
	flag.StringVar(&output, "output", "docs", "")
	flag.StringVar(&output, "o", "docs", "")
	flag.StringVar(&format, "format", "both", "")
	flag.StringVar(&title, "title", "API Documentation", "")
	flag.StringVar(&title, "t", "API Documentation", "")
	flag.StringVar(&description, "desc", "", "")
	flag.StringVar(&apiVersion, "api-version", "1.0.0", "")
	flag.StringVar(&host, "host", "", "")
	flag.StringVar(&basePath, "base-path", "/", "")
	flag.BoolVar(&showVersion, "version", false, "")
	flag.BoolVar(&showVersion, "v", false, "")
	flag.BoolVar(&quiet, "quiet", false, "")
	flag.BoolVar(&quiet, "q", false, "")
	flag.BoolVar(&generateUI, "ui", true, "")
	flag.StringVar(&exclude, "exclude", "", "")
	flag.StringVar(&exclude, "x", "", "")
	flag.BoolVar(&parseVendor, "parse-vendor", false, "")
	flag.BoolVar(&parseDeps, "parse-deps", false, "")

	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `swaggo - Generate OpenAPI docs from Gin handlers

Usage:
  swaggo [flags]

Examples:
  swaggo -d ./myproject -t "My API"
  swaggo -d . -e cmd/api/main.go -o docs/api
  swaggo -d . -x test,mock

Flags:
  -d, --dir <path>          Project root directory (default ".")
  -e, --entry <file>        Entry file for import tracing (e.g. cmd/api/main.go)
  -o, --output <path>       Output directory (default "docs")
  -t, --title <string>      API title (default "API Documentation")
      --desc <string>       API description
      --api-version <ver>   API version (default "1.0.0")
      --host <host>         API host (e.g. localhost:8080)
      --base-path <path>    API base path (default "/")
      --format <fmt>        Output format: json, yaml, both (default "both")
      --ui                  Generate Swagger UI HTML (default true)
  -x, --exclude <dirs>      Directories to exclude (comma separated)
      --parse-vendor        Parse vendor directory
      --parse-deps          Parse external dependencies
  -q, --quiet               Quiet mode
  -v, --version             Show version

Entry Mode:
  When --entry is specified, only packages imported from that entry file
  are parsed. Useful for monorepos with multiple services.
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
