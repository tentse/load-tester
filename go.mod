module github.com/tentse/load-tester

go 1.26.3

// [should-fix] goleak is imported directly by loadtest/run_test.go, so it is not indirect.
// `go mod tidy -diff` removes this marker and adds the required transitive checksums to go.sum.
require go.uber.org/goleak v1.3.0 // indirect
