# ct - File Extension Counter

`ct` is an efficient command-line tool for counting the number of file extensions in a specified directory. It supports concurrent processing, multi-threading optimization, and flexible handling of hidden files and subdirectories. Written in Go, the program includes built-in support for composite extensions (e.g., `.tar.gz`) and uses a Trie data structure for fast extension matching.

## Features

- **File Extension Counting**: Quickly calculates the frequency of file extensions in a directory.
- **Hidden File Support**: Use the `-a` flag to include hidden files (starting with `.`).
- **Scope Limitation**: Use the `-c` flag to restrict counting to the current directory (excluding subdirectories).
- **Performance Timing**: Use the `-t` flag to display execution time.
- **Concurrency Optimization**: Leverages multi-threading and a sharded counter (`ShardedCounter`) for improved performance.
- **Composite Extension Support**: Matches complex extensions via the embedded `composite_exts.txt` file.

## Installation

### Prerequisites
- Go 1.16 or higher (latest version recommended).

### Build and Install
1. Clone or download the repository:
   ```bash
   git clone <repository-url>
   cd ct
   ```
2. Build the program:
   ```bash
   go build -o ct
   ```
3. (Optional) Move the binary to a system path:
   ```bash
   sudo mv ct /usr/local/bin/
   ```

After completion, you can run the `ct` command from any directory.

## Usage

### Command Syntax
```
ct [flags] [directory]
```

- `[flags]`: Optional flags, which can be combined (e.g., `-at`).
- `[directory]`: Target directory, defaults to the current directory (`.`).

### Supported Flags
| Flag | Description                           |
|------|---------------------------------------|
| `-a` | Include hidden files (starting with `.`) |
| `-c` | Count only files in the current directory (no recursion) |
| `-t` | Show execution time (in milliseconds) |

Flags can be combined, e.g., `-at` enables both `-a` and `-t`.

### Examples

Assuming the current directory structure is:
```
.
├── file.txt
├── .hidden.txt
├── subdir
│   └── subfile.txt
├── .hiddendir
│   └── hiddenfile.txt
```

1. **Default Scan (current directory and subdirectories, non-hidden files)**:
   ```bash
   ./ct
   ```
   Output:
   ```
    2 .txt
   Total files: 2
   ```

2. **Include Hidden Files and Show Time**:
   ```bash
   ./ct -at
   ```
   Output:
   ```
    3 .txt
    1 .hidden.txt
   Total files: 4
   Execution time: 12.345 ms
   ```

3. **Count Only Current Directory with All Files**:
   ```bash
   ./ct -cta
   ```
   Output:
   ```
    1 .txt
    1 .hidden.txt
   Total files: 2
   Execution time: 10.234 ms
   ```

4. **Scan a Specific Directory**:
   ```bash
   ./ct -a /path/to/dir
   ```
   Output: Depends on the contents of the specified directory.

### Error Handling
- Unknown flags (e.g., `-x`) will trigger:
  ```
  Unknown flag: -x
  ```
- Multiple directory arguments will result in:
  ```
  Too many arguments
  ```

## Technical Details

- **Concurrency Design**: Uses Go goroutines and channels for multi-threaded file processing, with worker count dynamically adjusted based on CPU cores.
- **Sharded Counter**: Employs `ShardedCounter` to minimize concurrent write conflicts and enhance counting performance.
- **Batch Processing**: Dynamically calculates batch size (between `MinBatchSize` and `MaxBatchSize`) to reduce memory allocation overhead.
- **Extension Matching**: Utilizes a Trie data structure for fast matching of composite extensions, configured via `composite_exts.txt`.

### composite_exts.txt
This file is embedded in the program and defines supported composite extensions (e.g., `.tar.gz`). You can modify it and recompile to customize extension support.

## Performance Optimizations

- **Batch Processing**: Files are processed in batches, with size dynamically adjusted based on CPU cores (50-200).
- **Channel Buffering**: File channel buffer size is `numWorkers * 4` to reduce blocking.
- **Memory Reuse**: Batch slices are reused after processing to minimize memory allocations.

## Contributing

Issues and Pull Requests are welcome! Please ensure code adheres to Go conventions and include necessary tests.

## License

This project is licensed under the MIT License. See the `LICENSE` file for details.