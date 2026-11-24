# Debugger examples

Two small programs to try the debugger (delve and debugpy are installed with the Go and the Python language, see the main README):

- `calc.py`: open it, put a breakpoint on the `return result / len(numbers)` line (`ctrl+b`), press `ctrl+f5`. At the stop look at the
  variables, type `numbers` or `sum(numbers)` into the console, try `tab` or `ctrl+space` on `re`.
- `go/main.go`: a breakpoint in `price()` stops three times (`f9` continues), the call stack shows `total` and `main` above it.
