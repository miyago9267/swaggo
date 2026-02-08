# Contributing to swaggo

Thank you for your interest in contributing to swaggo!

## How to Contribute

### Reporting Issues

- Check existing issues before creating a new one
- Include Go version, swaggo version, and minimal reproduction code
- Describe expected vs actual behavior

### Pull Requests

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make your changes
4. Run tests: `go test ./...`
5. Commit with clear message: `git commit -m "feat: add new feature"`
6. Push and create PR

### Commit Convention

We use [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` New feature
- `fix:` Bug fix
- `docs:` Documentation only
- `refactor:` Code refactoring
- `test:` Adding tests
- `chore:` Maintenance tasks

### Development Setup

```bash
git clone https://github.com/miyago9267/swaggo.git
cd swaggo
go mod download
go build ./cmd/swaggo
```

### Running Tests

```bash
go test ./...
go test -v ./pkg/swaggo/...
```

## Code Style

- Follow standard Go conventions
- Run `go fmt` before committing
- Keep functions focused and small
- Add comments for exported types and functions

## Questions?

Open an issue with the `question` label.
