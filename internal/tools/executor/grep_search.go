package executortools

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"botson/internal/executor"
	"botson/internal/sandbox"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

type GrepSearchArgs struct {
	Query           string   `json:"query"`
	Path            string   `json:"path,omitempty"`
	IsRegex         bool     `json:"is_regex,omitempty"`
	CaseInsensitive bool     `json:"case_insensitive,omitempty"`
	Includes        []string `json:"includes,omitempty"`
}

type GrepMatch struct {
	File        string `json:"file"`
	LineNumber  int    `json:"line_number"`
	LineContent string `json:"line_content"`
}

func MakeGrepSearchTool(mgr *executor.Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "grep_search",
		Description: "Finds exact pattern or regex matches in the active environment's workspace files. Returns line numbers and contents.",
	}, func(ctx agent.Context, args GrepSearchArgs) ([]GrepMatch, error) {
		query := args.Query
		if query == "" {
			return nil, fmt.Errorf("query cannot be empty")
		}

		target := mgr.GetActiveTarget()
		var baseDir string

		if sb, ok := target.(*sandbox.Sandbox); ok {
			baseDir = filepath.Clean(filepath.Join(sb.RootfsPath, args.Path))
		} else {
			// Host OS mode: resolve relative to working directory
			cleanedPath := filepath.Clean(args.Path)
			if filepath.IsAbs(cleanedPath) {
				baseDir = cleanedPath
			} else {
				baseDir = filepath.Clean(filepath.Join(".", cleanedPath))
			}
		}

		// Compile regex or match criteria
		var re *regexp.Regexp
		var err error
		if args.IsRegex {
			pattern := query
			if args.CaseInsensitive {
				pattern = "(?i)" + pattern
			}
			re, err = regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid regex query: %w", err)
			}
		} else if args.CaseInsensitive {
			query = strings.ToLower(query)
		}

		var matches []GrepMatch
		maxMatches := 50

		err = filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // Skip inaccessible files
			}
			if d.IsDir() {
				name := d.Name()
				if name == ".git" || name == "build" || name == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}

			// Filter by Includes globs if specified
			if len(args.Includes) > 0 {
				matchedGlob := false
				for _, pattern := range args.Includes {
					if matched, _ := filepath.Match(pattern, d.Name()); matched {
						matchedGlob = true
						break
					}
				}
				if !matchedGlob {
					return nil
				}
			}

			// Open and scan file
			file, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer file.Close()

			// Exclude binary files
			buf := make([]byte, 512)
			n, _ := file.Read(buf)
			if n > 0 {
				for i := 0; i < n; i++ {
					if buf[i] == 0 {
						return nil
					}
				}
			}
			_, _ = file.Seek(0, 0) // reset reader

			scanner := bufio.NewScanner(file)
			lineNum := 0
			for scanner.Scan() {
				lineNum++
				line := scanner.Text()

				isMatch := false
				if args.IsRegex {
					isMatch = re.MatchString(line)
				} else {
					if args.CaseInsensitive {
						isMatch = strings.Contains(strings.ToLower(line), query)
					} else {
						isMatch = strings.Contains(line, query)
					}
				}

				if isMatch {
					relPath, err := filepath.Rel(baseDir, path)
					if err != nil {
						relPath = path
					}
					matches = append(matches, GrepMatch{
						File:        relPath,
						LineNumber:  lineNum,
						LineContent: strings.TrimSpace(line),
					})

					if len(matches) >= maxMatches {
						return fmt.Errorf("limit_reached")
					}
				}
			}
			return nil
		})

		if err != nil && err.Error() != "limit_reached" {
			return nil, fmt.Errorf("failed to scan workspace: %w", err)
		}

		return matches, nil
	})
}
