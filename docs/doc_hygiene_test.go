package docs_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	windowsLocalPathRE = regexp.MustCompile(`(?i)\b[A-Z]:\\(?:Users|Code)\\[^\s` + "`" + `)]+`)
	unixHomePathRE     = regexp.MustCompile(`/(?:Users|home)/[^/\s` + "`" + `]+`)
	dsnWithPasswordRE  = regexp.MustCompile(`(?i)\b(?:postgres(?:ql)?|mysql)://([^:\s` + "`" + `/]+):([^@\s` + "`" + `]+)@([^\s` + "`" + `/]+)`)
	aiArtifactRE       = regexp.MustCompile(`(?i)(contentReference\[oaicite:|oai_citation|citeturn\d+search\d+|grok_card|utm_source=(?:chatgpt\.com|copilot\.com|openai|claude\.ai|perplexity\.ai))`)
)

func TestPublicMarkdownHygiene(t *testing.T) {
	root := repoRoot(t)
	var findings []string

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || name == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(mustRel(t, root, path))
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			lineNo := i + 1
			switch {
			case windowsLocalPathRE.MatchString(line):
				findings = append(findings, formatFinding(rel, lineNo, "local Windows path", line))
			case hasDisallowedUnixHomePath(line):
				findings = append(findings, formatFinding(rel, lineNo, "local Unix home path", line))
			case aiArtifactRE.MatchString(line):
				findings = append(findings, formatFinding(rel, lineNo, "AI citation or tracking artifact", line))
			case dsnWithPasswordRE.MatchString(line) && !isAllowedExampleDSN(line):
				findings = append(findings, formatFinding(rel, lineNo, "credential-bearing DSN without placeholders", line))
			case looksLikeRedisHardDependencyClaim(line):
				findings = append(findings, formatFinding(rel, lineNo, "Redis hard-dependency claim (Redis is optional)", line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) > 0 {
		t.Fatalf("public Markdown hygiene violations:\n%s", strings.Join(findings, "\n"))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root")
		}
		dir = parent
	}
}

func mustRel(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func formatFinding(path string, line int, kind string, text string) string {
	return path + ":" + itoa(line) + ": " + kind + ": " + strings.TrimSpace(text)
}

func isAllowedExampleDSN(line string) bool {
	matches := dsnWithPasswordRE.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		return true
	}
	for _, match := range matches {
		user, password, host := strings.ToLower(match[1]), strings.ToLower(match[2]), strings.ToLower(match[3])
		if strings.Contains(match[0], "<") || strings.Contains(password, "***") {
			continue
		}
		if host == "localhost:5432" && user == "postgres" && password == "test" {
			continue
		}
		if user == "user" && (password == "pass" || password == "password") && strings.HasPrefix(host, "host") {
			continue
		}
		return false
	}
	return true
}

func hasDisallowedUnixHomePath(line string) bool {
	matches := unixHomePathRE.FindAllString(line, -1)
	for _, match := range matches {
		normalized := strings.ToLower(match)
		if normalized == "/home/runner" || normalized == "/home/app" {
			continue
		}
		return true
	}
	return false
}

// looksLikeRedisHardDependencyClaim flags docs that claim Redis is mandatory.
// Optional Redis (#118) is supported via REDIS_URL; single-node installs may
// leave it empty. Claims that Redis is a hard/required runtime dependency are
// incorrect. Optional / fail-open language is allowed.
func looksLikeRedisHardDependencyClaim(line string) bool {
	lower := strings.ToLower(line)
	if !strings.Contains(lower, "redis") {
		return false
	}
	// Explicitly optional / disabled-by-default wording is fine.
	if strings.Contains(lower, "optional") ||
		strings.Contains(lower, "可选") ||
		strings.Contains(lower, "fail-open") ||
		strings.Contains(lower, "fail open") ||
		strings.Contains(lower, "never a hard") ||
		strings.Contains(lower, "not a hard") ||
		strings.Contains(lower, "no hard") ||
		strings.Contains(lower, "not required") ||
		strings.Contains(lower, "not mandatory") ||
		strings.Contains(lower, "empty =") ||
		strings.Contains(lower, "empty disables") ||
		strings.Contains(lower, "leave it empty") ||
		strings.Contains(lower, "default remains") ||
		strings.Contains(lower, "process-local") ||
		strings.Contains(lower, "process local") ||
		strings.Contains(lower, "off by default") ||
		strings.Contains(lower, "by default") ||
		strings.Contains(lower, "redis_url") ||
		strings.Contains(lower, "尚未") ||
		strings.Contains(lower, "没有") ||
		strings.Contains(lower, "无 redis") {
		return false
	}
	// Ban only hard-requirement wording.
	return strings.Contains(lower, "hard dependency") ||
		strings.Contains(lower, "hard-dependency") ||
		strings.Contains(lower, "required dependency") ||
		strings.Contains(lower, "redis is required") ||
		strings.Contains(lower, "must set redis") ||
		strings.Contains(lower, "requires redis") ||
		strings.Contains(lower, "必须") && strings.Contains(lower, "redis")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
