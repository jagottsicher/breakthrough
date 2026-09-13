package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jagottsicher/breakthrough/internal/contextindex"
)

func main() {
	root := flag.String("root", ".", "repository root to index")
	output := flag.String("output", "docs/code-index.json", "path for the generated JSON index")
	flag.Parse()

	rootPath, err := filepath.Abs(*root)
	if err != nil {
		log.Fatal(err)
	}
	outputPath, err := filepath.Abs(*output)
	if err != nil {
		log.Fatal(err)
	}

	index, err := contextindex.Build(rootPath, contextindex.Options{Exclude: []string{outputPath}})
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := contextindex.Write(outputPath, index); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s (%d files, %d packages, %d symbols, Merkle root %s)\n", *output, len(index.Files), len(index.Packages), len(index.Symbols), index.MerkleRoot)
}
