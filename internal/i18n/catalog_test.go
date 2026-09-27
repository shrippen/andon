package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"andon/internal/enums"
)

// Keys used literally in code and templates, e.g. {{t "conn.ical_url"}},
// T("hint.x.title", …), Message: "kimai.missing_day" (a rule's hint).
var (
	templateKey = regexp.MustCompile(`\bt\s+"([a-z0-9_.]+)"`)
	codeKey     = regexp.MustCompile(`\bi18n\.T\(\s*"([a-z0-9_.]+)"`)
	hintMessage = regexp.MustCompile(`Message:\s*"([a-z0-9_.]+)"`)
)

const srcRoot = "../"

// TestCatalogsHaveSameKeys: a key missing in one language shows the
// fallback (or the raw key) to every user of that language.
func TestCatalogsHaveSameKeys(t *testing.T) {
	ensureLoaded()
	de, en := catalogs[enums.LocaleDE], catalogs[enums.LocaleEN]
	if missing := missingIn(en, de); len(missing) > 0 {
		t.Errorf("only in de.yml: %v", missing)
	}
	if missing := missingIn(de, en); len(missing) > 0 {
		t.Errorf("only in en.yml: %v", missing)
	}
}

// TestUsedKeysExist: every literal key in templates and code, and title
// and why of every hint a rule emits, exists in the catalog.
func TestUsedKeysExist(t *testing.T) {
	ensureLoaded()
	de := catalogs[enums.LocaleDE]
	used := map[string]bool{}

	err := filepath.WalkDir(srcRoot, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || strings.HasSuffix(path, "_test.go") {
			return err
		}
		switch filepath.Ext(path) {
		case ".html":
			collect(t, path, templateKey, used, "")
		case ".go":
			collect(t, path, codeKey, used, "")
			if strings.Contains(path, "/rules/") {
				collect(t, path, hintMessage, used, "hint.")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var missing []string
	for key := range used {
		if _, ok := de[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("used but not in catalog: %v", missing)
	}
}

// collect adds the matches of re in one file; with prefix set, a match
// is a hint message and needs its .title and .why.
func collect(t *testing.T, path string, re *regexp.Regexp, used map[string]bool, prefix string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
		if strings.HasSuffix(m[1], ".") || strings.HasSuffix(m[1], "_") {
			continue // prefix of a computed key, e.g. i18n.T("hint."+msg+".title")
		}
		if prefix == "" {
			used[m[1]] = true
			continue
		}
		used[prefix+m[1]+".title"] = true
		used[prefix+m[1]+".why"] = true
	}
}

func missingIn(have, want map[string]string) []string {
	var out []string
	for key := range want {
		if _, ok := have[key]; !ok {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
