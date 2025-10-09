# Testing Documentation

## Overview

goscribe includes comprehensive unit tests to ensure reliability and correctness of core functionality.

## Test Statistics

- **Total Test Cases**: 23 (8 test functions with 15 sub-tests)
- **Code Coverage**: ~21.5%
- **All Tests**: PASSING ✅

## Test Functions

### 1. TestFindAction (3 sub-tests)
Tests the action lookup functionality:
- ✅ Find existing action
- ✅ Action not found (returns nil)
- ✅ Empty action ID handling

### 2. TestValidateConfig (12 sub-tests)
Validates configuration file validation logic:
- ✅ Valid config accepted
- ✅ Empty config rejected
- ✅ Missing ID field detected
- ✅ Missing name field detected
- ✅ Missing type field detected
- ✅ Missing prompt field detected
- ✅ Missing model field detected
- ✅ Duplicate IDs detected
- ✅ Invalid type rejected
- ✅ Temperature too low rejected
- ✅ Temperature too high rejected
- ✅ Invalid max tokens rejected

### 3. TestLoadConfigActions
Tests loading and parsing YAML config files:
- ✅ Valid config loaded successfully
- ✅ API key extracted correctly
- ✅ Actions parsed correctly
- ✅ All action fields validated

### 4. TestLoadConfigActionsInvalid
Tests error handling for invalid configs:
- ✅ Invalid config rejected with error

### 5. TestLoadConfigActionsNonExistent
Tests error handling for missing files:
- ✅ Non-existent file returns error

### 6. TestStoreAPIKey
Tests API key storage functionality:
- ✅ API key stored to config file
- ✅ Existing config preserved
- ✅ API key retrieved after storage

### 7. TestCreateDefaultConfig
Tests default config generation:
- ✅ Default config file created
- ✅ Config directory created if missing
- ✅ Default config is valid and loadable
- ✅ Contains all built-in actions

### 8. TestGetDefaultConfigContent
Tests default config template:
- ✅ Returns non-empty content
- ✅ Contains required sections
- ✅ Contains expected action IDs

## Running Tests

### All Tests (Verbose)
```bash
make test
```

### Quick Test Run
```bash
make test-short
```

### Coverage Report
```bash
make test-coverage
```
This generates:
- `coverage.out` - Coverage data file
- `coverage.html` - HTML coverage report

### Go Test Commands
```bash
# Run all tests
go test -v ./...

# Run specific test
go test -v -run TestFindAction

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Test Coverage Areas

### ✅ Covered
- Configuration validation
- Action management (find, list)
- Config file loading and parsing
- API key storage
- Default config generation
- Error handling for invalid configs

### ⚠️ Not Covered (Future Work)
- OpenAI API interactions (transcribeAudio, processWithOpenAI)
- Main function CLI flow
- File I/O operations for transcripts
- Command-line flag parsing
- Config reset functionality

## Test Best Practices

1. **Isolation**: Tests use `t.TempDir()` for file operations
2. **Environment**: Tests backup and restore HOME environment variable
3. **Coverage**: Each test validates both success and error cases
4. **Assertions**: Clear error messages on failures
5. **Table-Driven**: Uses sub-tests for comprehensive coverage

## Continuous Integration

Tests can be integrated into CI/CD pipelines:

```yaml
# Example GitHub Actions workflow
- name: Run tests
  run: make test

- name: Generate coverage
  run: make test-coverage

- name: Upload coverage
  uses: codecov/codecov-action@v3
  with:
    files: ./coverage.out
```

## Adding New Tests

When adding new functionality:

1. Create test function in `main_test.go`
2. Use table-driven tests for multiple cases
3. Test both success and error paths
4. Use `t.TempDir()` for temporary files
5. Run `make test-coverage` to verify coverage

Example:
```go
func TestNewFeature(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"valid input", "test", "expected", false},
        {"invalid input", "", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := NewFeature(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
            }
            if got != tt.want {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

## Test Maintenance

- Run tests before committing: `make test`
- Update tests when changing functionality
- Maintain >20% code coverage
- Keep test execution time under 1 second
- Document complex test scenarios

## Known Limitations

1. **API Mocking**: OpenAI API calls are not mocked (integration tests needed)
2. **CLI Testing**: Main function flow not tested (requires CLI testing framework)
3. **Coverage**: Currently ~21.5%, target should be >50%

## Future Testing Goals

- [ ] Add integration tests for OpenAI API
- [ ] Increase coverage to >50%
- [ ] Add benchmark tests for performance
- [ ] Add CLI integration tests
- [ ] Add test for transcript file processing
- [ ] Mock HTTP client for API testing
