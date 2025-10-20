# Command Directory

This directory contains the main entry points for the different binaries in this project, following Go's standard project layout.

## Structure

```
cmd/
├── home-ci/        # Main home-ci application
│   └── main.go
└── home-ci-e2e/    # End-to-end test harness
    └── main.go
```

## Binaries

### `home-ci`
The main CI monitoring application that watches git repositories and runs tests automatically.

**Build:**
```bash
go build -o home-ci ./cmd/home-ci
# or
make build
```

### `home-ci-e2e`
The end-to-end test harness used to validate home-ci functionality by creating test repositories and simulating development activity.

**Build:**
```bash
go build -o home-ci-e2e ./cmd/home-ci-e2e
# or
make build-test
```

## Usage

See the main [README.md](../README.md) for usage instructions and [test/README.md](../test/README.md) for testing information.