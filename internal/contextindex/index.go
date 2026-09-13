// Package contextindex builds a deterministic source-context index for the repository.
package contextindex

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Options controls which repository files are excluded from the index.
type Options struct {
	Exclude []string
}

// Index is the complete deterministic context index.
type Index struct {
	Version    int       `json:"version"`
	Files      []File    `json:"files"`
	Packages   []Package `json:"packages"`
	Symbols    []Symbol  `json:"symbols"`
	MerkleRoot string    `json:"merkle_root"`
}

// File describes one indexed repository file.
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Package describes a Go package and its direct imports.
type Package struct {
	ImportPath string   `json:"import_path"`
	Directory  string   `json:"directory"`
	Name       string   `json:"name"`
	Files      []string `json:"files"`
	Imports    []string `json:"imports"`
}

// Symbol describes a declaration found in a Go source file.
type Symbol struct {
	Package  string `json:"package"`
	File     string `json:"file"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Receiver string `json:"receiver,omitempty"`
	Line     int    `json:"line"`
}

// Build walks root and returns a deterministic source-context index.
func Build(root string, options Options) (Index, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Index{}, err
	}

	excluded := map[string]struct{}{}
	for _, path := range options.Exclude {
		rel, err := filepath.Rel(root, path)
		if err == nil {
			excluded[filepath.ToSlash(rel)] = struct{}{}
		}
	}

	modulePath, err := readModulePath(filepath.Join(root, "go.mod"))
	if err != nil && !os.IsNotExist(err) {
		return Index{}, err
	}

	index := Index{Version: 1}
	packages := map[string]*packageBuilder{}
	fset := token.NewFileSet()

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel != "." && excludedDirectory(rel) {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if _, skip := excluded[rel]; skip || excludedDirectory(filepath.Dir(rel)) {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		index.Files = append(index.Files, File{Path: rel, Size: info.Size(), SHA256: digest})

		if filepath.Ext(path) != ".go" {
			return nil
		}
		astFile, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		directory := filepath.ToSlash(filepath.Dir(rel))
		if directory == "." {
			directory = ""
		}
		builder := packages[directory]
		if builder == nil {
			builder = &packageBuilder{directory: directory, name: astFile.Name.Name, imports: map[string]struct{}{}}
			packages[directory] = builder
		}
		builder.files = append(builder.files, rel)
		for _, importSpec := range astFile.Imports {
			importPath, err := strconv.Unquote(importSpec.Path.Value)
			if err == nil {
				builder.imports[importPath] = struct{}{}
			}
		}
		collectSymbols(&index, astFile, fset, rel, packageImportPath(modulePath, directory))
		return nil
	})
	if err != nil {
		return Index{}, err
	}

	sort.Slice(index.Files, func(i, j int) bool { return index.Files[i].Path < index.Files[j].Path })
	for _, builder := range packages {
		files := append([]string(nil), builder.files...)
		sort.Strings(files)
		imports := make([]string, 0, len(builder.imports))
		for importPath := range builder.imports {
			imports = append(imports, importPath)
		}
		sort.Strings(imports)
		index.Packages = append(index.Packages, Package{
			ImportPath: packageImportPath(modulePath, builder.directory),
			Directory:  builder.directory,
			Name:       builder.name,
			Files:      files,
			Imports:    imports,
		})
	}
	sort.Slice(index.Packages, func(i, j int) bool { return index.Packages[i].ImportPath < index.Packages[j].ImportPath })
	sort.Slice(index.Symbols, func(i, j int) bool {
		if index.Symbols[i].File != index.Symbols[j].File {
			return index.Symbols[i].File < index.Symbols[j].File
		}
		return index.Symbols[i].Line < index.Symbols[j].Line
	})
	index.MerkleRoot = merkleRoot(index.Files)
	return index, nil
}

// Write serializes index as stable, human-readable JSON.
func Write(path string, index Index) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

type packageBuilder struct {
	directory string
	name      string
	files     []string
	imports   map[string]struct{}
}

func collectSymbols(index *Index, file *ast.File, fset *token.FileSet, path, packagePath string) {
	ast.Inspect(file, func(node ast.Node) bool {
		symbol := Symbol{Package: packagePath, File: path}
		switch declaration := node.(type) {
		case *ast.FuncDecl:
			symbol.Name = declaration.Name.Name
			symbol.Kind = "function"
			if declaration.Recv != nil && len(declaration.Recv.List) > 0 {
				symbol.Kind = "method"
				symbol.Receiver = nodeText(fset, declaration.Recv.List[0].Type)
			}
		case *ast.TypeSpec:
			symbol.Name, symbol.Kind = declaration.Name.Name, "type"
		default:
			return true
		}
		symbol.Line = fset.Position(node.Pos()).Line
		index.Symbols = append(index.Symbols, symbol)
		return true
	})
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || (general.Tok != token.CONST && general.Tok != token.VAR) {
			continue
		}
		kind := strings.ToLower(general.Tok.String())
		for _, specification := range general.Specs {
			values := specification.(*ast.ValueSpec)
			for _, name := range values.Names {
				index.Symbols = append(index.Symbols, Symbol{
					Package: packagePath,
					File:    path,
					Name:    name.Name,
					Kind:    kind,
					Line:    fset.Position(name.Pos()).Line,
				})
			}
		}
	}
}

func nodeText(fset *token.FileSet, node ast.Node) string {
	var output strings.Builder
	if err := format.Node(&output, fset, node); err != nil {
		return ""
	}
	return output.String()
}

func readModulePath(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", scanner.Err()
}

func packageImportPath(modulePath, directory string) string {
	if modulePath == "" {
		return directory
	}
	if directory == "" {
		return modulePath
	}
	return modulePath + "/" + directory
}

func excludedDirectory(path string) bool {
	path = filepath.ToSlash(path)
	if path == "docs/images" || strings.HasPrefix(path, "docs/images/") {
		return true
	}
	for _, component := range strings.Split(path, "/") {
		if component == ".git" || component == ".cache" || component == "vendor" || component == "node_modules" {
			return true
		}
	}
	return false
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func merkleRoot(files []File) string {
	if len(files) == 0 {
		digest := sha256.Sum256(nil)
		return hex.EncodeToString(digest[:])
	}
	level := make([][]byte, len(files))
	for i, file := range files {
		leaf := sha256.Sum256([]byte(file.Path + "\x00" + file.SHA256))
		level[i] = leaf[:]
	}
	for len(level) > 1 {
		next := make([][]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			right := level[i]
			if i+1 < len(level) {
				right = level[i+1]
			}
			combined := append(append([]byte(nil), level[i]...), right...)
			digest := sha256.Sum256(combined)
			next = append(next, digest[:])
		}
		level = next
	}
	return hex.EncodeToString(level[0])
}
