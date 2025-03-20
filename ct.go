package main

import (
	"bufio"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed composite_exts.txt
var configFile embed.FS

// Constants
const (
	NoExtension       = "(no extension)"
	MinBatchSize      = 50
	MaxBatchSize      = 200
	ShardCountBase    = 2
	DefaultBufferSize = 1024
)

// TrieNode represents a node in the compressed Trie for extension matching
type TrieNode struct {
	prefix   string
	children map[rune]*TrieNode
	isEnd    bool
}

// ExtensionCount holds an extension and its occurrence count
type ExtensionCount struct {
	Extension string
	Count     int32
}

// ShardedCounter manages concurrent counting of extensions
type ShardedCounter struct {
	shards []*sync.Map
}

func main() {
	// Define command-line flags
	currentOnly := false
	showTime := false
	showHidden := false
	showVersion := false

	// Manually parse command-line arguments
	args := os.Args[1:] // Skip program name
	dir := "."          // Default directory is current directory
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			for _, char := range arg[1:] {
				switch char {
				case 'v', 'V':
					showVersion = true
				case 'c':
					currentOnly = true
				case 't':
					showTime = true
				case 'a':
					showHidden = true
				default:
					fmt.Fprintf(os.Stderr, "Unknown flag: -%c\n", char)
					os.Exit(1)
				}
			}
		} else {
			// First non-flag argument is considered the directory
			if dir == "." {
				dir = arg
			} else {
				fmt.Fprintf(os.Stderr, "Too many arguments\n")
				os.Exit(1)
			}
		}
	}

	if showVersion {
		fmt.Printf("ct version %s\n", version)
		os.Exit(1)
	}

	// Record start time if -t is specified
	var startTime time.Time
	if showTime {
		startTime = time.Now()
	}

	// Load composite extensions into Trie
	trie, err := loadExtensionsTrie()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading extensions: %v\n", err)
		os.Exit(1)
	}

	// Count extensions and get total files
	counts, total, err := countExtensions(dir, trie, currentOnly, showHidden)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error counting extensions: %v\n", err)
		os.Exit(1)
	}

	// Sort and display results
	displayResults(counts, total)

	// Show execution time if -t is specified
	if showTime {
		executionTime := time.Since(startTime)
		fmt.Printf("Execution time: %.3f ms\n", float64(executionTime.Nanoseconds())/1e6)
	}
}

// NewShardedCounter initializes a sharded counter with a given number of shards
func NewShardedCounter(shardCount int) *ShardedCounter {
	shards := make([]*sync.Map, shardCount)
	for i := range shards {
		shards[i] = &sync.Map{}
	}
	return &ShardedCounter{shards: shards}
}

// shardIndex computes the shard index for a given key using FNV-32 hash
func (sc *ShardedCounter) shardIndex(key string) int {
	return int(fnv32(key) % uint32(len(sc.shards)))
}

// Increment safely increments the count for an extension
func (sc *ShardedCounter) Increment(key string) {
	shard := sc.shards[sc.shardIndex(key)]
	if v, loaded := shard.LoadOrStore(key, int32(1)); loaded {
		// Use atomic.AddInt32 instead of CAS loop
		oldValue := v.(int32)
		newValue := oldValue + 1
		shard.Store(key, newValue)
	}
}

// ToSlice converts the sharded map to a slice of ExtensionCount
func (sc *ShardedCounter) ToSlice() []ExtensionCount {
	result := make([]ExtensionCount, 0, DefaultBufferSize) // Pre-allocate capacity
	for _, shard := range sc.shards {
		shard.Range(func(key, value interface{}) bool {
			result = append(result, ExtensionCount{
				Extension: key.(string),
				Count:     value.(int32),
			})
			return true
		})
	}
	return result
}

// fnv32 computes a 32-bit FNV-1a hash for sharding
func fnv32(s string) uint32 {
	const prime32 = 16777619
	hash := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		hash = (hash ^ uint32(s[i])) * prime32
	}
	return hash
}

// loadExtensionsTrie builds a Trie from the embedded config file
func loadExtensionsTrie() (*TrieNode, error) {
	file, err := configFile.Open("composite_exts.txt")
	if err != nil {
		return nil, fmt.Errorf("failed to open config: %v", err)
	}
	defer file.Close()

	root := newTrieNode("")
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		root.insert(line)
		count++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading config: %v", err)
	}
	if count == 0 {
		return nil, fmt.Errorf("config file is empty or invalid")
	}
	return root, nil
}

// newTrieNode creates a new Trie node with the given prefix
func newTrieNode(prefix string) *TrieNode {
	return &TrieNode{
		prefix:   prefix,
		children: make(map[rune]*TrieNode),
	}
}

// insert adds an extension to the Trie
func (n *TrieNode) insert(ext string) {
	current := n
	for i := 0; i < len(ext); {
		if len(current.children) == 0 && !current.isEnd {
			current.prefix = ext[i:]
			current.isEnd = true
			return
		}

		prefixLen := commonPrefixLength(ext[i:], current.prefix)
		if prefixLen == 0 {
			char := rune(ext[i])
			if _, exists := current.children[char]; !exists {
				current.children[char] = newTrieNode("")
			}
			current = current.children[char]
			i++
		} else if prefixLen < len(current.prefix) {
			current.split(prefixLen, ext[i+prefixLen:])
			return
		} else if prefixLen == len(ext[i:]) {
			current.isEnd = true
			return
		} else {
			i += prefixLen
			char := rune(ext[i])
			if _, exists := current.children[char]; !exists {
				current.children[char] = newTrieNode("")
			}
			current = current.children[char]
			i++
		}
	}
}

// split splits a Trie node at the given prefix length
func (n *TrieNode) split(prefixLen int, remaining string) {
	oldPrefix := n.prefix
	n.prefix = oldPrefix[:prefixLen]
	newChild := &TrieNode{
		prefix:   oldPrefix[prefixLen:],
		children: n.children,
		isEnd:    n.isEnd,
	}
	n.children = make(map[rune]*TrieNode)
	n.children[rune(oldPrefix[prefixLen])] = newChild
	n.isEnd = false

	if len(remaining) > 0 {
		char := rune(remaining[0])
		n.children[char] = newTrieNode(remaining[1:])
		n.children[char].isEnd = true
	} else {
		n.isEnd = true
	}
}

// commonPrefixLength returns the length of the common prefix between two strings
func commonPrefixLength(a, b string) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return minLen
}

// getExtension extracts the extension from a filename
func getExtension(filename string, trie *TrieNode) string {
	// Try composite extensions via Trie
	longestMatch := trie.findLongestMatch(filename)
	if longestMatch != "" {
		return longestMatch
	}

	// Fallback to single extension
	if dotIndex := strings.LastIndex(filename, "."); dotIndex != -1 && dotIndex < len(filename)-1 {
		return filename[dotIndex:]
	}
	return NoExtension
}

// findLongestMatch finds the longest matching extension in the Trie
func (n *TrieNode) findLongestMatch(filename string) string {
	current := n
	start := 0
	longestMatch := ""
	for i := 0; i < len(filename); {
		if len(current.prefix) > 0 {
			if strings.HasPrefix(filename[i:], current.prefix) {
				i += len(current.prefix)
				if current.isEnd && (i == len(filename) || filename[i] == '.') {
					longestMatch = filename[start:i]
				}
				if i >= len(filename) {
					break
				}
				if next, exists := current.children[rune(filename[i])]; exists {
					current = next
					i++
					continue
				}
				break
			} else {
				break
			}
		}
		if i >= len(filename) {
			break
		}
		char := rune(filename[i])
		if next, exists := current.children[char]; exists {
			current = next
			i++
			start = i - len(current.prefix)
		} else {
			break
		}
	}
	return longestMatch
}

// countExtensions counts extensions in the specified directory
func countExtensions(dir string, trie *TrieNode, currentOnly, showHidden bool) ([]ExtensionCount, int32, error) {
	// Calculate optimal worker count and batch size based on CPU cores
	numWorkers := runtime.NumCPU()
	batchSize := calculateOptimalBatchSize(numWorkers)

	counter := NewShardedCounter(numWorkers * ShardCountBase)
	var total int32

	fileChan := make(chan string, numWorkers*4) // Increase buffer size
	var wg sync.WaitGroup

	// Start worker pool
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go processFiles(fileChan, trie, counter, &wg, batchSize)
	}

	// Traverse directory in a separate goroutine
	var walkWg sync.WaitGroup
	walkWg.Add(1)
	errChan := make(chan error, 1) // Error channel

	go func() {
		defer walkWg.Done()
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				// Log error but continue processing
				fmt.Fprintf(os.Stderr, "Warning: Error accessing %s: %v\n", path, err)
				return nil
			}

			// Extract base name to check if it's hidden
			base := filepath.Base(path)

			// Skip hidden files and directories unless -a is specified, but allow root directory
			if !showHidden && strings.HasPrefix(base, ".") && path != dir {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			if d.IsDir() {
				if currentOnly && path != dir {
					return filepath.SkipDir
				}
				return nil
			}

			atomic.AddInt32(&total, 1)
			select {
			case fileChan <- path:
				// Successfully sent to channel
			default:
				// Channel is full, process some files
				fileChan <- path
			}
			return nil
		})

		if err != nil {
			select {
			case errChan <- err:
				// Successfully sent error
			default:
				// Error channel is full, print error
				fmt.Fprintf(os.Stderr, "Critical error: %v\n", err)
			}
		}
	}()

	// Wait for directory traversal to complete and close channel
	walkWg.Wait()
	close(fileChan)

	// Wait for all workers to finish
	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		return nil, 0, err
	}

	return counter.ToSlice(), total, nil
}

// calculateOptimalBatchSize calculates the optimal batch size based on CPU cores
func calculateOptimalBatchSize(numCPU int) int {
	// Simply adjust batch size based on CPU count
	batchSize := numCPU * 10

	// Ensure batch size is within a reasonable range
	if batchSize < MinBatchSize {
		return MinBatchSize
	}
	if batchSize > MaxBatchSize {
		return MaxBatchSize
	}
	return batchSize
}

// processFiles handles file batches and updates the counter
func processFiles(fileChan <-chan string, trie *TrieNode, counter *ShardedCounter, wg *sync.WaitGroup, batchSize int) {
	defer wg.Done()
	batch := make([]string, 0, batchSize)

	for path := range fileChan {
		batch = append(batch, path)
		if len(batch) >= batchSize {
			processBatch(batch, trie, counter)
			// Reuse slice to reduce memory allocation
			batch = batch[:0]
		}
	}

	// Process remaining files
	if len(batch) > 0 {
		processBatch(batch, trie, counter)
	}
}

// processBatch processes a batch of files and updates the counter
func processBatch(batch []string, trie *TrieNode, counter *ShardedCounter) {
	for _, path := range batch {
		counter.Increment(getExtension(filepath.Base(path), trie))
	}
}

// displayResults sorts and prints the extension counts
func displayResults(counts []ExtensionCount, total int32) {
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Extension == NoExtension {
			return false
		}
		if counts[j].Extension == NoExtension {
			return true
		}
		return counts[i].Extension < counts[j].Extension
	})

	width := len(fmt.Sprintf("%d", total))
	for _, ec := range counts {
		fmt.Printf("%*d %s\n", width+1, ec.Count, ec.Extension)
	}
	fmt.Printf("Total files: %d\n", total)
}
