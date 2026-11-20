// The Go files in this folder (sample inputs and the generated sandbox) are test data, not part of jelly-diff.
// This separate module keeps `go build ./...`, `go vet ./...` and `go test ./...` of the parent module away from
// them: some samples declare the same functions twice and the sandbox contains files with merge conflict markers.
module example.com/jelly-diff-playground

go 1.24.0
