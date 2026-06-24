package handlers

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// loadHelpFixture loads the real `xkeen -help` output captured in testdata.
func loadHelpFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "xkeen-help.txt"))
	if err != nil {
		t.Fatalf("failed to read help fixture: %v", err)
	}
	return string(data)
}

func TestParseHelp_RealFixture_Count(t *testing.T) {
	cmds := parseHelp(loadHelpFixture(t))
	// The fixture lists ~60 flags. Sanity: a healthy count.
	if len(cmds) < 50 {
		t.Errorf("expected >=50 commands from real help, got %d", len(cmds))
	}
}

func TestParseHelp_RealFixture_SpecificCommands(t *testing.T) {
	cmds := parseHelp(loadHelpFixture(t))

	// Key management commands must be present (these were previously
	// hardcoded; they must survive the switch to runtime parsing).
	for _, flag := range []string{"-start", "-stop", "-restart", "-status", "-i", "-remove", "-tp", "-xb", "-xbr", "-v"} {
		if _, ok := cmds[flag]; !ok {
			t.Errorf("expected command %q in parsed help, not found", flag)
		}
	}
}

func TestParseHelp_RealFixture_DescriptionsExtracted(t *testing.T) {
	cmds := parseHelp(loadHelpFixture(t))

	if c := cmds["-status"]; c.Description == "" {
		t.Errorf("-status has empty description")
	}
	// -i description should mention установка
	if c := cmds["-i"]; !contains(c.Description, "установк") && !contains(c.Description, "Установк") {
		t.Errorf("-i description unexpected: %q", c.Description)
	}
}

func TestParseHelp_RealFixture_DangerousFlags(t *testing.T) {
	cmds := parseHelp(loadHelpFixture(t))

	// Installation + Removal categories → dangerous.
	dangerous := []string{"-i", "-io", "-remove", "-dgs", "-dgi", "-dgips", "-dx", "-dm", "-dk"}
	for _, flag := range dangerous {
		c, ok := cmds[flag]
		if !ok {
			t.Errorf("expected %q in parsed help", flag)
			continue
		}
		if !c.Dangerous {
			t.Errorf("expected %q to be Dangerous=true (cat=%q desc=%q)", flag, "", c.Description)
		}
	}

	// Non-destructive commands → NOT dangerous.
	safe := []string{"-start", "-stop", "-restart", "-status", "-tp", "-k", "-g", "-kb", "-kbr", "-xb", "-xbr", "-about", "-v", "-channel", "-xray", "-mihomo"}
	for _, flag := range safe {
		c, ok := cmds[flag]
		if !ok {
			t.Errorf("expected %q in parsed help", flag)
			continue
		}
		if c.Dangerous {
			t.Errorf("expected %q to be Dangerous=false (desc=%q)", flag, c.Description)
		}
	}
}

func TestParseHelp_RealFixture_RenamedCommands(t *testing.T) {
	// Regression: the old hardcoded list had STALE names (-tpx, -cb, -cbr).
	// The real xkeen uses -tp, -xb, -xbr. Runtime parsing must yield the REAL names.
	cmds := parseHelp(loadHelpFixture(t))

	if _, ok := cmds["-tp"]; !ok {
		t.Error("expected -tp (real xkeen flag) — was renamed from stale -tpx")
	}
	if _, ok := cmds["-tpx"]; ok {
		t.Error("stale flag -tpx should NOT exist in real help")
	}
	if _, ok := cmds["-xb"]; !ok {
		t.Error("expected -xb (real xkeen flag) — was renamed from stale -cb")
	}
	if _, ok := cmds["-cb"]; ok {
		t.Error("stale flag -cb should NOT exist in real help")
	}
}

func TestParseHelp_RealFixture_PhantomCommandsAbsent(t *testing.T) {
	// The old hardcoded list had phantom flags (-rrk, -modules, ...) that real
	// xkeen does NOT advertise in -help. They must be absent.
	cmds := parseHelp(loadHelpFixture(t))
	for _, flag := range []string{"-rrk", "-rrx", "-rrm", "-drk", "-drx", "-drm", "-modules", "-delmodules"} {
		if _, ok := cmds[flag]; ok {
			t.Errorf("phantom flag %q should NOT be in real help output", flag)
		}
	}
}

func TestParseHelp_RealFixture_Timeout(t *testing.T) {
	cmds := parseHelp(loadHelpFixture(t))
	for flag, c := range cmds {
		if c.Timeout != CommandTimeout {
			t.Errorf("%q timeout = %v, want %v", flag, c.Timeout, CommandTimeout)
		}
	}
}

// --- parser unit tests on synthetic input ---

func TestParseHelp_BasicExtraction(t *testing.T) {
	input := "  Установка\n" +
		"        -i              Установка XKeen\n" +
		"  Управление\n" +
		"        -start          Запуск\n"
	cmds := parseHelp(input)

	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(cmds), cmds)
	}
	if cmds["-i"].Description != "Установка XKeen" {
		t.Errorf("-i desc = %q", cmds["-i"].Description)
	}
	if cmds["-start"].Description != "Запуск" {
		t.Errorf("-start desc = %q", cmds["-start"].Description)
	}
}

func TestParseHelp_TabSeparated(t *testing.T) {
	// Whitespace-tolerant: tabs should work too.
	input := "  Установка\n\t-i\tУстановка\n"
	cmds := parseHelp(input)
	if _, ok := cmds["-i"]; !ok {
		t.Errorf("expected -i parsed from tab-separated input, got %v", cmds)
	}
}

func TestParseHelp_FlagWithoutDescriptionSkipped(t *testing.T) {
	input := "  Категория\n        -flag\n        -ok   Has desc\n"
	cmds := parseHelp(input)
	if _, ok := cmds["-flag"]; ok {
		t.Error("flag without description should be skipped")
	}
	if _, ok := cmds["-ok"]; !ok {
		t.Error("flag with description should be present")
	}
}

func TestParseHelp_DescriptionWithExampleSafe(t *testing.T) {
	// The help text embeds an example "xkeen -i -toff" inside -toff's description.
	// Only the LEADING token must be taken as the flag.
	input := "  Установка\n" +
		"        -toff           Отключение таймаута (xkeen -i -toff)\n"
	cmds := parseHelp(input)
	if _, ok := cmds["-toff"]; !ok {
		t.Errorf("expected -toff as flag, got %v", cmds)
	}
	// "-i" must NOT appear as a separate command from the embedded example.
	if _, ok := cmds["-i"]; ok {
		t.Error("embedded example -i must not be parsed as a separate flag")
	}
}

func TestParseHelp_EmptyInput(t *testing.T) {
	cmds := parseHelp("")
	if len(cmds) != 0 {
		t.Errorf("empty input should yield 0 commands, got %d", len(cmds))
	}
}

func TestParseHelp_TopLevelLinesIgnored(t *testing.T) {
	// Non-indented lines (boilerplate like "Установка" with no leading space)
	// should NOT be treated as categories that make following commands dangerous.
	// Here both lines have no indent → nothing parsed.
	input := "Установка\n-i что-то\n"
	cmds := parseHelp(input)
	if len(cmds) != 0 {
		t.Errorf("non-indented input should yield 0 commands, got %d: %v", len(cmds), cmds)
	}
}

// --- isDangerous unit tests ---

func TestIsDangerous_Category(t *testing.T) {
	cases := []struct {
		category string
		want     bool
	}{
		{"Установка", true},
		{"Удаление", true},
		{"Переустановка", false}, // critical: NOT matched despite "установ" substring
		{"Обновление", false},
		{"Управление прокси-клиентом", false},
		{"Резервная копия XKeen", false},
		{"Информация", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isDangerous(c.category, "безобидное описание"); got != c.want {
			t.Errorf("isDangerous(category=%q) = %v, want %v", c.category, got, c.want)
		}
	}
}

func TestIsDangerous_Description(t *testing.T) {
	cases := []struct {
		desc string
		want bool
	}{
		{"Полная деинсталляция XKeen", true},
		{"Удалить GeoSite", true},
		{"Удаление задачи", true},
		{"Запуск", false},
		{"Обновить XKeen", false},
		{"Переустановить XKeen", false}, // NOT dangerous
		{"", false},
	}
	for _, c := range cases {
		if got := isDangerous("Безопасная категория", c.desc); got != c.want {
			t.Errorf("isDangerous(desc=%q) = %v, want %v", c.desc, got, c.want)
		}
	}
}

func TestIsDangerous_CaseInsensitive(t *testing.T) {
	// Lowercase category variants.
	if !isDangerous("установка", "x") {
		t.Error("lowercase 'установка' should be dangerous")
	}
	if !isDangerous("УДАЛЕНИЕ", "x") {
		t.Error("uppercase 'УДАЛЕНИЕ' should be dangerous")
	}
}

// contains is a tiny case-sensitive substring helper for assertions.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Ensure CommandTimeout is a sane duration (guards against accidental zero).
func TestCommandTimeoutSane(t *testing.T) {
	if CommandTimeout < time.Minute {
		t.Errorf("CommandTimeout = %v, expected >= 1m", CommandTimeout)
	}
}
